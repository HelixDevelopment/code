package helixagent

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.helix.code/internal/llm"
)

// health_redaction_test.go — CONST-042 / Article XII §12.1 standing guard
// (§11.4.135) against a configured endpoint's CREDENTIAL riding out of this
// adapter in an operator-facing string.
//
// WHY THIS FILE IS SHAPED THIS WAY — the round-6 BLOCKING finding, recorded so
// the lesson survives the fix.
//
// The first version of this guard drove ONE site (GetHealth) with ONE shape
// (`svc-user:s3cr3tpw@host`) and PASSED. It passed for a reason that does not
// generalise: net/http runs a request URL through its own stripPassword before
// storing it in the *url.Error it returns, and stripPassword masks the PASSWORD
// component and nothing else. The chosen fixture happened to have a password.
// The ordinary machine shape for a bearer credential —
// `https://<token>@host`, where the credential IS the username and there is no
// password at all — was echoed IN FULL, in the very same message the guard was
// reading, alongside the base URL the guard had just proved was redacted.
//
// Worse: that exact shape was ALREADY written down as must-cover in this
// batch's sibling table (../../endpoint_redaction_test.go, case
// `token_as_username_no_password`). A shape the batch had declared was simply
// absent from this table, and nothing made that absence visible.
//
// So this file is keyed SITE × SHAPE, and:
//
//   - Every transport-bearing method of the adapter is a SITE. A leak carrier
//     is a property of the TRANSPORT, not of one method, so a new method that
//     talks to the server inherits the whole corpus of known shapes.
//   - TestShapeTable_CoversEveryShapeTheSiblingTableDeclares parses the sibling
//     table's source and FAILS if a shape declared there is missing here. That
//     is the mechanical fix for the actual defect: the absence is now loud.
//
// §11.4.115 polarity: RED_MODE is NOT implemented here, for the reason the
// sibling file records — the defect is the ABSENCE of a redaction call, and no
// runtime input re-creates it on fixed source. The RED evidence was captured by
// running this matrix against the pre-fix artifact (every no-password shape
// leaking at every site; the password shape passing, which is the bluff) and
// by the paired §1.1 mutation that removes the redaction call and re-runs it.

// redactionSite is ONE method of this adapter that performs a transport call
// and surfaces the resulting error to an operator. It returns the
// operator-facing MESSAGE (empty when the method has none) and the returned
// error; the assertions below check BOTH, because GetHealth interpolates the
// error into its message with %v, so either alone is an incomplete carrier.
type redactionSite struct {
	name  string
	drive func(ctx context.Context, p *Provider) (string, error)
}

func redactionSites() []redactionSite {
	req := func() *llm.LLMRequest {
		return &llm.LLMRequest{Messages: []llm.Message{{Role: "user", Content: "ping"}}}
	}
	return []redactionSite{
		{
			name: "GetHealth",
			drive: func(ctx context.Context, p *Provider) (string, error) {
				h, err := p.GetHealth(ctx)
				if h == nil {
					return "", err
				}
				return h.Message, err
			},
		},
		{
			name: "Generate",
			drive: func(ctx context.Context, p *Provider) (string, error) {
				_, err := p.Generate(ctx, req())
				return "", err
			},
		},
		{
			name: "GenerateStream",
			drive: func(ctx context.Context, p *Provider) (string, error) {
				ch := make(chan llm.LLMResponse, 8)
				drained := make(chan struct{})
				go func() {
					for range ch {
					}
					close(drained)
				}()
				err := p.GenerateStream(ctx, req(), ch)
				<-drained // GenerateStream closes ch; never leak the drain goroutine.
				return "", err
			},
		},
		{
			name: "fetchModels",
			drive: func(ctx context.Context, p *Provider) (string, error) {
				// The REAL site. GetModels swallows this error and returns nil,
				// so driving GetModels would assert on nothing.
				_, err := p.fetchModels(ctx)
				return "", err
			},
		},
		{
			name: "Embed",
			drive: func(ctx context.Context, p *Provider) (string, error) {
				_, err := p.Embed(ctx, []string{"ping"})
				return "", err
			},
		},
	}
}

// redactionShape is ONE way a credential can sit in a configured base URL.
//
// Hosts are pinned to 127.0.0.1:1 — reserved and never listening — so every
// case fails immediately with no DNS lookup, no outbound packet and no timeout
// wait. The SHAPE is what the case is about; the host is incidental.
type redactionShape struct {
	name     string
	endpoint string
	// mustNotHave lists every substring that must be absent from the
	// operator-facing text — the credential, plus (N4) any artefact that would
	// show redaction corrupted a value it should not have touched.
	mustNotHave []string
	// mustHave keeps redaction honest in the other direction: an operator has
	// to still be able to tell WHICH endpoint is misconfigured.
	mustHave []string
	// verbatim, when non-empty, must appear UNCHANGED — the credential-free
	// control, proving redaction does not rewrite values it need not touch.
	verbatim string
	// localOnlyReason documents a shape that has no counterpart in the sibling
	// table. Empty means the shape is expected to exist in both.
	localOnlyReason string
}

