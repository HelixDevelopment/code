package llm

import (
	"strings"
	"testing"
)

// endpoint_redaction_test.go — CONST-042 / Article XII §12.1 standing guard
// (§11.4.135) against a configured endpoint's CREDENTIAL riding into an error
// message that reaches an HTTP response body.
//
// The defect reproduced here: a cloud-gate refusal quoting the configured
// endpoint VERBATIM. An operator who sets
//
//	HELIX_LLM_LOCAL_OPENAI_ENDPOINT=https://user:s3cr3t@api.example.com/v1
//
// therefore had "s3cr3t" rendered into the constructor's error — and that
// error is wrapped by the server's localRouteRemoteEndpointError and returned
// to the caller as a 500 body. A real credential, handed to whoever can reach
// the endpoint, from a value the operator supplied only to the server process.
//
// PER-GATE-SITE, NOT PER-CONSTRUCTOR (round-5 review finding). The first
// version of this guard drove ONE constructor. The defect class is per GATE
// SITE: a second gate added in the same batch (NewKoboldAIProvider) shipped
// with the identical raw-%q leak and this file could not see it, because a
// guard scoped to one constructor structurally cannot cover a site it does not
// call. The table below is therefore keyed by SITE, and
// TestCloudGateSites_TableCoversEverySiteTheScannerFinds cross-checks its
// membership against the AST scan in cloud_gate_redaction_scan_test.go, so a
// FOURTH gate site added without a row here FAILS rather than passing silently.
//
// §11.4.115 polarity: RED_MODE is NOT implemented here, for the same honest
// reason the sibling cloud_gate_closed_test.go records. The historical defect
// is the ABSENCE of redaction at a format call — no runtime input re-creates
// it on the fixed source, and re-implementing the raw-%q format locally to
// "reproduce" it would be testing a copy of the bug, which §11.4.115 forbids.
// The RED evidence for this guard was captured instead by running these
// assertions against the genuinely broken source before the fix landed, and by
// the paired §1.1 mutation that reverts the format argument and re-runs them.

// cloudGateEndpointSite is ONE construction site that (a) refuses while the
// cloud gate is closed and (b) names the configured endpoint in its refusal.
// Every such site must quote the endpoint through RedactEndpointForMessage.
type cloudGateEndpointSite struct {
	name string
	// scannerSymbol is the PACKAGE-QUALIFIED enclosing function the AST scan
	// reports this site under (round-6 N7: a bare function name would let one
	// row satisfy the cross-check for same-named functions in two packages). It is what binds a row to a real source location: the
	// cross-check test compares this set against the scanner's findings.
	scannerSymbol string
	// construct drives the REAL site with the given endpoint and returns the
	// refusal error. Never a reimplementation of the message.
	construct func(endpoint string) error
}

func cloudGateEndpointSites() []cloudGateEndpointSite {
	return []cloudGateEndpointSite{
		{
			name:          "openai_compatible",
			scannerSymbol: "llm.NewOpenAICompatibleProvider",
			construct: func(endpoint string) error {
				_, err := NewOpenAICompatibleProvider("leaky", OpenAICompatibleConfig{
					BaseURL: endpoint,
				})
				return err
			},
		},
		{
			name:          "koboldai",
			scannerSymbol: "llm.NewKoboldAIProvider",
			construct: func(endpoint string) error {
				_, err := NewKoboldAIProvider(KoboldAIConfig{
					BaseURL: endpoint,
					APIKey:  "kobold-bearer-token",
				})
				return err
			},
		},
	}
}

type endpointRedactionCase struct {
	name        string
	endpoint    string
	mustNotHave []string
	mustHave    []string
}

