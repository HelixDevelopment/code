package llm

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// OpenAICompatibleProvider implements the Provider interface for OpenAI-compatible local services
// This includes VLLM, Text Generation WebUI, LM Studio, LocalAI, FastChat, Jan AI, and many others
type OpenAICompatibleProvider struct {
	name       string
	config     OpenAICompatibleConfig
	httpClient *http.Client
	models     []ModelInfo
	lastHealth *ProviderHealth
	isRunning  bool

	// healthMu guards lastHealth (HXC-214). GetHealth is NAMED as a query but
	// genuinely mutates the record, so callers invoke it from concurrent paths
	// — health monitors, status endpoints, IsAvailable, and XiaomiProvider,
	// which delegates its own GetHealth straight to this type — assuming it is
	// safe. Every read and write of lastHealth goes through recordHealth, and
	// no network call is ever made with this lock held.
	//
	// KNOWN-UNGUARDED, reported separately (NOT fixed here): isRunning is
	// written by Stop() and read by Generate/Stream/IsAvailable/GetHealth with
	// no synchronization at all. Bringing it under a lock touches five call
	// sites outside the health path, so it belongs to its own change rather
	// than being smuggled into this one.
	healthMu sync.Mutex
}

// OpenAICompatibleConfig holds configuration for OpenAI-compatible providers
type OpenAICompatibleConfig struct {
	BaseURL          string            `json:"base_url"`
	APIKey           string            `json:"api_key"`
	DefaultModel     string            `json:"default_model"`
	Timeout          time.Duration     `json:"timeout"`
	MaxRetries       int               `json:"max_retries"`
	Headers          map[string]string `json:"headers"`
	StreamingSupport bool              `json:"streaming_support"`
	ModelEndpoint    string            `json:"model_endpoint"`
	ChatEndpoint     string            `json:"chat_endpoint"`

	// CACertFile is an OPTIONAL path to a PEM certificate (or bundle) to
	// TRUST IN ADDITION TO the host's system roots when dialling an https://
	// BaseURL. It exists for backends fronted by a private / self-signed CA —
	// the HelixLLM gateway on https://127.0.0.1:8443 being the in-repo case
	// (its CA lives at <repo-root>/submodules/helix_llm/certs/cert.pem).
	//
	// EMPTY IS THE DEFAULT AND CHANGES NOTHING. When this field is blank the
	// provider builds exactly the plain &http.Client{Timeout: ...} it always
	// built, with Go's default transport and the system trust store — the
	// behaviour every existing local (llama.cpp coder, vLLM, LM Studio,
	// LocalAI, Ollama-compatible) and hosted backend on this constructor
	// relies on. This field is strictly additive.
	//
	// SECURITY (CONST-035 / §11.4.6): there is deliberately NO
	// InsecureSkipVerify companion and no "fall back to skipping
	// verification" path. An unreadable or unparseable CA file is a hard
	// construction ERROR, never a silent downgrade to an unverified TLS
	// connection — a silent downgrade is exactly the shape of defect this
	// codebase classes as a bluff (the caller would believe it had a verified
	// channel that it does not have). The certificate is a PUBLIC trust
	// anchor, not a credential, so nothing here is secret (CONST-042).
	CACertFile string `json:"ca_cert_file"`
}

// OpenAICompatibleRequest represents an OpenAI-compatible API request.
// Messages uses OpenAIMessage (not llm.Message) so the assistant turn's
// tool_calls[].function.arguments serialises as a JSON STRING via
// wireSendToolCall — local backends fronted here (VLLM, LMStudio, …) follow the
// same OpenAI contract requiring string-encoded arguments.
type OpenAICompatibleRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
}

// OpenAICompatibleResponse represents an OpenAI-compatible API response
type OpenAICompatibleResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []OpenAICompatibleChoice `json:"choices"`
	Usage   OpenAICompatibleUsage    `json:"usage"`
}

// OpenAICompatibleChoice represents a choice in the response
type OpenAICompatibleChoice struct {
	Index        int                     `json:"index"`
	Message      OpenAICompatibleMessage `json:"message,omitempty"`
	Delta        OpenAICompatibleDelta   `json:"delta,omitempty"`
	FinishReason string                  `json:"finish_reason"`
}

// OpenAICompatibleMessage represents a message in the response.
// ToolCalls uses openAIWireToolCall (not llm.ToolCall) because
// `function.arguments` arrives as a JSON STRING on the wire (OpenAI's canonical
// encoding); decoding straight into llm.ToolCall.Function.Arguments
// (map[string]interface{}) fails. parseOpenAIWireToolCalls handles both the
// string-encoded and raw-object forms — the same path the Groq/DeepSeek/
// Mistral/OpenRouter providers use.
type OpenAICompatibleMessage struct {
	Role             string               `json:"role"`
	Content          string               `json:"content"`
	ToolCalls        []openAIWireToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
}

// OpenAICompatibleDelta represents a delta in streaming response
type OpenAICompatibleDelta struct {
	Role             string               `json:"role,omitempty"`
	Content          string               `json:"content,omitempty"`
	ToolCalls        []openAIWireToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
}

// OpenAICompatibleUsage represents token usage information
type OpenAICompatibleUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAICompatibleModel represents a model in the API response
type OpenAICompatibleModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// endpointRedactionMask is the same mask net/url's URL.Redacted() writes in
// place of a password, reused for the password-less userinfo case below so a
// reader sees ONE mask shape rather than two.
const endpointRedactionMask = "xxxxx"

// endpointUnidentifiableUserinfoPlaceholder replaces a value that carries an
// "@" in userinfo position which net/url did NOT parse as userinfo (round-6
// N3). Distinct wording from the unparseable placeholder below because the
// value IS parseable — what failed is identifying WHERE the credential sits.
const endpointUnidentifiableUserinfoPlaceholder = "[endpoint value redacted: credential position not identifiable]"

// endpointUnparseablePlaceholder replaces an endpoint value net/url refuses.
// It deliberately carries no fragment of the value: an unparseable string is
// precisely where a malformed credential can hide, so echoing "just the part
// before the @" would be the leak this function exists to prevent.
const endpointUnparseablePlaceholder = "[endpoint value redacted: not a parseable URL]"

// endpointSchemeRe matches an RFC 3986 scheme followed by an authority
// marker, ANCHORED at the start of the value.
//
// It replaces an unanchored strings.Contains(rawEndpoint, "://"), which was
// not a scheme test at all and let two credential-bearing shapes past the
// normalisation below untouched — where url.Parse then saw no authority,
// left u.User nil, and the value was returned BYTE-IDENTICAL:
//
//	//svc:s3cr3tpw@api.example.com/v1              (protocol-relative: no "://" anywhere)
//	svc:s3cr3tpw@api.example.com/r?to=https://x    (the "://" was in a QUERY PARAMETER)
//
// Both were proven end-to-end through a real gate refusal before this fix.
var endpointSchemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://`)

