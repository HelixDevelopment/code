package llm

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
)

// provider_factory.go (P1-F12-T07): unified construction + selection for the
// four cloud backends covered by Feature 12 — Anthropic, Bedrock, Vertex AI,
// Azure OpenAI. The pre-existing NewProvider() in factory.go is the
// catch-all factory across every Provider type (cloud + local + OpenAI-
// compatible). NewCloudProvider() narrows scope to the cloud quartet so
// the wizard / selector path can refuse to construct anything outside that
// universe — and so a misconfigured "ollama" string in --provider does not
// silently fall through to a local backend.

// ErrNoProviderConfigured is returned by Select when none of the four sources
// (flag / env / config / wizard-default) yielded a value. Callers running in
// interactive mode should launch the wizard on this error; non-interactive
// callers should surface it to the user with a remediation hint.
var ErrNoProviderConfigured = errors.New(
	"no provider configured: pass --provider, set HELIX_LLM_PROVIDER, " +
		"populate provider in config, or run `helixcode wizard`")

// ErrCloudDisabled is returned by NewCloudProvider when a hosted (cloud)
// provider is requested while the cloud gate is closed. The gate is the W2c-1
// local-only-serving control (operator mandate 2026-09-05): config key
// llm.cloud.enabled, default FALSE. Local types (Ollama, LlamaCpp) are
// exempt — the gate exists to keep serving local-by-default, not to break
// local routes.
var ErrCloudDisabled = errors.New(
	"cloud LLM providers are disabled by configuration")

// cloudGate is the process-wide cloud gate state. Zero value = closed, which
// is the mandated default (llm.cloud.enabled defaults false); startup code
// (internal/server.New, cmd's generate path) opens it via SetCloudEnabled
// when the operator has explicitly enabled cloud providers in config. An
// atomic rather than a plain bool so a gate flip cannot tear a concurrent
// construction.
var cloudGate atomic.Bool

// SetCloudEnabled wires the cloud gate from configuration. Called once at
// process startup with cfg.LLM.Cloud.Enabled.
func SetCloudEnabled(enabled bool) {
	cloudGate.Store(enabled)
}

// CloudEnabled reports the current cloud gate state for status surfacing
// (the /api/v1/llm/providers listing reports it alongside provider status).
func CloudEnabled() bool {
	return cloudGate.Load()
}

// isLocalProviderType reports whether t is one of the two local construction
// types handled by NewCloudProvider — these are exempt from the cloud gate.
func isLocalProviderType(t ProviderType) bool {
	return t == ProviderTypeOllama || t == ProviderTypeLlamaCpp
}