func redactionShapes() []redactionShape {
	return []redactionShape{
		{
			// The shape that PASSED on broken source: net/http's stripPassword
			// masks the PASSWORD, so it proves nothing about the general case.
			// Kept precisely so the contrast with the next row stays visible.
			//
			// ROUND-7 BLOCKER A1: it passed for a SECOND reason nobody had
			// written down — it only ever forbade the password. stripPassword
			// preserves the USERNAME, and so did our own helper, so the
			// username rode out of every one of these sites while this row
			// stayed green. It now forbids both components.
			name:        "user_and_password",
			endpoint:    "https://svc-user:s3cr3tpw@127.0.0.1:1/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-user"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// ROUND-7 BLOCKER A1. `curl -u sk_test_xxx:` is Stripe's own
			// published form: a live key in the USERNAME slot with an EMPTY
			// password. Password() reports ("", true), so the password-less
			// masking branch was skipped and Redacted() printed the key.
			name:        "key_as_username_empty_password",
			endpoint:    "https://sk_test_4eC39HqLyjWDarjtT1zdp7dc:@127.0.0.1:1/v1",
			mustNotHave: []string{"sk_test_4eC39HqLyjWDarjtT1zdp7dc"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// Same convention, dummy password instead of an empty one.
			name:        "key_as_username_dummy_password",
			endpoint:    "https://sk-live-dummypw-9876543210:x@127.0.0.1:1/v1",
			mustNotHave: []string{"sk-live-dummypw-9876543210"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// Round-6 BLOCKER B1. No password exists, so nothing in the
			// stdlib masks it; the credential is the username and rode out
			// in full.
			name:        "token_as_username_no_password",
			endpoint:    "https://sk-live-abcdef0123456789@127.0.0.1:1/v1",
			mustNotHave: []string{"sk-live-abcdef0123456789"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// Percent-encoded userinfo: the operator's credential contains a
			// character that must be escaped in a URL. Still password-less,
			// so still unmasked by the stdlib.
			// ROUND-7 A1 sweep: forbade only the credential's SUFFIX.
			name:        "percent_encoded_userinfo",
			endpoint:    "https://sk%40live-abcdef0123456789@127.0.0.1:1/v1",
			mustNotHave: []string{"sk%40live-abcdef0123456789", "sk@live-abcdef0123456789", "abcdef0123456789"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// ROUND-7 A1 sweep: the username slot was `svc`/`user`, a token too
			// generic to assert absent without risking a false failure on
			// unrelated message text. Renamed to a distinctive value so BOTH
			// components of the credential are actually asserted.
			name:        "scheme_less_with_password",
			endpoint:    "svc-acct-nz:hunter2@127.0.0.1:1",
			mustNotHave: []string{"hunter2", "svc-acct-nz"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "protocol_relative_with_password",
			endpoint:    "//svc-acct-nz:s3cr3tpw@127.0.0.1:1/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "scheme_less_with_scheme_shaped_query_param",
			endpoint:    "svc-acct-nz:s3cr3tpw@127.0.0.1:1/r?to=https://x",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "credential_in_query_parameter",
			endpoint:    "https://127.0.0.1:1/v1?api_key=s3cr3tpw",
			mustNotHave: []string{"s3cr3tpw"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "credential_in_fragment",
			endpoint:    "https://127.0.0.1:1/v1#token=s3cr3tpw",
			mustNotHave: []string{"s3cr3tpw"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "credential_in_query_alternate_keys",
			endpoint:    "https://127.0.0.1:1/v1?pass=s3cr3tA&passphrase=s3cr3tB&bearer=s3cr3tC&jwt=s3cr3tD&cred=s3cr3tE",
			mustNotHave: []string{"s3cr3tA", "s3cr3tB", "s3cr3tC", "s3cr3tD", "s3cr3tE"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// The three N3 rows carry NO mustHave: the honest answer for a
			// value whose credential position cannot be identified is the
			// fail-closed placeholder, which names no host.
			name:        "at_before_path_no_userinfo_single_slash",
			endpoint:    "http:/svc-acct-nz:s3cr3tpw@127.0.0.1:1/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
			localOnlyReason: "the sibling table drives GATE REFUSALS and this value is not " +
				"refused: isLocalEndpointURL verdicts it LOCAL because the \"://\"-less " +
				"prefixing makes net/url read \"http\" as the hostname. It reaches the " +
				"transport here, so it belongs in THIS table; the sibling covers it in " +
				"TestRedactEndpointForMessage_UnresolvedUserinfoMarkerIsNotEchoed instead.",
		},
		{
			// ROUND-7 A1 sweep: NOT a half-assertion — no userinfo exists
			// here, which is the whole point of the row.
			name:        "at_in_path_after_host",
			endpoint:    "https://127.0.0.1:1/x:s3cr3tpw@extra/v1",
			mustNotHave: []string{"s3cr3tpw"},
		},
		{
			name:        "scheme_shaped_prefix_with_digit",
			endpoint:    "1http://svc-acct-nz:s3cr3tpw@127.0.0.1:1/x",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
		},
		{
			// ROUND-7 A2. The request-build carrier, covered DELIBERATELY.
			//
			// A non-numeric port makes url.Parse reject the URL, so
			// http.NewRequestWithContext fails at every site before any
			// packet is sent. The mustHave below is the point of the row: it
			// asserts the error really is the PARSE error (only NewRequest
			// produces "invalid port" here), so this row cannot silently
			// start passing through the dial path instead and leave the
			// request-build carrier unexercised again.
			name:        "invalid_port_with_token",
			endpoint:    "https://sk-live-invalidport-t0k3n@127.0.0.1:notaport/v1",
			mustNotHave: []string{"sk-live-invalidport-t0k3n"},
			mustHave:    []string{"invalid port"},
		},
		{
			name:        "credential_in_fragment_with_escaped_sibling",
			endpoint:    "https://127.0.0.1:1/v1#token=s3cr3tpw&path=%2Fx",
			mustNotHave: []string{"s3cr3tpw", "%252F"},
			mustHave:    []string{"127.0.0.1", "%2Fx"},
		},
		{
			// The other direction. Redaction that quietly rewrites every
			// ordinary endpoint would degrade every ordinary diagnostic for a
			// leak that is not present.
			name:            "credential_free_control",
			endpoint:        "http://127.0.0.1:1/v1",
			mustNotHave:     nil,
			mustHave:        []string{"127.0.0.1"},
			verbatim:        "http://127.0.0.1:1/v1",
			localOnlyReason: "the sibling table covers the credential-free case in its own dedicated byte-identity test rather than as a table row",
		},
	}
}

// TestTransportSites_RedactEndpointCredential drives EVERY site with EVERY
// shape.
func TestTransportSites_RedactEndpointCredential(t *testing.T) {
	for _, site := range redactionSites() {
		for _, shape := range redactionShapes() {
			t.Run(site.name+"/"+shape.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				msg, err := site.drive(ctx, New(shape.endpoint))
				if err == nil {
					t.Fatalf("premise broken: %s against %q was expected to fail "+
						"(127.0.0.1:1 refuses immediately, and the malformed shapes are "+
						"rejected before any packet); got a nil error",
						site.name, shape.endpoint)
				}
				combined := msg + "\n" + err.Error()

				for _, forbidden := range shape.mustNotHave {
					if strings.Contains(combined, forbidden) {
						t.Errorf("CONST-042: %s emits the forbidden substring %q for the %s "+
							"endpoint shape.\n  message: %s\n  error  : %s",
							site.name, forbidden, shape.name, msg, err.Error())
					}
				}
				for _, want := range shape.mustHave {
					if !strings.Contains(combined, want) {
						t.Errorf("redaction destroyed the diagnostic: %s/%s must still name %q "+
							"so an operator can identify the misconfigured endpoint.\n  message: %s\n  error  : %s",
							site.name, shape.name, want, msg, err.Error())
					}
				}
				if shape.verbatim != "" {
					if !strings.Contains(combined, shape.verbatim) {
						t.Errorf("a credential-free endpoint must survive byte-identically; %s/%s "+
							"no longer contains %q.\n  message: %s\n  error  : %s",
							site.name, shape.name, shape.verbatim, msg, err.Error())
					}
					if strings.Contains(combined, "xxxxx") {
						t.Errorf("a credential-free endpoint was masked anyway at %s/%s; redaction "+
							"must not rewrite values it need not touch.\n  message: %s\n  error  : %s",
							site.name, shape.name, msg, err.Error())
					}
				}
			})
		}
	}
}

// TestGetHealth_ZeroProviderBranchRedactsBaseURL covers the one GetHealth
// branch the matrix above cannot reach: a server that ANSWERS but reports an
// empty roster. That branch formats the base URL without any transport error,
// so it exercises RedactEndpointForMessage alone.
func TestGetHealth_ZeroProviderBranchRedactsBaseURL(t *testing.T) {
	srv := newZeroProviderServer(t)
	const secret = "sk-live-zero-roster-token"
	p := New(strings.Replace(srv, "http://", "http://"+secret+"@", 1))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	health, err := p.GetHealth(ctx)
	if err == nil || health == nil {
		t.Fatalf("premise broken: an empty roster must yield an unhealthy record plus an error "+
			"(health=%v err=%v)", health, err)
	}
	if !strings.Contains(health.Message, "zero providers") {
		t.Fatalf("premise broken: expected the zero-provider branch, got %q", health.Message)
	}
	if strings.Contains(health.Message+"\n"+err.Error(), secret) {
		t.Errorf("CONST-042: the zero-provider branch leaks %q.\n  message: %s", secret, health.Message)
	}
}

// TestShapeTable_CoversEveryShapeTheSiblingTableDeclares is the mechanical
// guard against the round-6 defect RECURRING: a shape declared must-cover in
// one table being silently absent from another.
//
// It parses each sibling table's SOURCE (rather than importing it — test files
// are not importable across packages) and compares the declared shape-name
// sets in both directions. A name only here must carry a localOnlyReason, so
// divergence is always a deliberate, written-down decision.
//
// ROUND-8 (c) — there are now THREE tables, not two, and this check reads both
// of the others. The new one is ../../transport_error_redaction_test.go, which
// drives the TRANSPORT carrier in OpenAICompatibleProvider and
// KoboldAIProvider; round 8 found ten of that file's twelve wraps passing raw
// *url.Error values into fmt.Errorf. Adding a third table without extending
// this cross-check would have re-created the very gap the check exists to
// close, one table over: a shape could then live in two tables and be absent
// from the third with nothing to say so. Each table now requires every shape
// declared in EITHER of the other two.
func TestShapeTable_CoversEveryShapeTheSiblingTableDeclares(t *testing.T) {
	siblingTables := []struct{ path, fn string }{
		{"../../endpoint_redaction_test.go", "endpointRedactionCases"},
		{"../../transport_error_redaction_test.go", "transportRedactionShapes"},
	}

	siblingSet := map[string]bool{}
	sourceOf := map[string]string{}
	for _, st := range siblingTables {
		names := shapeNamesInFuncLiteral(t, st.path, st.fn)
		if len(names) == 0 {
			t.Fatalf("premise broken: parsed no shape names out of %s (%s) — that table was "+
				"renamed or restructured, and this cross-check has stopped checking it",
				st.path, st.fn)
		}
		for _, n := range names {
			siblingSet[n] = true
			if _, seen := sourceOf[n]; !seen {
				sourceOf[n] = st.path
			}
		}
	}

	local := map[string]redactionShape{}
	for _, s := range redactionShapes() {
		local[s.name] = s
	}

	var sibling []string
	for n := range siblingSet {
		sibling = append(sibling, n)
	}
	sort.Strings(sibling)

	for _, name := range sibling {
		if _, ok := local[name]; !ok {
			t.Errorf("shape %q is declared must-cover in %s but is ABSENT from this file's "+
				"table. That is exactly the round-6 defect: a shape the batch had already "+
				"written down, missing from the guard that needed it.", name, sourceOf[name])
		}
	}

	var localOnly []string
	for name, shape := range local {
		if siblingSet[name] {
			continue
		}
		if shape.localOnlyReason == "" {
			t.Errorf("shape %q exists here but in NEITHER sibling table, with no "+
				"localOnlyReason recorded. Either add it to a sibling table or state why it "+
				"does not belong there — drift in this direction is how a shape stops "+
				"propagating.", name)
		}
		localOnly = append(localOnly, name)
	}
	sort.Strings(localOnly)
	t.Logf("sibling shapes (union of %d tables): %v", len(siblingTables), sibling)
	t.Logf("local-only shapes (each with a recorded reason): %v", localOnly)
}

// shapeNamesInFuncLiteral extracts every `name: "..."` string literal appearing
// inside the named function's body.
func shapeNamesInFuncLiteral(t *testing.T, path, funcName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "name" {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if v, err := strconv.Unquote(lit.Value); err == nil {
				names = append(names, v)
			}
			return true
		})
	}
	sort.Strings(names)
	return names
}

// newZeroProviderServer starts a real local HTTP server that answers
// /v1/providers with an EMPTY roster, so GetHealth's zero-provider branch can
// be driven without a network or a live HelixAgent.
func newZeroProviderServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/providers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}