// endpointCredentialParamFragments name the query- and fragment-parameter
// KEYS whose value is treated as a credential.
//
// "?api_key=" is an ordinary OpenAI-compatible gateway convention, so a
// credential in the query is not an exotic shape — and unlike userinfo,
// net/url's Redacted() does nothing about it.
//
// Deliberately substring matches, so "X-Amz-Signature", "access_token" and
// "apiKey" are all covered without enumerating spellings. A non-credential
// parameter that happens to contain one of these fragments is masked too;
// that costs a little diagnostic detail in one error message and can never
// cost a credential, which is the right way round for this function to err.
//
// Round-6 N2 widened the list after five ordinary spellings were shown to pass
// BYTE-IDENTICAL through it: "?pass=", "?passphrase=", "?bearer=", "?jwt=" and
// "?cred=". The entries are chosen as the SHORTEST fragment that subsumes a
// family — "pass" covers password/passwd/passphrase, "cred" covers
// credential/creds — so the list stays a set of families rather than a list of
// spellings someone has to remember to extend. "pwd" survives separately
// because it shares no prefix with "pass".
//
// The widening also masks more non-credential keys ("bypass", "passthrough").
// That is the documented direction of error above, restated here so the cost is
// visible at the point where it was accepted.
var endpointCredentialParamFragments = []string{
	"key", "token", "secret", "pass", "pwd", "auth",
	"cred", "sig", "session", "bearer", "jwt",
}

// RedactEndpointForMessage renders a CONFIGURED endpoint safe to quote in an
// error message (CONST-042 / Article XII §12.1 — no credential may leak to
// logs, artefacts, or a response body).
//
// Why this exists: every cloud-gate refusal names the endpoint so an operator
// can see WHICH value is misconfigured, and those errors reach an HTTP
// response body (the server's localRouteRemoteEndpointError wraps them into a
// 500). An operator who sets
// HELIX_LLM_LOCAL_OPENAI_ENDPOINT=https://user:pw@host/v1 would otherwise have
// "pw" rendered into a 500 returned to whoever can reach the endpoint.
//
// WHAT IS REDACTED, precisely — the scope is the two places a credential can
// sit in a URL that this function can identify mechanically:
//
//  1. USERINFO — replaced IN FULL with the mask whenever any userinfo is
//     present, keeping scheme, host, port and path so the operator can still
//     identify the endpoint. This does NOT delegate to url.URL.Redacted()'s
//     own masking, and must not: Redacted() masks the PASSWORD and PRESERVES
//     THE USERNAME, so both "https://<token>@host" (password-less bearer
//     shape) and "https://<key>:@host" (the vendor-documented `curl -u key:`
//     shape, where a password component exists but is empty) would print the
//     credential in full. Masking the whole userinfo is a deliberate
//     strengthening of the stdlib behaviour, not an approximation, and it
//     costs no diagnostic: nothing that identifies the endpoint lives in the
//     userinfo.
//  2. QUERY and FRAGMENT parameters whose KEY looks credential-bearing
//     (endpointCredentialParamFragments) — value replaced with the mask, key
//     kept so the message still says WHICH parameter was set.
//
// AND WHAT IS NOT (§11.4.6 — stated, not implied away). A credential can be
// carried in a shape no mechanical rule can distinguish from ordinary
// configuration: a path segment ("/v1/sk-live-abc/chat"), an opaque host
// label, or a query parameter with an innocuous name ("?t=..."). Those pass
// through. This function reduces the leak surface to the two identifiable
// carriers above; it does not make an arbitrary string safe to print, and no
// caller should read it as doing so.
//
// Values carrying NEITHER carrier are returned BYTE-IDENTICAL, deliberately:
// re-rendering through url.URL.String() can normalise or percent-escape, and
// silently rewriting the endpoint in every ordinary misconfiguration message
// would degrade the diagnostic for a leak that is not present.
//
// An UNPARSEABLE value yields the placeholder, never the raw string: failing
// open there would defeat the whole function, since a value net/url cannot
// parse is the most likely place for a malformed credential to sit.
//
// Scheme normalisation. Three anchored cases, decided in this order:
//
//	scheme://host…   parsed as-is
//	//host…          parsed as "http:" + value   (protocol-relative)
//	anything else    parsed as "http://" + value (scheme-less authority)
//
// The last case is why a scheme-less "user:pw@host:8000" cannot slip through:
// url.Parse would otherwise read "user" as the SCHEME and never see the
// userinfo. The temporary prefix is stripped back off — note the two prefixes
// are different LENGTHS, so each case trims exactly what it added — so the
// operator sees the shape they configured.
func RedactEndpointForMessage(rawEndpoint string) string {
	raw := strings.TrimSpace(rawEndpoint)
	if raw == "" {
		return rawEndpoint
	}

	u, addedPrefix, ok := parseEndpointForRedaction(raw)
	if !ok {
		return endpointUnparseablePlaceholder
	}

	hasUserinfo := u.User != nil
	// ROUND-6 N3 — fail closed on an "@" that did not become userinfo.
	//
	// Three real shapes put an "@" in userinfo position that net/url resolves
	// to something else, leaving u.User nil and the value BYTE-IDENTICAL:
	//
	//	http:/user:pw@host/x            one slash, so no authority is parsed
	//	https://user/x:pw@host/v1       the "@" lands in the PATH
	//	1http://user:pw@host/x          not a legal scheme, so no authority
	//
	// The discriminator is positional and cheap: an "@" appearing before any
	// "?" or "#" is in the region where userinfo can legally live, so if no
	// userinfo was parsed out of it the function cannot say where the
	// credential is — and answering "nowhere" is the leak.
	//
	// COST, stated rather than implied away (§11.4.6 / §11.4.201): a legal
	// endpoint with an "@" in its PATH ("/@scope/v1") now gets the placeholder
	// instead of its own text. That is a false refusal of a diagnostic, which
	// is the cheap direction; the alternative is echoing a credential.
	if !hasUserinfo && endpointHasUnresolvedUserinfoMarker(raw) {
		return endpointUnidentifiableUserinfoPlaceholder
	}
	maskedParams := maskCredentialEndpointParams(u)
	if !hasUserinfo && !maskedParams {
		// Nothing identifiable to hide: pass the operator's own value through
		// untouched rather than a normalised rewrite of it.
		return rawEndpoint
	}

	if hasUserinfo {
		// ROUND-7 BLOCKER A1 — replace the WHOLE userinfo, unconditionally.
		//
		// The previous form masked the userinfo ONLY when no password
		// component existed and otherwise delegated to url.URL.Redacted().
		// Redacted() (like net/http's stripPassword, which shapes every
		// *url.Error this package rewrites) masks the PASSWORD and PRESERVES
		// THE USERNAME — so any value with a password component printed its
		// username in full at every transport site, both carriers, and in the
		// gate refusal that reaches a 500 response body.
		//
		// That is not an exotic shape: `curl -u sk_test_xxx:` is Stripe's own
		// documented form, i.e. the credential IS the username and the
		// password is empty-but-present, which is precisely the branch the
		// old condition sent to Redacted().
		//
		// There is no case where the username half of a userinfo is worth
		// printing: scheme, host, port and path all survive below and are
		// what identify the endpoint for the operator. Masking the whole
		// userinfo costs no diagnostic and removes the carrier entirely,
		// rather than removing one of its two components.
		u.User = url.User(endpointRedactionMask)
	}
	redacted := u.Redacted()
	return strings.TrimPrefix(redacted, addedPrefix)
}