// isLocalEndpointURL reports whether rawURL addresses a LOCAL inference
// endpoint — the locality half of the W2c-1 cloud gate. Companion to
// isLocalProviderType above: that predicate keys on the PROVIDER IDENTITY
// (Ollama / LlamaCpp), this one keys on the ENDPOINT the provider will
// actually dial.
//
// Endpoint locality — not provider identity — is the only workable test for
// the OpenAI-compatible constructor, because the SAME constructor serves the
// local HelixLLM coder (http://localhost:18434), llama.cpp, vLLM, LM Studio
// and LocalAI AND every hosted OpenAI-compatible catalogue provider (Cerebras,
// Together, Fireworks, Novita, …). Gating that constructor on a provider-name
// list, or blanket-refusing it, would break local serving — the exact opposite
// of what the local-only-serving mandate (operator mandate 2026-09-05) asks
// for.
//
// Treated as LOCAL:
//   - loopback: 127.0.0.0/8, ::1, host "localhost", and any "*.localhost"
//   - unspecified: 0.0.0.0, ::
//   - RFC1918 private: 10/8, 172.16/12, 192.168/16
//   - link-local: 169.254/16, fe80::/10
//   - CGNAT shared address space: 100.64/10 (RFC 6598)
//   - IPv6 unique-local: fc00::/7 (RFC 4193)
//   - a bare hostname with no dots — a LAN short name ("coder", "gpu-box")
//   - RFC 6762 mDNS link-local names ("gpu-box.local") and RFC 8375
//     home-network names ("nas.home.arpa") — reserved namespaces that are not
//     resolvable on the public Internet
//
// DELIBERATE JUDGEMENT CALL: a LAN-hosted inference server (192.168.x.y, or a
// bare "gpu-box" short name) counts as LOCAL under this gate. The mandate is
// about not reaching HOSTED CLOUD SERVICES, not about never leaving this
// machine — an operator's own GPU box on their own network IS their local
// serving infrastructure, and refusing it would push operators to disable the
// gate wholesale, which is strictly worse for the mandate than admitting the
// LAN. Anything routable on the public Internet is REMOTE.
//
// Fails CLOSED: an empty, blank, or unparseable URL — or one that parses with
// no host — is NOT local. An endpoint we cannot positively identify as local is
// treated as remote, so a malformed value can never smuggle a hosted provider
// past the gate. That includes an IP-LITERAL SHAPE that fails to parse: it is
// refused outright rather than handed to the bare-hostname rule, which is only
// meant to judge DNS-style short names. Parsing is done with net/url +
// netip.ParseAddr (netip, not net.ParseIP, because net.ParseIP rejects every
// RFC 6874 zoned address — see the call site); substring-matching on
// "localhost" is deliberately NOT used (it would accept
// "https://localhost.evil.example.com").
func isLocalEndpointURL(rawURL string) bool {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return false
	}
	// Accept scheme-less input ("localhost:18434", "192.168.1.5:8000"):
	// url.Parse would otherwise read "localhost" as the SCHEME and leave Host
	// empty, so a legitimately-local scheme-less endpoint would fail closed and
	// break local serving.
	//
	// SHARED HEURISTIC, DIFFERENT FAILURE DIRECTIONS — read before "fixing"
	// this line. RedactEndpointForMessage (openai_compatible_provider.go) once
	// carried the identical unanchored strings.Contains(raw, "://") test, and
	// there it was a real bypass: two credential-bearing shapes
	// ("//user:pw@host/v1"; "user:pw@host/r?to=https://x") skipped the prefix,
	// url.Parse saw no authority, and the value was echoed VERBATIM into an
	// error — the function FAILED OPEN, so it was anchored to
	// endpointSchemeRe there.
	//
	// HERE the same imprecision fails CLOSED and is therefore left alone. If
	// this test wrongly skips the prefix, url.Parse yields an empty Hostname(),
	// the host == "" branch below returns false, the endpoint is judged REMOTE
	// and the gate REFUSES. A misread value can only ever cost a false refusal
	// of a local endpoint, never admit a hosted one — the safe direction for a
	// security gate. Anchoring it would be a behaviour change to the locality
	// predicate (which endpoints construct at all), not a leak fix, so it is
	// deliberately out of scope for the CONST-042 work.
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	// Hostname() strips the port and the IPv6 brackets ("[::1]:8080" → "::1").
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	// RESERVED NON-ROUTABLE NAMESPACES. Both suffixes below are reserved by
	// RFC for names that are, by specification, NOT resolvable on the public
	// Internet — so a name in either can no more reach a hosted cloud service
	// than 192.168.0.0/16 can, and the same judgement the block above applies
	// to ".localhost" applies to them.
	//
	//   - ".local" (RFC 6762 §3) is reserved for link-local multicast DNS. It
	//     is the DEFAULT LAN name of every macOS and every Avahi/systemd-
	//     resolved host, so "http://gpu-box.local:8000" is the single most
	//     ordinary way an operator names their own GPU box. Classifying it
	//     REMOTE was a false refusal (§11.4.201) of a genuinely local endpoint
	//     — and precisely the outcome the DELIBERATE JUDGEMENT CALL documented
	//     above warns against, since an operator refused their own LAN box is
	//     pushed toward disabling the gate wholesale, which is strictly worse
	//     for the mandate than admitting the LAN.
	//   - ".home.arpa" (RFC 8375) is the reserved special-use name for
	//     home/residential networks, serving the same role for router-managed
	//     LANs. Both the zone apex and names under it are covered, mirroring
	//     how "localhost" itself is matched exactly as well as by suffix.
	//
	// Scope is exactly one label-suffix each: matching is on ".local" as a
	// SUFFIX, so a routable lookalike like "local.example.com" or
	// "mylocal.example.com" ends in ".com" and stays REMOTE. host is already
	// lower-cased above, so "GPU-BOX.LOCAL" matches too. A trailing-dot FQDN
	// ("gpu-box.local.") does NOT match — that is pre-existing behaviour of
	// this predicate for every name form, pinned by the locality table rather
	// than changed here.
	if strings.HasSuffix(host, ".local") ||
		host == "home.arpa" || strings.HasSuffix(host, ".home.arpa") {
		return true
	}
	// netip.ParseAddr, NOT net.ParseIP: net.ParseIP rejects EVERY zoned address
	// outright (stdlib net/ip.go parseIP — `if err != nil || ip.Zone() != ""`
	// returns invalid), while net/url PRESERVES an RFC 6874 %25-escaped zone in
	// Hostname(). A public zoned literal such as "2606:4700:4700::1111%eth0"
	// therefore used to yield nil here, fall through to the bare-hostname branch
	// below, contain no dot, and be verdicted LOCAL — constructing a hosted
	// provider with the gate CLOSED. Parsing with netip and stripping the zone
	// keeps the address-family rules keyed on the ADDRESS, never on the
	// interface name attached to it. AsSlice() yields 4 bytes for an IPv4
	// address and 16 for IPv6 (IPv4-mapped forms included), both of which
	// net.IP's IsLoopback/IsPrivate/IsLinkLocalUnicast handle natively.
	if addr, err := netip.ParseAddr(host); err == nil {
		return isLocalIP(net.IP(addr.WithZone("").AsSlice()))
	}
	// An IP-LITERAL SHAPE that failed to parse must fail CLOSED here rather than
	// reach the bare-hostname branch, which was never meant to receive one: a
	// colon is the IPv6 group separator and '%' the RFC 6874 zone delimiter, so
	// a malformed literal carrying either always answers "no dot" and would be
	// admitted as a LAN short name. This restores the documented contract above
	// — "an unparseable URL is NOT local".
	if strings.ContainsAny(host, ":%") {
		return false
	}
	// DOTLESS IPv4 SHAPES must fail CLOSED before the bare-hostname branch
	// below, which would otherwise admit them: an IPv4 address needs no dots,
	// so "no dot ⇒ LAN name" is not sound on its own. Measured against libc
	// getaddrinfo on the host this guard was written on:
	//
	//	134744072    -> 8.8.8.8      (decimal-integer form  — PUBLIC)
	//	0x08080808   -> 8.8.8.8      (hex form              — PUBLIC)
	//	2130706433   -> 127.0.0.1    (decimal loopback)
	//	gpu-box      -> UNRESOLVED   (control: an ordinary dotless LAN name)
	//
	// Both PUBLIC spellings were previously verdicted LOCAL and walked straight
	// past the gate. Go's own PURE resolver rejects these forms, so the dial
	// fails as things stand — but under the cgo resolver (GODEBUG=netdns=cgo, a
	// macOS build with cgo, an nsswitch configuration that forces cgo) Go calls
	// getaddrinfo and the address resolves. That makes this latent rather than
	// live, and a real bypass of a security control either way, so the refusal
	// keys on the SHAPE rather than on what happens to resolve in one runtime
	// configuration.
	//
	// The trade, stated plainly: this also refuses 2130706433, a legitimate —
	// if bizarre — spelling of loopback. Fail-closed is the correct direction
	// for a gate, nobody writes 127.0.0.1 that way, and admitting the shape at
	// all is precisely what re-opens the bypass. host is already lower-cased
	// above, so an uppercase "0X" prefix is covered by the same test.
	if isAllDigitLabel(host) || strings.HasPrefix(host, "0x") {
		return false
	}
	// A non-IP hostname. A bare short name with no dot is a LAN name resolved
	// via mDNS / NetBIOS / /etc/hosts — local. Anything dotted is a DNS name
	// that can resolve anywhere on the public Internet — remote.
	return !strings.Contains(host, ".")
}

