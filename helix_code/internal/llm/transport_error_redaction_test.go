package llm

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// transport_error_redaction_test.go — CONST-042 / Article XII §12.1 standing
// guard (§11.4.135) for the TRANSPORT carrier in the two providers that live
// in this package: OpenAICompatibleProvider and KoboldAIProvider.
//
// WHY THIS FILE EXISTS — the round-8 BLOCKING finding, recorded so the lesson
// survives the fix.
//
// RedactEndpointsInError is DEFINED in openai_compatible_provider.go. Its doc
// comment said, of the ordering rule:
//
//	"Every call site therefore redacts the error the moment it comes back
//	 from the transport, and wraps afterwards."
//
// That sentence was FALSE in the file that houses the helper. The helper had
// ten call sites in providers/helixagent and two here; the sixteen transport
// wraps in these two providers passed the raw *url.Error straight into
// fmt.Errorf. A configured endpoint of the documented Stripe form
// `https://sk_live_xxx:@host/v1` therefore rode out of Generate() and
// GetHealth() in full, through internal/server/llm_generate.go's
// "generation failed: %v" into a 502 response body.
//
// net/http's own stripPassword is why that survived earlier review: it masks
// the PASSWORD component and NOTHING else, so a password-bearing fixture comes
// back clean while the ordinary machine shape — the credential IS the username,
// no password at all — is echoed verbatim. The locality gate does not mitigate
// it either: a userinfo-bearing LOCAL endpoint constructs fine, which is
// exactly what every shape below proves.
//
// THE UNIVERSAL CLAIM IS THE ACTUAL DEFECT. A scoped fix carrying a universal
// doc claim is how the KoboldAI gate site was missed one round earlier: a
// reader greps for the helper, reads "every call site", and stops looking. So
// this batch does not merely add the missing wraps — it makes the claim
// mechanically enforceable in transport_redaction_scan_test.go, and the doc
// comment now states only what that scan enforces.
//
// SHAPE × SITE, and cross-checked in both directions:
//   - Every transport-bearing method of both providers is a SITE. A leak
//     carrier is a property of the TRANSPORT, not of one method.
//   - Every shape the sibling tables declare must appear here, either driven
//     or with a written-down reason it cannot be. See
//     TestTransportShapeTable_CoversEveryShapeTheSiblingTablesDeclare.
//
// §11.4.115 polarity: RED_MODE is NOT implemented, for the reason the sibling
// guards record — the defect is the ABSENCE of a redaction call, and no
// runtime input re-creates it on fixed source. RED was captured by running
// this matrix against the pre-fix artifact (every shape leaking at every one
// of the eight sites), and is re-provable at any time by the paired §1.1
// mutation that removes a wrap and re-runs it.

// transportRedactionSite is ONE method of one provider that performs a
// transport call and surfaces the resulting error to an operator.
//
// drive CONSTRUCTS the provider too, deliberately: the constructor itself runs
// discoverModels and logs its error, so construction is part of the carrier
// under test rather than setup that happens to precede it.
type transportRedactionSite struct {
	name string
	// drive returns the operator-facing MESSAGE (empty when the method has
	// none) and the returned error. Both are checked: GetHealth interpolates
	// the error into its message with %v, so either alone is incomplete.
	drive func(ctx context.Context, endpoint string) (string, error)
}