// endpointHasUnresolvedUserinfoMarker reports whether raw contains an "@" in
// the region where userinfo can legally appear — i.e. before any "?" or "#".
//
// An "@" AFTER either delimiter belongs to a query or fragment VALUE
// ("?email=a@b.com") and is not a userinfo marker, so it must not trip the
// fail-closed branch and rewrite an ordinary credential-free endpoint.
func endpointHasUnresolvedUserinfoMarker(raw string) bool {
	at := strings.IndexByte(raw, '@')
	if at < 0 {
		return false
	}
	if delim := strings.IndexAny(raw, "?#"); delim >= 0 && delim < at {
		return false
	}
	return true
}

// parseEndpointForRedaction normalises rawEndpoint to something url.Parse can
// read as an authority-bearing URL, returning the parsed value and whatever
// prefix had to be added (to be trimmed back off the rendered result).
//
// The OPAQUE fallback exists because prefixing an authority mangles a legal
// opaque URI: "https:api.example.com/v1" becomes "http://https:api.example.com/v1",
// whose "port" is not numeric, so url.Parse rejects it and the placeholder was
// returned for a value carrying no credential at all — a false refusal
// (§11.4.201) of correct operator configuration.
//
// The fallback is reached ONLY when the authority form failed to parse, and it
// refuses anything containing "@": userinfo cannot exist without one, so a
// value with no "@" has no userinfo for the opaque parse to overlook, while a
// value that has one is treated as the malformed-credential case it may be.
func parseEndpointForRedaction(raw string) (*url.URL, string, bool) {
	addedPrefix := ""
	switch {
	case endpointSchemeRe.MatchString(raw):
		// Already absolute; parse verbatim.
	case strings.HasPrefix(raw, "//"):
		addedPrefix = "http:"
	default:
		addedPrefix = "http://"
	}

	if u, err := url.Parse(addedPrefix + raw); err == nil {
		return u, addedPrefix, true
	}

	if strings.Contains(raw, "@") {
		return nil, "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque == "" {
		return nil, "", false
	}
	return u, "", true
}

// maskCredentialEndpointParams replaces the VALUE of every credential-shaped
// query or fragment parameter with the mask, reporting whether it changed
// anything. Reported rather than inferred, so a value with nothing to mask can
// be returned byte-identical by the caller instead of re-rendered.
func maskCredentialEndpointParams(u *url.URL) bool {
	masked := false
	if u.RawQuery != "" {
		if q, changed := maskCredentialPairs(u.RawQuery); changed {
			u.RawQuery = q
			masked = true
		}
	}
	if frag := u.EscapedFragment(); frag != "" {
		if f, changed := maskCredentialPairs(frag); changed {
			// f is an ESCAPED fragment (maskCredentialPairs works on the raw
			// string), so it belongs in RawFragment. Round-6 N4: assigning it
			// to Fragment instead made String() escape it a SECOND time, so an
			// untouched sibling parameter's "%2F" was re-emitted as "%252F" —
			// contradicting this function's documented promise that parameters
			// it does not mask survive verbatim.
			//
			// Both fields are still written together, for the original reason:
			// String() prefers RawFragment only when it is a valid encoding of
			// Fragment, so a stale RawFragment could re-emit the masked
			// credential. If f will not round-trip, fall back to the
			// double-escaping-but-non-leaking form — losing verbatimness is
			// acceptable, losing the mask is not.
			if unescaped, err := url.PathUnescape(f); err == nil {
				u.Fragment = unescaped
				u.RawFragment = f
			} else {
				u.Fragment = f
				u.RawFragment = ""
			}
			masked = true
		}
	}
	return masked
}

// maskCredentialPairs rewrites an "a=b&c=d" parameter string, masking the
// value of every credential-shaped key. It works on the RAW string rather than
// url.Values so that ordering, separators and encoding of the untouched
// parameters survive verbatim.
func maskCredentialPairs(rawPairs string) (string, bool) {
	parts := strings.Split(rawPairs, "&")
	changed := false
	for i, part := range parts {
		key, _, hasValue := strings.Cut(part, "=")
		if !hasValue || !isCredentialEndpointParam(key) {
			continue
		}
		parts[i] = key + "=" + endpointRedactionMask
		changed = true
	}
	if !changed {
		return rawPairs, false
	}
	return strings.Join(parts, "&"), true
}

// errorChainRedactionMaxDepth bounds the cause-chain walk below. A cyclic or
// pathologically deep chain must not hang the process inside an error path.
//
// ROUND-7 A3: the bound was 32, and a chain DEEPER than that is a SILENT
// fail-open — the walk simply stops and every *url.Error below the cut keeps
// its credential. That was demonstrated with lazily-unwrapping wrappers. The
// bound is not removed (a cyclic chain must still terminate) but is raised far
// past any wrapping depth real code produces, so the fail-open needs a
// deliberately adversarial 257-deep chain rather than an unlucky one.
//
// HONEST BOUNDARY (§11.4.6): this makes the fail-open unreachable in practice,
// NOT impossible. A chain deeper than this still loses redaction below the
// cut, and that residual is stated here rather than implied away.
const errorChainRedactionMaxDepth = 256

// RedactEndpointsInError makes a TRANSPORT error safe to interpolate into an
// operator-facing message, by rewriting the URL carried by every *url.Error in
// err's cause chain through RedactEndpointForMessage. It returns the SAME
// error value it was given.
//
// WHY THIS EXISTS — the second carrier. RedactEndpointForMessage cleans the
// CONFIGURED endpoint string. It does nothing about the endpoint that net/http
// and net/url put inside the error they return, and those are a genuinely
// separate carrier of the same credential:
//
//	health.Message = fmt.Sprintf("unreachable at %s: %v",
//	    RedactEndpointForMessage(p.baseURL), err)   // <- err still has the URL
//
// net/http already runs the request URL through its own stripPassword before
// storing it in *url.Error, which is exactly why this gap survived a guard:
// stripPassword masks the PASSWORD component and nothing else, so
// "https://user:pw@host" comes back clean while the ordinary machine shape for
// a bearer credential — "https://<token>@host", where the credential IS the
// username and there is no password at all — is echoed IN FULL. A test written
// with a password-bearing fixture therefore passes for a reason that does not
// generalise to the password-less shape.
//
// net/url is the other producer: url.Parse failures are returned as
// &url.Error{Op: "parse", URL: rawURL, ...} with the raw value untouched, so
// http.NewRequest on a malformed credential-bearing endpoint leaks it too.
//
// IN-PLACE, DELIBERATELY. The *url.Error values are mutated rather than copied,
// so the returned error is identical to the input: errors.Is, errors.As,
// errors.Unwrap, and the net.Error Timeout()/Temporary() assertions callers use
// to classify a failure all behave exactly as they did before. Rebuilding the
// chain would break precisely those. Concretely, internal/server's
// providerResolveStatus picks 403/400/500/503 by errors.Is on sentinel values,
// so a rebuilt chain would ship as wrong HTTP status codes rather than as a
// test failure; the net.Error half is contract preservation for arbitrary
// callers rather than a named consumer (see the Timeout/Temporary note below).
//
// ORDERING IS LOAD-BEARING — call this BEFORE wrapping. fmt.Errorf formats its
// message EAGERLY, so a wrapper built before redaction keeps a copy of the
// unredacted text in its own message even after the inner *url.Error is
// cleaned. The call must sit INSIDE the format call — fmt.Errorf("...: %w",
// RedactEndpointsInError(err)) — never around it.
//
// WHICH CALL SITES ACTUALLY DO THAT, and how you can tell. An earlier revision
// of this comment asserted "every call site therefore redacts the error the
// moment it comes back from the transport" — and that sentence was FALSE in
// this very file: ten of its twelve transport wraps passed the raw *url.Error
// straight into fmt.Errorf, for the whole round-8 batch, while a reader who
// grepped for this helper and read this paragraph had every reason to stop
// looking. An unenforced claim in a doc comment decays into a false fact.
//
// So the claim is now mechanical rather than editorial, and it is scoped to
// exactly what a scan can check: transport_redaction_scan_test.go walks the
// module, takes EVERY non-test file that MENTIONS this helper as having
// adopted the discipline, and FAILS if any transport-derived error in such a
// file reaches fmt.Errorf/fmt.Sprintf without passing through it. Partial
// adoption — the round-8 defect — is what that scan catches.
//
// It says NOTHING about files that never mention this helper (they make no
// claim to redact, and neither does this comment on their behalf), and its
// taint tracking is position-ordered rather than control-flow-aware. Both
// boundaries are stated in that file rather than implied away here.
//
// WHAT IS NOT COVERED (§11.4.6 — stated, not implied away):
//   - The bound above. Everything RedactEndpointForMessage cannot identify —
//     a credential in a path segment, an opaque host label, a query parameter
//     with an innocuous name — passes through here as well.
//   - Errors that carry an endpoint somewhere OTHER than a *url.Error.URL
//     field — a driver that formats the address into its own message string is
//     not reachable from here.
//   - Anything already formatted into a wrapper's message, per the ordering
//     note above.
func RedactEndpointsInError(err error) error {
	if err == nil {
		return nil
	}
	redactURLErrorsInChain(err, 0)
	return err
}