// isAllDigitLabel reports whether s is non-empty and entirely ASCII digits —
// the decimal-integer spelling of an IPv4 address ("134744072"). Written out
// rather than delegated to strconv so it cannot silently accept the sign,
// underscore-separator and base-prefix forms strconv.ParseUint tolerates, and
// so it never depends on the value FITTING a uint32: the point is the shape a
// resolver may interpret as an address, not a successful conversion.
func isAllDigitLabel(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// cgnatBlock is RFC 6598 shared address space (100.64.0.0/10). net.IP.IsPrivate
// does NOT cover it, but a carrier-grade-NAT / LAN-appliance address is not a
// hosted cloud endpoint, so the gate treats it as local for the same reason it
// treats RFC1918 as local. Parsed once at init; the CIDR literal cannot fail to
// parse, and isLocalIP nil-guards the result rather than assuming that.
var cgnatBlock = func() *net.IPNet {
	_, block, err := net.ParseCIDR("100.64.0.0/10")
	if err != nil {
		return nil
	}
	return block
}()

// isLocalIP applies the address-family rules documented on isLocalEndpointURL
// to an already-parsed IP. Split out so each CIDR judgement is readable and
// independently exercised by the locality table test.
func isLocalIP(ip net.IP) bool {
	switch {
	case ip.IsLoopback(): // 127.0.0.0/8, ::1
		return true
	case ip.IsUnspecified(): // 0.0.0.0, ::
		return true
	case ip.IsPrivate(): // 10/8, 172.16/12, 192.168/16, fc00::/7
		return true
	case ip.IsLinkLocalUnicast(): // 169.254/16, fe80::/10
		return true
	case cgnatBlock != nil && cgnatBlock.Contains(ip): // 100.64/10 (RFC 6598)
		return true
	default:
		return false
	}
}

// SelectorInput captures the four sources of provider-type selection in the
// precedence order the Selector applies (flag > env > config > wizard).
// Each field is the raw, untrimmed string the caller obtained from that
// source. Empty strings are treated as "source did not provide a value" and
// the next source is consulted.
type SelectorInput struct {
	// Flag is the CLI flag value (e.g., --provider=bedrock). Highest
	// precedence — explicit user instruction for this invocation.
	Flag string

	// Env is the value of HELIX_LLM_PROVIDER (or whatever the caller has
	// chosen as its env var). Mid precedence — runtime override without
	// rewriting config.
	Env string

	// Config is the value loaded from a persisted config file
	// (e.g., $XDG_CONFIG_HOME/helixcode/config.yaml's provider field).
	// Lowest precedence among "user-provided" sources.
	Config string
}

// Select resolves the cloud ProviderType to use using flag > env > config
// precedence. It returns ErrNoProviderConfigured (sentinel, errors.Is-able)
// when every source is empty — at which point the caller should launch the
// interactive wizard if running interactively. Unknown/unsupported provider
// strings return a non-sentinel error so the caller can distinguish the
// "needs wizard" case from "user typed garbage".
//
// The Selector is pure: no env reads, no file IO, no construction. It is
// intentionally trivial so it can be exercised exhaustively by unit tests.
func Select(input SelectorInput) (ProviderType, error) {
	raw := firstNonEmpty(input.Flag, input.Env, input.Config)
	if raw == "" {
		return "", ErrNoProviderConfigured
	}
	return parseCloudProviderType(raw)
}

// NewCloudProvider constructs the concrete cloud Provider for the given
// resolved ProviderType using cfg. It only handles the four Feature-12
// cloud backends. Local / OpenAI-compatible / hosted-OpenAI providers are
// rejected — callers needing those should use NewProvider() in factory.go.
//
// Returned Provider already implements the full Provider interface; any
// credential / endpoint resolution failures bubble up from the underlying
// New<X>Provider constructor. Per the audits in T03–T06, all four
// constructors defer credential validation where the SDK permits it, so
// this function can succeed in offline / no-creds environments and let
// runtime calls surface real auth errors (anti-bluff: no fake-success
// "available" status when nothing actually works).
func NewCloudProvider(t ProviderType, cfg ProviderConfigEntry) (Provider, error) {
	// Normalise cfg.Type to match t so downstream provider code that reads
	// cfg.Type sees a coherent value even if the caller passed a config
	// loaded from a different source.
	cfg.Type = t

	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving): hosted provider construction refuses while the gate is
	// closed — even if API keys are present in the environment — unless the
	// operator has explicitly set llm.cloud.enabled: true. Local types are
	// exempt. Checked BEFORE any per-type switch arm so no hosted arm can
	// construct by a back door.
	if !cloudGate.Load() && !isLocalProviderType(t) {
		return nil, fmt.Errorf("%w: llm.cloud.enabled is false (default); "+
			"hosted provider %q will not be constructed. Set "+
			"llm.cloud.enabled: true to permit cloud providers, or use the "+
			"local routes (local/helixllm coder, llamacpp, ollama)",
			ErrCloudDisabled, t)
	}

	switch t {
	case ProviderTypeAnthropic:
		return providerOrNil(NewAnthropicProvider(cfg))
	case ProviderTypeBedrock:
		return providerOrNil(NewBedrockProvider(cfg))
	case ProviderTypeVertexAI:
		return providerOrNil(NewVertexAIProvider(cfg))
	case ProviderTypeAzure:
		return providerOrNil(NewAzureProvider(cfg))
	case ProviderTypeGroq:
		return providerOrNil(NewGroqProvider(cfg))
	case ProviderTypeOpenAI:
		return providerOrNil(NewOpenAIProvider(cfg))
	case ProviderTypeGemini:
		return providerOrNil(NewGeminiProvider(cfg))
	case ProviderTypeOpenRouter:
		return providerOrNil(NewOpenRouterProvider(cfg))
	case ProviderTypeXAI:
		return providerOrNil(NewXAIProvider(cfg))
	case ProviderTypeQwen:
		return providerOrNil(NewQwenProvider(cfg))
	case ProviderTypeCopilot:
		return providerOrNil(NewCopilotProvider(cfg))
	case ProviderTypeMistral:
		return providerOrNil(NewMistralProvider(cfg))
	case ProviderTypeDeepSeek:
		return providerOrNil(NewDeepSeekProvider(cfg))
	case ProviderTypeOllama:
		return newOllamaFromEntry(cfg)
	case ProviderTypeLlamaCpp:
		return newLlamaCPPFromEntry(cfg)
	case ProviderTypeReplicate:
		return providerOrNil(NewReplicateProvider(cfg))
	default:
		return nil, fmt.Errorf(
			"NewCloudProvider: %q is not a cloud provider type (supported: %s)",
			t, supportedCloudProviderList())
	}
}