func transportRedactionSites() []transportRedactionSite {
	req := func() *LLMRequest {
		return &LLMRequest{Messages: []Message{{Role: "user", Content: "ping"}}}
	}
	// Timeouts are irrelevant — 127.0.0.1:1 refuses immediately — but a short
	// one keeps a mistake cheap rather than hanging the suite.
	openai := func(endpoint string) (*OpenAICompatibleProvider, error) {
		return NewOpenAICompatibleProvider("vllm", OpenAICompatibleConfig{
			BaseURL: endpoint,
			APIKey:  "unused-the-endpoint-is-the-carrier",
			Timeout: 2 * time.Second,
		})
	}
	kobold := func(endpoint string) (*KoboldAIProvider, error) {
		return NewKoboldAIProvider(KoboldAIConfig{
			BaseURL: endpoint,
			APIKey:  "unused-the-endpoint-is-the-carrier",
			Timeout: 2 * time.Second,
		})
	}
	drainStream := func(run func(chan LLMResponse) error) error {
		ch := make(chan LLMResponse, 8)
		drained := make(chan struct{})
		go func() {
			for range ch {
			}
			close(drained)
		}()
		err := run(ch)
		<-drained // the provider closes ch; never leak the drain goroutine.
		return err
	}

	return []transportRedactionSite{
		{
			name: "openai_compatible/GetHealth",
			drive: func(ctx context.Context, endpoint string) (string, error) {
				p, err := openai(endpoint)
				if err != nil {
					return "", err
				}
				h, err := p.GetHealth(ctx)
				if h == nil {
					return "", err
				}
				return h.Message, err
			},
		},
		{
			name: "openai_compatible/Generate",
			drive: func(ctx context.Context, endpoint string) (string, error) {
				p, err := openai(endpoint)
				if err != nil {
					return "", err
				}
				_, err = p.Generate(ctx, req())
				return "", err
			},
		},
		{
			name: "openai_compatible/GenerateStream",
			drive: func(ctx context.Context, endpoint string) (string, error) {
				p, err := openai(endpoint)
				if err != nil {
					return "", err
				}
				return "", drainStream(func(ch chan LLMResponse) error {
					return p.GenerateStream(ctx, req(), ch)
				})
			},
		},
		{
			name: "openai_compatible/discoverModels",
			drive: func(_ context.Context, endpoint string) (string, error) {
				p, err := openai(endpoint)
				if err != nil {
					return "", err
				}
				// The REAL site. The constructor swallows this error into a
				// log line, so driving construction alone would assert on
				// nothing.
				return "", p.discoverModels()
			},
		},
		{
			name: "koboldai/GetHealth",
			drive: func(ctx context.Context, endpoint string) (string, error) {
				p, err := kobold(endpoint)
				if err != nil {
					return "", err
				}
				h, err := p.GetHealth(ctx)
				if h == nil {
					return "", err
				}
				return h.Message, err
			},
		},
		{
			name: "koboldai/Generate",
			drive: func(ctx context.Context, endpoint string) (string, error) {
				p, err := kobold(endpoint)
				if err != nil {
					return "", err
				}
				_, err = p.Generate(ctx, req())
				return "", err
			},
		},
		{
			name: "koboldai/GenerateStream",
			drive: func(ctx context.Context, endpoint string) (string, error) {
				p, err := kobold(endpoint)
				if err != nil {
					return "", err
				}
				return "", drainStream(func(ch chan LLMResponse) error {
					return p.GenerateStream(ctx, req(), ch)
				})
			},
		},
		{
			name: "koboldai/discoverModels",
			drive: func(_ context.Context, endpoint string) (string, error) {
				p, err := kobold(endpoint)
				if err != nil {
					return "", err
				}
				return "", p.discoverModels()
			},
		},
	}
}

// transportRedactionShape is ONE way a credential can sit in a configured base
// URL. Hosts are pinned to 127.0.0.1:1 — reserved and never listening — so
// every case fails immediately with no DNS lookup and no outbound packet.
type transportRedactionShape struct {
	name        string
	endpoint    string
	mustNotHave []string
	mustHave    []string
	// verbatim, when non-empty, must appear UNCHANGED — the credential-free
	// control, proving redaction does not rewrite what it need not touch.
	verbatim string
	// gateRefusedReason names a shape that CANNOT reach the transport in these
	// two providers because the W2c-1 locality gate refuses construction
	// first. Such a row is still declared here — so the cross-check sees a
	// deliberate exclusion rather than an absence — and the test ASSERTS the
	// refusal, so a row cannot silently become drivable-but-undriven.
	gateRefusedReason string
	// localOnlyReason documents a shape with no counterpart in a sibling
	// table. Empty means the shape is expected to exist in all of them.
	localOnlyReason string
}