// redactURLErrorsInChain visits EVERY node of err's cause tree, not just the
// first match.
//
// errors.As would stop at the first *url.Error it finds; a chain can hold more
// than one (a retry wrapper around a transport error, or either branch of an
// errors.Join), and a missed one is a leak. Both Unwrap shapes are followed for
// the same reason.
func redactURLErrorsInChain(err error, depth int) {
	if err == nil || depth > errorChainRedactionMaxDepth {
		return
	}
	if ue, ok := err.(*url.Error); ok {
		raw := ue.URL
		ue.URL = RedactEndpointForMessage(raw)
		// ROUND-7 — the FOURTH carrier, found by the A1 half-assertion sweep.
		//
		// Rewriting ue.URL is not sufficient, because net/http echoes parts of
		// the endpoint inside the WRAPPED cause as well. The concrete case: a
		// scheme-less endpoint "sk_test_xxx:@host/v1" is read by net/url with
		// the CREDENTIAL as the scheme, so the transport fails with
		//
		//	unsupported protocol scheme "sk_test_xxx"
		//
		// and that text lives in ue.Err, which this walk previously never
		// touched. The credential therefore rode out of every transport site
		// in full while ue.URL beside it was correctly masked.
		//
		// It was invisible until the sweep because the shape rows named their
		// username "svc"/"user" and never asserted it absent — the same
		// half-assertion that hid A1 itself.
		ue.Err = scrubUserinfoFromCause(raw, ue.Err)
	}
	switch u := err.(type) {
	case interface{ Unwrap() error }:
		redactURLErrorsInChain(u.Unwrap(), depth+1)
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			redactURLErrorsInChain(e, depth+1)
		}
	}
}

// scrubUserinfoFromCause returns cause with every USERINFO component of rawURL
// masked out of its rendered text, or cause unchanged when nothing matched.
//
// Only the userinfo components are scrubbed — not the host, port or path —
// because those identify the endpoint for the operator and carry no
// credential this function can identify (the same boundary
// RedactEndpointForMessage documents).
//
// Error IDENTITY is preserved: the returned value UNWRAPS to the original, so
// errors.Is / errors.As keep working on whatever the transport produced
// (context.DeadlineExceeded, net.Error, …). Only the rendered text changes.
//
// COST, stated rather than implied away (§11.4.6 / §11.4.201): the match is a
// plain substring replace, so a degenerate one- or two-character username
// would also mask innocent occurrences of those characters in the cause text.
// That is a corrupted diagnostic, which is the cheap direction; the
// alternative is echoing the credential.
func scrubUserinfoFromCause(rawURL string, cause error) error {
	if cause == nil {
		return nil
	}
	u, _, ok := parseEndpointForRedaction(strings.TrimSpace(rawURL))
	if !ok || u.User == nil {
		return cause
	}
	msg := cause.Error()
	out := msg
	if name := u.User.Username(); name != "" {
		out = strings.ReplaceAll(out, name, endpointRedactionMask)
	}
	if pw, has := u.User.Password(); has && pw != "" {
		out = strings.ReplaceAll(out, pw, endpointRedactionMask)
	}
	if out == msg {
		return cause
	}
	return &redactedCauseError{text: out, cause: cause}
}

// redactedCauseError renders a scrubbed message while remaining transparent to
// errors.Is / errors.As through Unwrap.
type redactedCauseError struct {
	text  string
	cause error
}

func (e *redactedCauseError) Error() string { return e.text }
func (e *redactedCauseError) Unwrap() error { return e.cause }

// Timeout and Temporary are FORWARDED, and that forwarding is load-bearing.
//
// url.Error.Timeout() does a DIRECT type assertion on its Err field —
// `e.Err.(interface{ Timeout() bool })` — NOT errors.As. So a wrapper that
// only implements Unwrap is transparent to errors.Is/As but INVISIBLE to that
// assertion, and substituting one for a context.DeadlineExceeded cause would
// silently flip net.Error.Timeout() to false.
//
// WHY THAT MATTERS, stated accurately (§11.4.6). An earlier revision justified
// this forwarding by naming a specific consumer — internal/server/llm_generate.go
// "classifies HTTP status codes off exactly that". It does not, and did not:
// providerResolveStatus keys purely on errors.Is against three sentinels, and
// internal/server contains no non-test use of Timeout() or Temporary() at all.
// The forwarding is kept regardless, and for a better reason than a consumer
// that does not exist: *url.Error satisfies net.Error, that is part of the
// error's PUBLIC CONTRACT, and this helper promises to return a value
// indistinguishable from its input. Any caller — present or future, in this
// repo or another — may assert net.Error on a transport error, and none of
// them should be able to observe that redaction happened. An over-stated
// justification is worse than a general one: it decays into a false fact the
// moment its named consumer changes. Temporary() has the same shape in
// url.Error and is forwarded for the same reason.
//
// When the cause implements NEITHER, these return false — identical to the
// pre-wrap behaviour, where the type assertion simply failed.
func (e *redactedCauseError) Timeout() bool {
	t, ok := e.cause.(interface{ Timeout() bool })
	return ok && t.Timeout()
}

func (e *redactedCauseError) Temporary() bool {
	t, ok := e.cause.(interface{ Temporary() bool }) //nolint:staticcheck // mirrors url.Error
	return ok && t.Temporary()
}