// firstNonEmpty returns the first non-empty (after trim) string in args.
func firstNonEmpty(args ...string) string {
	for _, a := range args {
		if strings.TrimSpace(a) != "" {
			return a
		}
	}
	return ""
}

// parseCloudProviderType normalises the raw user-provided string into a
// canonical cloud ProviderType. Returns a non-sentinel error for unknown
// values so callers can distinguish "no source" (ErrNoProviderConfigured)
// from "user typed garbage".
//
// Anti-bluff (CONST-035): if the user typed a name that IS a known
// provider type but NOT one of F12's four direct-cloud backends (e.g.
// "groq", "openai", "gemini", "deepseek", "xai", "openrouter",
// "mistral", "ollama", "llamacpp"), surface a directed error that
// names the right path (server-mediated provider manager) rather than
// the generic "unknown cloud provider" message — which previously
// implied those providers aren't supported at all.
func parseCloudProviderType(raw string) (ProviderType, error) {
	norm := strings.ToLower(strings.TrimSpace(raw))
	switch norm {
	case "anthropic":
		return ProviderTypeAnthropic, nil
	case "bedrock", "aws", "aws-bedrock":
		return ProviderTypeBedrock, nil
	case "vertexai", "vertex", "vertex-ai", "gcp", "gcp-vertex":
		return ProviderTypeVertexAI, nil
	case "azure", "azure-openai", "azureopenai":
		return ProviderTypeAzure, nil
	case "groq":
		return ProviderTypeGroq, nil
	case "openai", "open-ai":
		return ProviderTypeOpenAI, nil
	case "gemini", "google":
		return ProviderTypeGemini, nil
	case "openrouter", "open-router":
		return ProviderTypeOpenRouter, nil
	case "xai", "grok":
		return ProviderTypeXAI, nil
	case "qwen":
		return ProviderTypeQwen, nil
	case "copilot", "github-copilot":
		return ProviderTypeCopilot, nil
	case "mistral":
		return ProviderTypeMistral, nil
	case "deepseek":
		return ProviderTypeDeepSeek, nil
	case "ollama":
		return ProviderTypeOllama, nil
	case "llamacpp", "llama-cpp", "llama.cpp":
		return ProviderTypeLlamaCpp, nil
	case "replicate":
		return ProviderTypeReplicate, nil
	case "vllm", "localai", "lmstudio":
		return "", fmt.Errorf(
			"provider %q is supported by HelixCode but not via the F12 direct-cloud-provider CLI path "+
				"(supported direct-cloud backends: %s). "+
				"Configure %q in HelixCode/config/config.yaml under llm.providers: and access it via the HelixCode server "+
				"(see docs/user_manual/ZERO_BLUFF_USER_MANUAL.md §2.4 'LLM Providers (F12)'). "+
				"The full provider list per CONST-039 is in docs/llms_verifier/.",
			raw, supportedCloudProviderList(), raw)
	default:
		return "", fmt.Errorf(
			"unknown provider %q (F12 direct-cloud supports: %s; "+
				"the full HelixCode provider catalogue per CONST-039 is accessed via the server-side provider manager — "+
				"see docs/user_manual/ZERO_BLUFF_USER_MANUAL.md §2.4)",
			raw, supportedCloudProviderList())
	}
}