// transportRedactionShapes mirrors providers/helixagent's redactionShapes.
// The corpus is duplicated rather than shared because Go test files are not
// importable across packages; the cross-check below is the mitigation, and it
// runs in both directions.
func transportRedactionShapes() []transportRedactionShape {
	return []transportRedactionShape{
		{
			// The shape that PASSED on broken source everywhere it appeared:
			// net/http's stripPassword masks the PASSWORD, so it proves
			// nothing about the general case. Kept for the contrast.
			name:        "user_and_password",
			endpoint:    "https://svc-user:s3cr3tpw@127.0.0.1:1/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-user"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// `curl -u sk_test_xxx:` is Stripe's own published form: a live
			// key in the USERNAME slot with an EMPTY password. This is the
			// shape the round-8 reviewer reproduced at runtime against these
			// exact providers.
			name:        "key_as_username_empty_password",
			endpoint:    "https://sk_test_4eC39HqLyjWDarjtT1zdp7dc:@127.0.0.1:1/v1",
			mustNotHave: []string{"sk_test_4eC39HqLyjWDarjtT1zdp7dc"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "key_as_username_dummy_password",
			endpoint:    "https://sk-live-dummypw-9876543210:x@127.0.0.1:1/v1",
			mustNotHave: []string{"sk-live-dummypw-9876543210"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "token_as_username_no_password",
			endpoint:    "https://sk-live-abcdef0123456789@127.0.0.1:1/v1",
			mustNotHave: []string{"sk-live-abcdef0123456789"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "percent_encoded_userinfo",
			endpoint:    "https://sk%40live-abcdef0123456789@127.0.0.1:1/v1",
			mustNotHave: []string{"sk%40live-abcdef0123456789", "sk@live-abcdef0123456789", "abcdef0123456789"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			name:        "scheme_less_with_password",
			endpoint:    "svc-acct-nz:hunter2@127.0.0.1:1",
			mustNotHave: []string{"hunter2", "svc-acct-nz"},
		},
		{
			name:     "protocol_relative_with_password",
			endpoint: "//svc-acct-nz:s3cr3tpw@127.0.0.1:1/v1",
			gateRefusedReason: "net/url reads the \"//\" prefix as a host-relative reference with " +
				"host \"svc-acct-nz\", which isLocalEndpointURL correctly verdicts NON-local, so " +
				"both constructors refuse before any transport call. The refusal message is the " +
				"carrier here, and endpoint_redaction_test.go owns that assertion.",
		},
		{
			name:     "scheme_less_with_scheme_shaped_query_param",
			endpoint: "svc-acct-nz:s3cr3tpw@127.0.0.1:1/r?to=https://x",
			gateRefusedReason: "verdicted NON-local by isLocalEndpointURL, so construction is " +
				"refused before the transport. Covered as a gate refusal by " +
				"endpoint_redaction_test.go.",
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
			// No mustHave: the honest answer for a value whose credential
			// position cannot be identified is the fail-closed placeholder,
			// which names no host.
			name:        "at_before_path_no_userinfo_single_slash",
			endpoint:    "http:/svc-acct-nz:s3cr3tpw@127.0.0.1:1/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
		},
		{
			name:        "at_in_path_after_host",
			endpoint:    "https://127.0.0.1:1/x:s3cr3tpw@extra/v1",
			mustNotHave: []string{"s3cr3tpw"},
		},
		{
			name:     "scheme_shaped_prefix_with_digit",
			endpoint: "1http://svc-acct-nz:s3cr3tpw@127.0.0.1:1/x",
			gateRefusedReason: "a scheme may not begin with a digit, so net/url rejects the value " +
				"outright and isLocalEndpointURL fail-closes; construction is refused before the " +
				"transport. Covered as a gate refusal by endpoint_redaction_test.go.",
		},
		{
			name:     "invalid_port_with_token",
			endpoint: "https://sk-live-invalidport-t0k3n@127.0.0.1:notaport/v1",
			gateRefusedReason: "a non-numeric port makes url.Parse reject the value, so " +
				"isLocalEndpointURL fail-closes and BOTH constructors refuse before any request " +
				"is built. This is the one shape that drives the REQUEST-BUILD carrier in " +
				"providers/helixagent (which has no gate in front of it); here that carrier is " +
				"reached instead through a malformed configured PATH — see " +
				"TestTransportSites_RequestBuildCarrierRedactsBaseURL.",
		},
		{
			name:        "credential_in_fragment_with_escaped_sibling",
			endpoint:    "https://127.0.0.1:1/v1#token=s3cr3tpw&path=%2Fx",
			mustNotHave: []string{"s3cr3tpw", "%252F"},
			mustHave:    []string{"127.0.0.1"},
		},
		{
			// The other direction. Redaction that quietly rewrote every
			// ordinary endpoint would degrade every ordinary diagnostic.
			name:     "credential_free_control",
			endpoint: "http://127.0.0.1:1/v1",
			mustHave: []string{"127.0.0.1"},
			verbatim: "http://127.0.0.1:1/v1",
			localOnlyReason: "the gate-refusal sibling covers the credential-free case in its own " +
				"dedicated byte-identity test rather than as a table row",
		},
	}
}

// TestTransportSites_RedactEndpointCredential drives EVERY site with EVERY
// shape. This is the assertion that was RED on the pre-fix artifact at all
// eight sites.
func TestTransportSites_RedactEndpointCredential(t *testing.T) {
	for _, site := range transportRedactionSites() {
		for _, shape := range transportRedactionShapes() {
			t.Run(site.name+"/"+shape.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()

				msg, err := site.drive(ctx, shape.endpoint)
				if err == nil {
					t.Fatalf("premise broken: %s against %q was expected to fail "+
						"(127.0.0.1:1 refuses immediately); got a nil error",
						site.name, shape.endpoint)
				}

				if shape.gateRefusedReason != "" {
					// The row asserts its own exclusion. If the gate ever
					// stops refusing this shape, it becomes a live transport
					// carrier with no assertions on it — so that transition
					// FAILS here rather than passing quietly.
					if !errors.Is(err, ErrCloudDisabled) {
						t.Fatalf("%s/%s is declared gate-refused, but construction did NOT "+
							"refuse with ErrCloudDisabled — it now reaches the transport and "+
							"this row is asserting nothing. Either drive it (drop "+
							"gateRefusedReason and add mustNotHave) or correct the reason.\n"+
							"  reason on file: %s\n  got error: %v",
							site.name, shape.name, shape.gateRefusedReason, err)
					}
					return
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
							"so an operator can identify the misconfigured endpoint.\n"+
							"  message: %s\n  error  : %s",
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

// TestTransportSites_RequestBuildCarrierRedactsBaseURL covers the OTHER
// transport carrier: http.NewRequestWithContext, which fails BEFORE any packet
// is sent and returns &url.Error{Op: "parse", URL: rawURL} with the raw value
// untouched.
//
// Reaching it needs a base URL that PASSES the locality gate (so it must parse
// and be loopback) combined with a request path that makes the CONCATENATION
// unparseable. OpenAICompatibleProvider takes both of its request paths from
// caller configuration (ModelEndpoint, ChatEndpoint), so this is ordinary
// operator misconfiguration, not a contrived input.
//
// HONEST REACHABILITY BOUNDARY (§11.4.6) — KoboldAIProvider is NOT driven
// here, and that is a finding rather than an omission: its four request paths
// are hard-coded literals ("/api/v1/model", "/api/v1/generate"), and its base
// URL must already parse to have passed the gate, so no configuration reaches
// its request-build carrier WHILE THE GATE IS CLOSED. With the gate OPEN
// (llm.cloud.enabled: true) the gate check is skipped entirely and an
// unparseable base URL does reach it — which is why those four sites are
// wrapped anyway, and why transport_redaction_scan_test.go enforces the wrap
// mechanically rather than trusting this reachability analysis to stay true.
func TestTransportSites_RequestBuildCarrierRedactsBaseURL(t *testing.T) {
	const secret = "sk-live-requestbuild-t0k3n"
	// Loopback and parseable, so the gate admits it; the credential is the
	// username with no password, the shape the stdlib does not mask.
	const base = "https://" + secret + "@127.0.0.1:1"
	// A DEL control character is rejected by net/url, so the concatenation
	// base+path fails inside http.NewRequestWithContext.
	const malformedPath = "/v1/models\x7f"

	p, err := NewOpenAICompatibleProvider("vllm", OpenAICompatibleConfig{
		BaseURL:       base,
		ModelEndpoint: malformedPath,
		ChatEndpoint:  malformedPath,
		APIKey:        "unused-the-endpoint-is-the-carrier",
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("premise broken: the base URL is loopback and parseable, so the locality "+
			"gate must admit it; got %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cases := []struct {
		name string
		run  func() (string, error)
	}{
		{"GetHealth", func() (string, error) {
			h, err := p.GetHealth(ctx)
			if h == nil {
				return "", err
			}
			return h.Message, err
		}},
		{"discoverModels", func() (string, error) { return "", p.discoverModels() }},
		{"Generate", func() (string, error) {
			_, err := p.Generate(ctx, &LLMRequest{Messages: []Message{{Role: "user", Content: "ping"}}})
			return "", err
		}},
		{"GenerateStream", func() (string, error) {
			ch := make(chan LLMResponse, 8)
			drained := make(chan struct{})
			go func() {
				for range ch {
				}
				close(drained)
			}()
			err := p.GenerateStream(ctx, &LLMRequest{Messages: []Message{{Role: "user", Content: "ping"}}}, ch)
			<-drained
			return "", err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := tc.run()
			if err == nil {
				t.Fatalf("premise broken: %s with an unparseable request path must fail", tc.name)
			}
			combined := msg + "\n" + err.Error()
			// The premise assertion: this really is the PARSE error, so the
			// row cannot silently start passing through the dial path and
			// leave the request-build carrier unexercised.
			if !strings.Contains(combined, "invalid control character in URL") {
				t.Fatalf("premise broken: expected the net/url parse rejection (only "+
					"http.NewRequestWithContext produces it here), got: %s", combined)
			}
			if strings.Contains(combined, secret) {
				t.Errorf("CONST-042: the request-build carrier at %s leaks %q.\n  message: %s\n  error  : %s",
					tc.name, secret, msg, err.Error())
			}
		})
	}
}

// TestTransportShapeTable_CoversEveryShapeTheSiblingTablesDeclare is the
// mechanical guard against the round-6/7/8 defect RECURRING: a shape declared
// must-cover in one table being silently absent from another.
//
// It checks THREE tables against each other. endpointRedactionCases lives in
// this package and is called directly; providers/helixagent's redactionShapes
// is parsed from source, because test files are not importable across
// packages.
func TestTransportShapeTable_CoversEveryShapeTheSiblingTablesDeclare(t *testing.T) {
	local := map[string]transportRedactionShape{}
	for _, s := range transportRedactionShapes() {
		local[s.name] = s
	}

	siblings := map[string][]string{}

	var gateNames []string
	for _, c := range endpointRedactionCases() {
		gateNames = append(gateNames, c.name)
	}
	sort.Strings(gateNames)
	siblings["endpoint_redaction_test.go:endpointRedactionCases"] = gateNames

	const helixagentPath = "providers/helixagent/health_redaction_test.go"
	helixagentNames := shapeNamesInFuncBody(t, helixagentPath, "redactionShapes")
	if len(helixagentNames) == 0 {
		t.Fatalf("premise broken: parsed no shape names out of %s — that table was renamed or "+
			"restructured and this cross-check has stopped checking anything", helixagentPath)
	}
	siblings[helixagentPath+":redactionShapes"] = helixagentNames

	union := map[string]bool{}
	for source, names := range siblings {
		for _, name := range names {
			union[name] = true
			if _, ok := local[name]; !ok {
				t.Errorf("shape %q is declared must-cover in %s but is ABSENT from this file's "+
					"table. That is exactly the recurring defect: a shape the batch had already "+
					"written down, missing from the guard that needed it.", name, source)
			}
		}
	}

	var localOnly []string
	for name, shape := range local {
		if union[name] {
			continue
		}
		if shape.localOnlyReason == "" {
			t.Errorf("shape %q exists here but in neither sibling table, with no localOnlyReason "+
				"recorded. Either add it to a sibling table or state why it does not belong "+
				"there — drift in this direction is how a shape stops propagating.", name)
		}
		localOnly = append(localOnly, name)
	}
	sort.Strings(localOnly)

	var refused []string
	for name, shape := range local {
		if shape.gateRefusedReason != "" {
			refused = append(refused, name)
		}
	}
	sort.Strings(refused)
	t.Logf("shapes driven against the transport: %d", len(local)-len(refused))
	t.Logf("shapes declared gate-refused (assertion: construction refuses): %v", refused)
	t.Logf("local-only shapes (each with a recorded reason): %v", localOnly)
}

// shapeNamesInFuncBody extracts every `name: "..."` string literal appearing
// inside the named function's body of the file at path.
func shapeNamesInFuncBody(t *testing.T, path, funcName string) []string {
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