// isCredentialEndpointParam reports whether a parameter key names a credential.
func isCredentialEndpointParam(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, frag := range endpointCredentialParamFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider.
//
// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive serving):
// this constructor is a SECOND gate site alongside NewCloudProvider, because
// hosted providers reach it WITHOUT ever passing through NewCloudProvider —
// BuildDynamicOpenAICompatibleProviders (verifier_dynamic_catalogue.go) and
// NewHostedOpenAICompatibleProvider (openai_compatible_catalogue.go) both call
// here directly. Without a check at this site the gate is decorative for that
// entire class: with llm.cloud.enabled false and e.g. CEREBRAS_API_KEY or
// TOGETHER_API_KEY exported, the TUI and desktop would still register REAL
// hosted providers.
//
// The exemption keys on ENDPOINT LOCALITY (isLocalEndpointURL), never on the
// provider name: the local HelixLLM coder route (base URL
// http://localhost:18434), llama.cpp, vLLM, LM Studio and LocalAI all use THIS
// constructor, so a name-based or blanket refusal would break local serving —
// the opposite of the mandate. Local endpoints are completely unaffected.
func NewOpenAICompatibleProvider(name string, config OpenAICompatibleConfig) (*OpenAICompatibleProvider, error) {
	// Set default endpoints if not specified
	if config.ModelEndpoint == "" {
		config.ModelEndpoint = "/v1/models"
	}
	if config.ChatEndpoint == "" {
		config.ChatEndpoint = "/v1/chat/completions"
	}

	// Resolve the EFFECTIVE base URL first — the same resolution getAPIURL
	// performs — so the gate judges the URL this provider will actually dial
	// rather than the possibly-empty configured value. A name-defaulted base
	// URL is always a loopback address, so callers relying on the per-backend
	// defaults stay local by construction and pass the gate.
	effectiveBaseURL := effectiveOpenAICompatibleBaseURL(name, config.BaseURL)
	if !cloudGate.Load() && !isLocalEndpointURL(effectiveBaseURL) {
		return nil, fmt.Errorf("%w: llm.cloud.enabled is false (default); "+
			"OpenAI-compatible provider %q targets the remote endpoint %q and "+
			"will not be constructed. Set llm.cloud.enabled: true to permit "+
			"cloud providers, or point it at a local endpoint (the local "+
			"helixllm coder, llamacpp, vllm, lmstudio and localai routes are "+
			"unaffected)",
			ErrCloudDisabled, name, RedactEndpointForMessage(effectiveBaseURL))
	}

	// TLS trust is resolved BEFORE the provider value is built so a bad CA
	// path fails construction outright rather than producing a provider that
	// dials and fails opaquely on every request (§11.4.6 — the failure names
	// its own cause).
	httpClient, clientErr := newOpenAICompatibleHTTPClient(name, config)
	if clientErr != nil {
		return nil, clientErr
	}

	provider := &OpenAICompatibleProvider{
		name:       name,
		config:     config,
		httpClient: httpClient,
		isRunning:  true,
		lastHealth: &ProviderHealth{
			Status:    "unknown",
			LastCheck: time.Now(),
		},
	}

	// Discover available models
	if err := provider.discoverModels(); err != nil {
		log.Printf("Warning: Failed to discover models for %s: %v", name, err)
	}

	log.Printf("✅ %s provider initialized with %d models", name, len(provider.models))
	return provider, nil
}

// newOpenAICompatibleHTTPClient builds the provider's *http.Client.
//
// TWO paths, and the first one is the one that must never change:
//
//	(1) config.CACertFile EMPTY  — the overwhelmingly common case, every
//	    plain-http local backend and every hosted https backend on public
//	    roots. Returns byte-for-byte the same &http.Client{Timeout: ...} this
//	    constructor has always built: nil Transport, so net/http uses
//	    DefaultTransport and the host's system trust store. No new behaviour
//	    reaches any existing caller.
//
//	(2) config.CACertFile SET — the private-CA case. The named PEM is
//	    APPENDED TO A COPY OF THE SYSTEM ROOT POOL (x509.SystemCertPool
//	    returns a copy; mutating it does not affect any other client), never
//	    substituted for it. Appending rather than replacing means a provider
//	    configured with a private CA can still reach ordinary public
//	    endpoints — a replace-the-pool implementation would silently break
//	    every public host for that provider.
//
// FAILURE IS LOUD (CONST-035 / §11.4.6). A missing, unreadable, or
// non-PEM CA file returns an error naming the path and the cause. There is
// no InsecureSkipVerify anywhere on this path and no fallback that would
// dial the endpoint with verification disabled: the caller either gets a
// client that genuinely verifies the named CA, or an honest error. Silently
// downgrading to an unverified connection would let a PASS be reported for a
// channel that was never authenticated.
//
// MinVersion is pinned to TLS 1.2 — Go's own default for clients since 1.18,
// stated explicitly here so the custom tls.Config cannot be read as relaxing
// anything relative to the default transport.
func newOpenAICompatibleHTTPClient(name string, config OpenAICompatibleConfig) (*http.Client, error) {
	caPath := strings.TrimSpace(config.CACertFile)
	if caPath == "" {
		// Path (1): unchanged from before this field existed.
		return &http.Client{Timeout: config.Timeout}, nil
	}

	pemBytes, readErr := os.ReadFile(caPath)
	if readErr != nil {
		return nil, fmt.Errorf("provider %q: cannot read CA certificate %q: %w "+
			"(TLS verification is NOT skipped on this failure — fix the path or "+
			"clear the CA setting)", name, caPath, readErr)
	}

	// A COPY of the system pool (SystemCertPool documents that it returns a
	// copy) so public roots keep working alongside the private CA.
	pool, poolErr := x509.SystemCertPool()
	if poolErr != nil || pool == nil {
		// Strictly-narrower fallback, never wider: an empty pool trusts ONLY
		// the CA appended below. Recorded rather than silent (§11.4.6).
		log.Printf("Warning: %s provider: system certificate pool unavailable (%v); "+
			"trusting ONLY %s", name, poolErr, caPath)
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("provider %q: CA certificate %q contains no parseable "+
			"PEM certificate (TLS verification is NOT skipped on this failure)",
			name, caPath)
	}

	tlsConfig := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}

	// CLONE THE STDLIB TRANSPORT, do not hand-build one. A zero-value
	// &http.Transport{TLSClientConfig: ...} is NOT "DefaultTransport plus a
	// trust pool" — it is a transport with every default DISCARDED:
	//
	//   ProxyFromEnvironment   gone -> HTTP(S)_PROXY / NO_PROXY ignored, so a
	//                          provider behind a corporate proxy silently
	//                          fails to connect (or worse, bypasses it).
	//   DialContext timeouts   gone -> no 30s dial timeout and no keep-alive;
	//                          a black-holed endpoint hangs on the connect
	//                          rather than erroring.
	//   TLSHandshakeTimeout    gone -> a stalled handshake never times out.
	//   MaxIdleConns /         gone -> unbounded idle connections, no
	//   IdleConnTimeout             90s reaping: an FD leak under load.
	//   ExpectContinueTimeout  gone.
	//   ForceAttemptHTTP2      gone -> and this one is a TRAP: net/http only
	//                          auto-upgrades to HTTP/2 when TLSClientConfig is
	//                          nil, so the moment a custom tls.Config is set
	//                          the connection silently drops to HTTP/1.1
	//                          unless ForceAttemptHTTP2 is true. Clone()
	//                          carries DefaultTransport's true through.
	//
	// Path (1) above returns a client with a nil Transport and therefore keeps
	// all of those; before this clone, path (2) quietly lost every one of them
	// — so configuring a private CA also downgraded the connection's proxy,
	// timeout, pooling and protocol behaviour. Clone() is the stdlib's own
	// documented way to derive a modified transport and is a deep copy, so
	// mutating the result cannot affect DefaultTransport or any other client.
	transport := clonedDefaultTransport(name)
	transport.TLSClientConfig = tlsConfig

	return &http.Client{
		Timeout:   config.Timeout,
		Transport: transport,
	}, nil
}