// supportedCloudProviderList returns a stable, human-readable list of the
// canonical direct-cloud-provider names for error messages. Expanded from
// the original 4 (anthropic/bedrock/vertexai/azure) to also cover Groq +
// OpenAI in round-41-continued, closing the most-common readiness gap
// for users who expect modern-CLI-agent parity (just plug in API key
// and go) with the two most-used cloud providers beyond the original
// four. Other providers still require server-mediated config.yaml setup
// — see ZERO_BLUFF_USER_MANUAL.md §2.4 Path A vs Path B.
func supportedCloudProviderList() string {
	return "anthropic, bedrock, vertexai, azure, groq, openai, gemini, openrouter, xai, qwen, copilot, mistral, deepseek, ollama, llamacpp, replicate"
}

// newOllamaFromEntry adapts the generic ProviderConfigEntry into the
// OllamaConfig that NewOllamaProvider expects. Defaults match the
// constructor's own defaults (BaseURL http://localhost:11434, 30s
// timeout, stream enabled). Endpoint from the entry overrides
// BaseURL. Per CONST-035: the adapter never silently swallows a
// construction error; failures bubble up to the caller.
func newOllamaFromEntry(cfg ProviderConfigEntry) (Provider, error) {
	oc := OllamaConfig{
		BaseURL:       cfg.Endpoint,
		StreamEnabled: true,
	}
	if oc.BaseURL == "" {
		oc.BaseURL = "http://localhost:11434"
	}
	if len(cfg.Models) > 0 {
		oc.DefaultModel = cfg.Models[0]
	}
	return providerOrNil(NewOllamaProvider(oc))
}

// newLlamaCPPFromEntry adapts ProviderConfigEntry into LlamaConfig.
// ServerHost comes from cfg.Endpoint when present, otherwise the
// constructor's default-localhost path applies. Model defaults to
// cfg.Models[0] if supplied. Anti-bluff: failures bubble up.
func newLlamaCPPFromEntry(cfg ProviderConfigEntry) (Provider, error) {
	lc := LlamaConfig{
		ServerHost: cfg.Endpoint,
	}
	if len(cfg.Models) > 0 {
		lc.Model = cfg.Models[0]
	}
	return providerOrNil(NewLlamaCPPProvider(lc))
}

// ParseCloudProviderType is the exported counterpart of parseCloudProviderType.
// Callers outside this package (e.g., the wizard cobra subcommand) use it to
// normalise user-supplied --provider strings without re-implementing the
// alias table here. Returns the same non-sentinel error on unknown input.
func ParseCloudProviderType(raw string) (ProviderType, error) {
	return parseCloudProviderType(raw)
}