func endpointRedactionCases() []endpointRedactionCase {
	return []endpointRedactionCase{
		{
			// ROUND-7 BLOCKER A1, half-assertion half. This row named a
			// two-component credential and forbade only ONE of them: it
			// asserted the PASSWORD was absent and never that the USERNAME
			// was. It therefore stayed green while the username slot leaked
			// in full — the THIRD instance of that trap in this batch. A row
			// must forbid EVERY component of the credential it names.
			name:        "user_and_password",
			endpoint:    "https://svc-user:s3cr3tpw@api.example.com:8443/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-user"},
			mustHave:    []string{"https", "api.example.com", "8443"},
		},
		{
			// ROUND-7 BLOCKER A1. DOCUMENTED VENDOR USAGE, not an exotic
			// shape: `curl -u sk_test_xxx:` is Stripe's own published form,
			// so a live key in the USERNAME slot with an EMPTY password is
			// ordinary operator configuration.
			//
			// url.Userinfo.Password() reports ("", true) here — a password
			// COMPONENT exists, it is merely empty — so the password-less
			// branch was skipped and the value fell through to
			// url.URL.Redacted(), which masks the PASSWORD ONLY and prints
			// the username verbatim. The credential is the username.
			name:        "key_as_username_empty_password",
			endpoint:    "https://sk_test_EXAMPLE_NOT_A_REAL_CREDENTIAL:@api.example.com/v1",
			mustNotHave: []string{"sk_test_EXAMPLE_NOT_A_REAL_CREDENTIAL"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// The same vendor convention spelled with a dummy password, which
			// is what a client that refuses an empty one produces. Same
			// carrier, same leak, different bytes.
			name:        "key_as_username_dummy_password",
			endpoint:    "https://sk-live-dummypw-9876543210:x@api.example.com/v1",
			mustNotHave: []string{"sk-live-dummypw-9876543210"},
			mustHave:    []string{"api.example.com"},
		},
		{
			name:        "token_as_username_no_password",
			endpoint:    "https://sk-live-abcdef0123456789@api.example.com/v1",
			mustNotHave: []string{"sk-live-abcdef0123456789"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// Round-6. Percent-encoded userinfo: still password-less, so still
			// unmasked by anything in the stdlib. Mirrors the same-named row in
			// providers/helixagent/health_redaction_test.go; the two tables
			// cross-check each other's membership.
			// ROUND-7 A1 sweep: the row forbade only the SUFFIX of the
			// credential, so a mask that ate the tail and kept the "sk@live-"
			// head would have passed. Both the encoded and decoded spellings
			// of the whole userinfo are forbidden now.
			name:        "percent_encoded_userinfo",
			endpoint:    "https://sk%40live-abcdef0123456789@api.example.com/v1",
			mustNotHave: []string{"sk%40live-abcdef0123456789", "sk@live-abcdef0123456789", "abcdef0123456789"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// ROUND-7 A1 sweep: the username slot was `svc`/`user`, a token too
			// generic to assert absent without risking a false failure on
			// unrelated message text. Renamed to a distinctive value so BOTH
			// components of the credential are actually asserted.
			name:        "scheme_less_with_password",
			endpoint:    "svc-acct-nz:hunter2@gpu.example.com:8000",
			mustNotHave: []string{"hunter2", "svc-acct-nz"},
			mustHave:    []string{"gpu.example.com", "8000"},
		},
		{
			// Round-5 BLOCKER 2, bypass 1. A protocol-relative URL contains
			// "://" nowhere, so an UNANCHORED strings.Contains(raw, "://")
			// scheme test skipped the prefix, url.Parse never saw an
			// authority, u.User stayed nil, and the value came back
			// BYTE-IDENTICAL with the password in it.
			name:        "protocol_relative_with_password",
			endpoint:    "//svc-acct-nz:s3cr3tpw@api.example.com/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// Round-5 BLOCKER 2, bypass 2. Here the "://" the unanchored test
			// found was inside a QUERY PARAMETER, not a scheme — same
			// byte-identical passthrough, same leak.
			name:        "scheme_less_with_scheme_shaped_query_param",
			endpoint:    "svc-acct-nz:s3cr3tpw@api.example.com/r?to=https://x",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// Round-5 N1. "?api_key=" is a real OpenAI-compatible gateway
			// convention, so a credential in the QUERY is not exotic.
			name:        "credential_in_query_parameter",
			endpoint:    "https://api.example.com/v1?api_key=s3cr3tpw",
			mustNotHave: []string{"s3cr3tpw"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// Round-6 N2. The fragment list missed several ordinary spellings:
			// "?pass=", "?passphrase=", "?bearer=", "?jwt=" and "?cred=" all
			// passed BYTE-IDENTICAL through a function whose whole job is to
			// mask credential-shaped parameters.
			name:        "credential_in_query_alternate_keys",
			endpoint:    "https://api.example.com/v1?pass=s3cr3tA&passphrase=s3cr3tB&bearer=s3cr3tC&jwt=s3cr3tD&cred=s3cr3tE",
			mustNotHave: []string{"s3cr3tA", "s3cr3tB", "s3cr3tC", "s3cr3tD", "s3cr3tE"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// Round-6 N3. An "@" is present but net/url parses NO userinfo,
			// because the "@" sits in the PATH rather than the authority. The
			// value therefore carried no u.User, no credential-shaped
			// parameter, and came back BYTE-IDENTICAL. mustHave is empty on
			// these rows deliberately: the honest answer for a value whose
			// credential position cannot be identified is the fail-closed
			// placeholder, which names no host.
			//
			// The single-slash variant ("http:/user:pw@host/x") is NOT a row
			// here: this table drives GATE REFUSALS, and that value is not
			// refused at all — isLocalEndpointURL verdicts it LOCAL, because
			// the "://"-less prefixing makes net/url read "http" as the
			// hostname. It is covered instead by
			// TestRedactEndpointForMessage_UnresolvedUserinfoMarkerIsNotEchoed
			// below (the function directly) and by the same-named row in
			// providers/helixagent/health_redaction_test.go (the transport).
			// ROUND-7 A1 sweep: NOT a half-assertion — there is no userinfo
			// here at all (that is the point of the row), so "s3cr3tpw" is
			// the whole of the credential this row carries.
			name:        "at_in_path_after_host",
			endpoint:    "https://api.example.com/x:s3cr3tpw@extra/v1",
			mustNotHave: []string{"s3cr3tpw"},
		},
		{
			name:        "scheme_shaped_prefix_with_digit",
			endpoint:    "1http://svc-acct-nz:s3cr3tpw@api.example.com/x",
			mustNotHave: []string{"s3cr3tpw", "svc-acct-nz"},
		},
		{
			// ROUND-7 A2. A DELIBERATE fixture for the request-build carrier.
			//
			// http.NewRequestWithContext fails before any packet when
			// url.Parse rejects the URL, and a non-numeric port is the
			// cleanest way to reach that branch on purpose. Until now the
			// carrier was only ever reached INCIDENTALLY — the
			// scheme_shaped_prefix_with_digit row happens to make url.Parse
			// fail with "first path segment cannot contain colon" — and a
			// carrier covered by accident stops being covered the moment the
			// row that happened to reach it is edited for another reason.
			//
			// mustHave is empty for the same reason as the other fail-closed
			// rows: an unparseable value yields the placeholder, which names
			// no host. The sibling table pins the carrier positively.
			name:        "invalid_port_with_token",
			endpoint:    "https://sk-live-invalidport-t0k3n@api.example.com:notaport/v1",
			mustNotHave: []string{"sk-live-invalidport-t0k3n"},
		},
		{
			// Round-6 N4. Masking the fragment re-rendered it through
			// url.URL.Fragment, which escapes on output — so an UNTOUCHED
			// sibling parameter's "%2F" became "%252F", contradicting the
			// documented promise that untouched parameters survive verbatim.
			name:        "credential_in_fragment_with_escaped_sibling",
			endpoint:    "https://api.example.com/v1#token=s3cr3tpw&path=%2Fx",
			mustNotHave: []string{"s3cr3tpw", "%252F"},
			mustHave:    []string{"api.example.com", "%2Fx"},
		},
		{
			// Round-5 N1, fragment half.
			name:        "credential_in_fragment",
			endpoint:    "https://api.example.com/v1#token=s3cr3tpw",
			mustNotHave: []string{"s3cr3tpw"},
			mustHave:    []string{"api.example.com"},
		},
	}
}

// TestCloudGateSites_RefusalRedactsEndpointCredential drives EVERY gate site
// in the table with EVERY leak shape. Site × shape, so a new site inherits the
// whole corpus of known bypasses and a new bypass is checked at every site.
func TestCloudGateSites_RefusalRedactsEndpointCredential(t *testing.T) {
	prev := CloudEnabled()
	SetCloudEnabled(false)
	t.Cleanup(func() { SetCloudEnabled(prev) })

	for _, site := range cloudGateEndpointSites() {
		for _, tc := range endpointRedactionCases() {
			t.Run(site.name+"/"+tc.name, func(t *testing.T) {
				err := site.construct(tc.endpoint)
				if err == nil {
					t.Fatalf("expected the cloud gate to refuse remote endpoint %q, got nil error",
						tc.endpoint)
				}
				msg := err.Error()
				for _, secret := range tc.mustNotHave {
					if strings.Contains(msg, secret) {
						t.Errorf("CONST-042 violation: the %s gate refusal leaks the credential "+
							"%q from the configured endpoint into an error that reaches an HTTP "+
							"response body.\nmessage: %s", site.name, secret, msg)
					}
				}
				for _, want := range tc.mustHave {
					if !strings.Contains(msg, want) {
						t.Errorf("redaction destroyed diagnostics: the %s message must still name "+
							"%q so an operator can identify the misconfigured endpoint.\nmessage: %s",
							site.name, want, msg)
					}
				}
			})
		}
	}
}

// TestRedactEndpointForMessage_UnparseableValueIsNotEchoed pins the
// fail-closed branch: a value net/url rejects is exactly where a malformed
// credential can hide, so it must NOT fall through to printing the raw string.
func TestRedactEndpointForMessage_UnparseableValueIsNotEchoed(t *testing.T) {
	// A DEL control character makes net/url reject the value outright.
	raw := "https://user:s3cr3tpw@api.example.com/v1\x7f"
	got := RedactEndpointForMessage(raw)
	if strings.Contains(got, "s3cr3tpw") {
		t.Fatalf("unparseable endpoint echoed verbatim, leaking its credential: %q", got)
	}
	if got == raw {
		t.Fatalf("unparseable endpoint returned unchanged; a safe placeholder is required: %q", got)
	}
}

// TestRedactEndpointForMessage_UnresolvedUserinfoMarkerIsNotEchoed covers the
// round-6 N3 shapes DIRECTLY, including the one the gate-site table above
// cannot drive because the gate does not refuse it.
//
// Each value puts an "@" where userinfo would live, but net/url resolves it to
// something else — a path segment, or nothing at all — so u.User stays nil and
// the pre-fix function returned the value BYTE-IDENTICAL, credential included.
func TestRedactEndpointForMessage_UnresolvedUserinfoMarkerIsNotEchoed(t *testing.T) {
	for _, raw := range []string{
		"http:/user:s3cr3tpw@api.example.com/v1",      // one slash: no authority parsed
		"https://api.example.com/x:s3cr3tpw@extra/v1", // the "@" lands in the PATH
		"1http://user:s3cr3tpw@api.example.com/x",     // not a legal scheme
	} {
		got := RedactEndpointForMessage(raw)
		if strings.Contains(got, "s3cr3tpw") {
			t.Errorf("%q was echoed with its credential intact: %q", raw, got)
		}
		if got == raw {
			t.Errorf("%q passed through unchanged; a value whose credential position cannot "+
				"be identified must fail closed to a placeholder", raw)
		}
	}
}

// TestRedactEndpointForMessage_CredentiallessValueIsUnchanged guards the
// overwhelmingly common case: an endpoint with no credential must render
// byte-identically to before, so redaction cannot silently degrade the
// operator-facing message of every ordinary misconfiguration.
func TestRedactEndpointForMessage_CredentiallessValueIsUnchanged(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:18434/v1",
		"https://api.example.com/v1",
		"gpu-box.local:8000",
		"",
		// Round-5 N4: a legal OPAQUE URI. The authority-prefix path mangles
		// it into something net/url rejects, and the placeholder that
		// produced was a false refusal (§11.4.201) of a credential-free
		// value — there is no "@" anywhere, so no userinfo can hide in it.
		"https:api.example.com/v1",
		// Protocol-relative WITHOUT a credential must also survive intact:
		// the new "//" branch must not rewrite values it need not touch.
		"//api.example.com/v1",
		// A query parameter whose name is NOT credential-ish is left alone.
		"https://api.example.com/v1?model=llama3.2&stream=true",
		// Round-6 N3, the other direction. The "@" here belongs to a VALUE and
		// sits after the "?", so the fail-closed discriminator must not fire.
		"https://api.example.com/v1?email=a@b.com",
	} {
		if got := RedactEndpointForMessage(raw); got != raw {
			t.Errorf("credential-free endpoint %q was rewritten to %q; it must pass through unchanged",
				raw, got)
		}
	}
}