// clonedDefaultTransport returns a deep copy of http.DefaultTransport, or a
// minimal transport when DefaultTransport is not the concrete *http.Transport
// the stdlib ships.
//
// The type assertion is guarded rather than bare because http.DefaultTransport
// is a package-level variable of INTERFACE type (http.RoundTripper): the
// stdlib always assigns it an *http.Transport, but any package in the process
// can reassign it (instrumentation wrappers and some test harnesses do). A
// bare `http.DefaultTransport.(*http.Transport)` would panic on such a
// process, taking down provider construction for a reason that has nothing to
// do with this provider.
//
// The fallback is deliberately the SAME transport shape this function built
// before the clone existed, so the worst case is exactly the old behaviour and
// never something weaker: the caller still receives a transport whose
// TLSClientConfig it sets, TLS verification is still fully enabled, and
// nothing is silently downgraded to an unverified connection. The one thing
// that is NOT silent is the event itself — it is logged, because losing the
// stdlib defaults is a real (if benign-by-design) degradation and §11.4.6
// forbids letting it pass unrecorded.
func clonedDefaultTransport(name string) *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
		return base.Clone()
	}
	log.Printf("Warning: %s provider: http.DefaultTransport is %T, not *http.Transport; "+
		"building a minimal TLS transport WITHOUT the stdlib proxy/timeout/HTTP2 "+
		"defaults (TLS verification is unaffected and remains enabled)",
		name, http.DefaultTransport)
	return &http.Transport{ForceAttemptHTTP2: true}
}

// BaseURL returns the provider's configured base URL. Exposed so out-of-package
// callers (and tests) can assert which URL a provider was constructed with —
// load-bearing for the CONST-036/CONST-046 guarantee that the dynamic catalogue
// uses the verifier's api_url rather than a hardcoded literal. Carries no
// credential (CONST-042).
func (p *OpenAICompatibleProvider) BaseURL() string {
	return p.config.BaseURL
}

// GetType returns the provider type
func (p *OpenAICompatibleProvider) GetType() ProviderType {
	switch p.name {
	case "vllm":
		return ProviderTypeVLLM
	case "localai":
		return ProviderTypeLocalAI
	case "fastchat":
		return ProviderTypeFastChat
	case "textgen":
		return ProviderTypeTextGen
	case "lmstudio":
		return ProviderTypeLMStudio
	case "jan":
		return ProviderTypeJan
	case "koboldai":
		return ProviderTypeKoboldAI
	case "gpt4all":
		return ProviderTypeGPT4All
	case "tabbyapi":
		return ProviderTypeTabbyAPI
	case "mlx":
		return ProviderTypeMLX
	case "mistralrs":
		return ProviderTypeMistralRS
	case "", "local":
		// Genuinely-unnamed / explicitly-local backends keep the generic type.
		return ProviderTypeLocal
	default:
		// Hosted OpenAI-compatible catalogue providers (cerebras, fireworks,
		// novita, …) are constructed with a distinct name. Returning the generic
		// ProviderTypeLocal here would mislabel EVERY such provider's
		// ModelInfo.Provider as "local" and collide them into one bucket. Derive a
		// stable distinct ProviderType from the name instead so each provider's
		// models are attributed to the correct provider (CONST-036 single-source
		// attribution). Known local backends are handled by the cases above.
		return ProviderType(p.name)
	}
}

// GetName returns the provider name
func (p *OpenAICompatibleProvider) GetName() string {
	return p.name
}

// GetModels returns available models
func (p *OpenAICompatibleProvider) GetModels() []ModelInfo {
	return p.models
}

// GetCapabilities returns model capabilities
func (p *OpenAICompatibleProvider) GetCapabilities() []ModelCapability {
	capabilities := []ModelCapability{
		CapabilityTextGeneration,
		CapabilityCodeGeneration,
		CapabilityCodeAnalysis,
		CapabilityPlanning,
		CapabilityDebugging,
		CapabilityRefactoring,
		CapabilityTesting,
	}

	// Add vision capability for models that might support it
	if p.supportsVision() {
		capabilities = append(capabilities, CapabilityVision)
	}

	return capabilities
}

// Generate generates a response using the OpenAI-compatible API
func (p *OpenAICompatibleProvider) Generate(ctx context.Context, request *LLMRequest) (*LLMResponse, error) {
	if !p.isRunning {
		return nil, ErrProviderUnavailable
	}

	// Prepare API request
	apiRequest := p.convertToOpenAIRequest(request)

	// Make API call
	startTime := time.Now()
	response, err := p.makeAPIRequest(ctx, apiRequest)
	if err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}

	processingTime := time.Since(startTime)

	// Convert response
	llmResponse := p.convertFromOpenAIResponse(response, request.ID, processingTime)

	return llmResponse, nil
}

// GenerateStream generates a streaming response
func (p *OpenAICompatibleProvider) GenerateStream(ctx context.Context, request *LLMRequest, ch chan<- LLMResponse) error {
	// Channel-ownership contract (see Provider.GenerateStream interface doc):
	// the provider (the SENDER) is the SOLE closer of ch, and MUST close it on
	// every return path — success, error, and ctx-cancel. The consumer never
	// closes ch; a double-close would panic in a spawned goroutine and crash the
	// process (server defect #5). defer guarantees close on every return path.
	defer close(ch)
	if !p.isRunning {
		return ErrProviderUnavailable
	}

	if !p.config.StreamingSupport {
		// Fallback to non-streaming
		response, err := p.Generate(ctx, request)
		if err != nil {
			return err
		}
		select {
		case ch <- *response:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	// Prepare streaming API request
	apiRequest := p.convertToOpenAIRequest(request)
	apiRequest.Stream = true

	// Make streaming request
	return p.makeStreamingRequest(ctx, apiRequest, ch, request.ID)
}

// IsAvailable checks if the provider is available
func (p *OpenAICompatibleProvider) IsAvailable(ctx context.Context) bool {
	if !p.isRunning {
		return false
	}

	health, err := p.GetHealth(ctx)
	return err == nil && health.Status == "healthy"
}

// GetHealth returns provider health status
func (p *OpenAICompatibleProvider) GetHealth(ctx context.Context) (*ProviderHealth, error) {
	if !p.isRunning {
		return &ProviderHealth{
			Status:     "unhealthy",
			LastCheck:  time.Now(),
			ErrorCount: 1,
		}, nil
	}

	// Test model endpoint for basic availability
	start := time.Now()
	url := p.getAPIURL(p.config.ModelEndpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return p.recordHealth("unhealthy", 0, errCountIncrement, modelCountUnchanged),
			fmt.Errorf("failed to create health check request: %v", RedactEndpointsInError(err))
	}

	// Set headers
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(req)
	latency := time.Since(start)

	if err != nil {
		return p.recordHealth("unhealthy", latency, errCountIncrement, modelCountUnchanged),
			fmt.Errorf("health check failed: %v", RedactEndpointsInError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return p.recordHealth("unhealthy", latency, errCountIncrement, modelCountUnchanged),
			fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	// Try to parse models to get model count
	var modelsResponse struct {
		Data []OpenAICompatibleModel `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		// Still consider it available: the endpoint answered, only the body was
		// unreadable, so this is not a new connectivity failure.
		return p.recordHealth("degraded", latency, errCountKeep, modelCountUnchanged), nil
	}

	return p.recordHealth("healthy", latency, errCountReset, len(modelsResponse.Data)), nil
}

// Close stops the provider
func (p *OpenAICompatibleProvider) Close() error {
	p.isRunning = false
	if p.httpClient != nil {
		p.httpClient.CloseIdleConnections()
	}
	log.Printf("✅ %s provider closed", p.name)
	return nil
}

// GetContextWindow returns the model's context window size in tokens.
// Default: 200_000 — OpenAI-compatible providers (VLLM, LMStudio, etc.) vary;
// 200k is a safe upper bound pending a Phase 3 model-aware upgrade.
func (p *OpenAICompatibleProvider) GetContextWindow() int {
	return 200_000
}

// CountTokens returns an estimated token count for text.
// Uses char-based fallback (1 token ≈ 3.5 chars) — Phase 3 will upgrade
// to the provider's tokenize endpoint when available.
func (p *OpenAICompatibleProvider) CountTokens(text string) (int, error) {
	return CharBasedTokenCount(text)
}

// Private helper methods

func (p *OpenAICompatibleProvider) discoverModels() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := p.getAPIURL(p.config.ModelEndpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create models request: %w", RedactEndpointsInError(err))
	}

	// Set headers
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch models: %w", RedactEndpointsInError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Data []OpenAICompatibleModel `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode models response: %w", err)
	}

	// Convert to ModelInfo
	for _, model := range response.Data {
		modelInfo := ModelInfo{
			Name:        model.ID,
			Provider:    p.GetType(),
			ContextSize: p.inferContextSize(model.ID),
			MaxTokens:   p.inferMaxTokens(model.ID),
			Description: fmt.Sprintf("%s model: %s", p.name, model.ID),
		}
		p.models = append(p.models, modelInfo)
	}

	for i := range p.models {
		EnrichModelInfo(&p.models[i])
	}

	return nil
}

func (p *OpenAICompatibleProvider) convertToOpenAIRequest(request *LLMRequest) *OpenAICompatibleRequest {
	// Convert each message to the OpenAI wire shape so the assistant turn's
	// tool_calls serialise function.arguments as a JSON STRING (the
	// OpenAI-compatible contract); marshalling llm.Message directly would emit
	// the object form the backends reject. omitempty keeps plain-chat messages
	// (no tool_calls) byte-identical on the wire.
	messages := make([]OpenAIMessage, 0, len(request.Messages))
	for _, msg := range request.Messages {
		m := OpenAIMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  toWireSendToolCalls(msg.ToolCalls),
		}
		if msg.Name != "" {
			m.Name = msg.Name
		}
		messages = append(messages, m)
	}
	return &OpenAICompatibleRequest{
		Model:       p.getModelName(request.Model),
		Messages:    messages,
		MaxTokens:   request.MaxTokens,
		Temperature: request.Temperature,
		TopP:        request.TopP,
		Stream:      request.Stream,
		Tools:       request.Tools,
		ToolChoice:  request.ToolChoice,
	}
}

func (p *OpenAICompatibleProvider) convertFromOpenAIResponse(response *OpenAICompatibleResponse, requestID uuid.UUID, processingTime time.Duration) *LLMResponse {
	llmResponse := &LLMResponse{
		ID:        uuid.New(),
		RequestID: requestID,
		Content:   "",
		Usage:     Usage{},
		// The backend reports which concrete model served the request. Carry it
		// through so callers surface the REAL identity instead of echoing the
		// requested alias (CONST-036 / CONST-037). Empty when the backend omits
		// it — consumers fall back to the requested model.
		Model:          response.Model,
		ProcessingTime: processingTime,
		CreatedAt:      time.Now(),
	}

	if len(response.Choices) > 0 {
		choice := response.Choices[0]
		llmResponse.Content = choice.Message.Content
		llmResponse.ToolCalls = parseOpenAIWireToolCalls(choice.Message.ToolCalls)
		llmResponse.FinishReason = choice.FinishReason

		// Capture reasoning_content if present (Xiaomi MiMo deep thinking)
		if choice.Message.ReasoningContent != "" {
			if llmResponse.ProviderMetadata == nil {
				llmResponse.ProviderMetadata = make(map[string]interface{})
			}
			llmResponse.ProviderMetadata["reasoning_content"] = choice.Message.ReasoningContent
		}
		// Round-53 LLMResponse.Err wiring (CONST-035 / Article XI §11.9):
		// the OpenAICompatibleProvider fans out to ~11 backends (VLLM,
		// LMStudio, Jan, LocalAI, FastChat, TextGen WebUI, KoboldAI,
		// GPT4All, TabbyAPI, MLX, MistralRS) — all advertise
		// OpenAI-compatible Chat Completions semantics including
		// `finish_reason: "length" | "stop" | "tool_calls" |
		// "content_filter"`. Reuse the round-46 OpenAI mapper. If any
		// individual backend diverges from this contract in the
		// future, TestRound53_OpenAICompatible_ReusesOpenAIMapper will
		// surface the regression and a backend-specific path MUST be
		// introduced in the same commit.
		llmResponse.Err = mapOpenAIFinishReasonToErr(choice.FinishReason)
	}

	llmResponse.Usage = Usage{
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		TotalTokens:      response.Usage.TotalTokens,
	}

	return llmResponse
}

func (p *OpenAICompatibleProvider) makeAPIRequest(ctx context.Context, request *OpenAICompatibleRequest) (*OpenAICompatibleResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.getAPIURL(p.config.ChatEndpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", RedactEndpointsInError(err))
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", RedactEndpointsInError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response OpenAICompatibleResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

func (p *OpenAICompatibleProvider) makeStreamingRequest(ctx context.Context, request *OpenAICompatibleRequest, ch chan<- LLMResponse, requestID uuid.UUID) error {
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.getAPIURL(p.config.ChatEndpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", RedactEndpointsInError(err))
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", RedactEndpointsInError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Process SSE stream
	for {
		var line string
		line, err = readSSELine(resp.Body)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read SSE line: %w", err)
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var streamResponse struct {
				ID      string                   `json:"id"`
				Object  string                   `json:"object"`
				Created int64                    `json:"created"`
				Model   string                   `json:"model"`
				Choices []OpenAICompatibleChoice `json:"choices"`
			}

			if err := json.Unmarshal([]byte(data), &streamResponse); err != nil {
				continue // Skip malformed JSON
			}

			if len(streamResponse.Choices) > 0 {
				choice := streamResponse.Choices[0]
				response := LLMResponse{
					ID:        uuid.New(),
					RequestID: requestID,
					Content:   choice.Delta.Content,
					// Carry the backend-reported model on EVERY chunk. Without
					// it the wire facade's streaming path has nothing to
					// upgrade from and keeps echoing the requested alias, so a
					// `stream:true` request reports "default" while a concrete
					// model serves it — the same CONST-036 / CONST-037 defect
					// the non-stream path fixes. Empty when the backend omits
					// it; the facade then keeps the requested model.
					Model:     streamResponse.Model,
					CreatedAt: time.Now(),
				}

				// Capture streaming reasoning_content if present (Xiaomi MiMo deep thinking)
				if choice.Delta.ReasoningContent != "" {
					response.ProviderMetadata = map[string]interface{}{
						"reasoning_content": choice.Delta.ReasoningContent,
					}
				}

				select {
				case ch <- response:
				case <-ctx.Done():
					return ctx.Err()
				}

				if choice.FinishReason != "" {
					// Round-53 LLMResponse.Err wiring for the streaming
					// path (CONST-035 / Article XI §11.9): when the
					// final frame carries finish_reason="length"/
					// "content_filter", emit a terminal LLMResponse
					// with Err populated so stream consumers (notably
					// tool_provider.go :201/:251) can distinguish a
					// clean stop from a partial-error stop on any of
					// the 11 OpenAI-compatible local backends fronted
					// by this provider (VLLM, LMStudio, Jan, etc.).
					if errSentinel := mapOpenAIFinishReasonToErr(choice.FinishReason); errSentinel != nil {
						select {
						case ch <- LLMResponse{
							ID:           uuid.New(),
							RequestID:    requestID,
							FinishReason: choice.FinishReason,
							Model:        streamResponse.Model,
							CreatedAt:    time.Now(),
							Err:          errSentinel,
						}:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					break
				}
			}
		}
	}

	return nil
}

func (p *OpenAICompatibleProvider) getModelName(requestedModel string) string {
	if requestedModel != "" {
		return requestedModel
	}

	if p.config.DefaultModel != "" {
		return p.config.DefaultModel
	}

	// Return first available model
	if len(p.models) > 0 {
		return p.models[0].Name
	}

	return "gpt-3.5-turbo" // Fallback default
}

func (p *OpenAICompatibleProvider) getAPIURL(endpoint string) string {
	return strings.TrimSuffix(
		effectiveOpenAICompatibleBaseURL(p.name, p.config.BaseURL), "/") + endpoint
}

// effectiveOpenAICompatibleBaseURL resolves the base URL this provider will
// ACTUALLY dial: the configured value with surrounding whitespace removed,
// falling back to the named backend's well-known LOCAL default when that leaves
// nothing.
//
// It exists because the gate and the dial used to disagree on ONE input. The
// W2c-1 check in NewOpenAICompatibleProvider tested TrimSpace(BaseURL) == "",
// so a whitespace-only BaseURL was judged as the localhost default and PASSED;
// getAPIURL tested BaseURL == "", so the same provider then dialled the literal
// "  " + endpoint. No leak followed — whitespace is not a host and the dial
// simply fails — but "the gate judges a different URL than the one dialled" is
// exactly the asymmetry the sibling KoboldAI gate's own comment argues against,
// and it is a defect whether or not today's inputs make it exploitable.
//
// Trimming (rather than fail-closing on whitespace, the choice KoboldAI makes
// for its own resolution) keeps the two halves symmetric AND repairs the
// malformed request URL: a whitespace-only endpoint now resolves to the local
// default at both sites instead of producing a URL nothing can serve.
func effectiveOpenAICompatibleBaseURL(name, configured string) string {
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		return trimmed
	}
	return defaultLocalBaseURLForName(name)
}

// defaultLocalBaseURLForName returns the well-known LOCAL base URL for a named
// OpenAI-compatible backend when the caller supplied none.
//
// SINGLE SOURCE OF TRUTH, deliberately: both getAPIURL (which builds the URL
// the provider dials) and the W2c-1 cloud-gate check in
// NewOpenAICompatibleProvider (which judges that URL's locality) resolve the
// effective base URL through this ONE table. Duplicating the table would let
// the two drift, and a gate that judges a different URL than the one actually
// dialled is a gate that can be walked past. Every entry is a loopback
// address, so a name-defaulted provider is local by construction.
func defaultLocalBaseURLForName(name string) string {
	switch name {
	case "vllm":
		return "http://localhost:8000"
	case "textgen", "oobabooga":
		return "http://localhost:5000"
	case "lmstudio":
		return "http://localhost:1234"
	case "localai":
		return "http://localhost:8080"
	case "jan":
		return "http://localhost:1337"
	case "koboldai":
		return "http://localhost:5001"
	case "gpt4all":
		return "http://localhost:4891"
	case "tabbyapi":
		return "http://localhost:5000"
	case "fastchat":
		return "http://localhost:7860"
	default:
		return "http://localhost:8080"
	}
}

// recordHealth applies a health verdict to lastHealth under healthMu and
// returns a copy the caller owns outright (HXC-214). The whole verdict lands in
// ONE critical section, so a concurrent reader cannot observe it half-applied.
func (p *OpenAICompatibleProvider) recordHealth(
	status string,
	latency time.Duration,
	adj errCountAdjust,
	modelCount int,
) *ProviderHealth {
	p.healthMu.Lock()
	defer p.healthMu.Unlock()
	return applyProviderHealth(p.lastHealth, status, latency, adj, modelCount)
}

// Helper functions for model capabilities

func (p *OpenAICompatibleProvider) inferContextSize(modelName string) int {
	modelName = strings.ToLower(modelName)

	// Common context sizes based on model names
	if strings.Contains(modelName, "32k") || strings.Contains(modelName, "32k") {
		return 32768
	}
	if strings.Contains(modelName, "16k") {
		return 16384
	}
	if strings.Contains(modelName, "8k") {
		return 8192
	}
	if strings.Contains(modelName, "gpt-4") {
		return 8192
	}
	if strings.Contains(modelName, "claude") {
		return 100000
	}
	if strings.Contains(modelName, "llama") {
		return 4096
	}

	return 4096 // Default
}

func (p *OpenAICompatibleProvider) inferMaxTokens(modelName string) int {
	contextSize := p.inferContextSize(modelName)
	return contextSize / 2 // Conservative estimate
}

func (p *OpenAICompatibleProvider) supportsTools(modelName string) bool {
	modelName = strings.ToLower(modelName)

	// Most modern models support tools
	return strings.Contains(modelName, "gpt-4") ||
		strings.Contains(modelName, "claude-3") ||
		strings.Contains(modelName, "llama-3") ||
		strings.Contains(modelName, "mistral")
}

func (p *OpenAICompatibleProvider) supportsVision() bool {
	// Most modern providers support vision
	return p.name == "lmstudio" || p.name == "jan" || p.name == "textgen"
}

func (p *OpenAICompatibleProvider) supportsVisionModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	return strings.Contains(modelName, "vision") ||
		strings.Contains(modelName, "multimodal") ||
		strings.Contains(modelName, "clip") ||
		strings.Contains(modelName, "llava")
}

// readSSELine reads a line from Server-Sent Events stream
func readSSELine(r io.Reader) (string, error) {
	var line []byte
	buf := make([]byte, 1)

	for {
		n, err := r.Read(buf)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}

		if buf[0] == '\n' {
			break
		}

		if buf[0] != '\r' {
			line = append(line, buf[0])
		}
	}

	return string(line), nil
}
