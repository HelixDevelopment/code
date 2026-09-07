package llm

import (
	"errors"
	"testing"
	"time"

	"dev.helix.code/internal/verifier"
)

// cloud_gate_endpoint_locality_test.go — guards the ENDPOINT-LOCALITY half of
// the W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
// serving).
//
// The defect these tests close: hosted OpenAI-compatible providers reach
// NewOpenAICompatibleProvider WITHOUT ever passing through NewCloudProvider
// (BuildDynamicOpenAICompatibleProviders in verifier_dynamic_catalogue.go and
// NewHostedOpenAICompatibleProvider in openai_compatible_catalogue.go both call
// it directly), so the gate in NewCloudProvider never saw them. With
// llm.cloud.enabled false and e.g. CEREBRAS_API_KEY exported, real hosted
// providers were still registered.
//
// The trap the fix had to avoid: that SAME constructor serves the local
// HelixLLM coder (http://localhost:18434), llama.cpp, vLLM, LM Studio and
// LocalAI. So the gate keys on endpoint locality, never on provider identity —
// and TestCloudGateClosed_LocalOpenAICompatibleStillConstructs below is the
// standing proof that local serving was not broken by closing the bypass.
//
// None of these tests may run with t.Parallel(): cloudGate is a process-global
// and every one of them observes or flips it.

// closeCloudGateForTest CLOSES the W2c-1 cloud gate for the duration of one
// test, restoring the previous state on cleanup. Mirror of
// openCloudGateForTest in provider_factory_test.go (which is reused as-is for
// the gate-open case below); the snapshot-and-restore shape is deliberate and
// load-bearing — TestCloudGate_DefaultStateIsClosed in cloud_gate_open_test.go
// reads the shared global with no isolation of its own and stays meaningful
// only because every gate-flipping test in this package restores what it found.
func closeCloudGateForTest(t *testing.T) {
	t.Helper()
	prev := CloudEnabled()
	SetCloudEnabled(false)
	t.Cleanup(func() { SetCloudEnabled(prev) })
}

// TestIsLocalEndpointURL_Locality exercises the endpoint-locality predicate
// across every class the gate distinguishes. Locality — not provider name — is
// what the gate keys on, so a wrong verdict here is directly a wrong gate
// decision: a false "local" lets a hosted provider through, a false "remote"
// breaks a working local route.
func TestIsLocalEndpointURL_Locality(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		// --- loopback -----------------------------------------------------
		{"loopback ipv4 with port", "http://127.0.0.1:18434", true},
		{"loopback ipv4 whole 127/8 block not just 127.0.0.1", "http://127.5.6.7:8000", true},
		{"hostname localhost", "http://localhost:11434", true},
		{"hostname localhost no port", "http://localhost", true},
		{"subdomain of .localhost", "http://coder.localhost:18434", true},
		{"loopback ipv6 bracketed", "http://[::1]:8080", true},
		{"loopback ipv6 bracketed no port", "http://[::1]", true},

		// --- unspecified --------------------------------------------------
		{"unspecified ipv4", "http://0.0.0.0:8000", true},
		{"unspecified ipv6", "http://[::]:8000", true},

		// --- RFC1918 private ----------------------------------------------
		{"private 10/8", "http://10.0.0.42:8000", true},
		{"private 172.16/12", "http://172.16.9.9:1234", true},
		{"private 172.31/12 upper bound", "http://172.31.255.254:1234", true},
		{"private 192.168/16 LAN gpu box", "http://192.168.1.50:8000", true},

		// --- link-local ---------------------------------------------------
		{"link-local ipv4 169.254/16", "http://169.254.10.1:8080", true},
		{"link-local ipv6 fe80::/10", "http://[fe80::1]:8080", true},

		// --- CGNAT / unique-local -----------------------------------------
		{"CGNAT 100.64/10", "http://100.64.0.1:8000", true},
		{"CGNAT 100.127/10 upper bound", "http://100.127.255.254:8000", true},
		{"IPv6 unique-local fc00::/7", "http://[fd00::1]:8080", true},

		// --- bare LAN hostnames -------------------------------------------
		{"bare hostname no dots", "http://coder:18434", true},
		{"bare hostname no dots no port", "http://gpu-box", true},

		// --- RFC 6762 .local / RFC 8375 .home.arpa (the FALSE-REFUSAL class)
		// Reserved, non-Internet-routable namespaces. ".local" is the default
		// LAN name of every macOS and Avahi host, so treating it as REMOTE
		// refused an operator's own GPU box — the §11.4.201 false refusal this
		// row set guards. The NEGATIVE rows below are what make the rule
		// falsifiable rather than a blanket substring pass: a genuinely
		// routable name that merely CONTAINS or STARTS WITH "local" must stay
		// REMOTE, so the suffix must be matched as a whole trailing label.
		{"mDNS .local LAN name", "http://gpu-box.local:8000", true},
		{"mDNS .local no port", "http://coder.local", true},
		{"mDNS .local is case-insensitive", "http://GPU-BOX.LOCAL:8000", true},
		{"multi-label under .local", "http://rack1.gpu-box.local:8000", true},
		{"RFC 8375 home.arpa apex", "http://home.arpa:8000", true},
		{"RFC 8375 name under home.arpa", "http://nas.home.arpa:8000", true},
		{"routable name ENDING in a different TLD, not .local", "https://local.example.com/v1", false},
		{"routable name merely CONTAINING local", "https://mylocal.example.com/v1", false},
		{"routable name whose label ends in local but TLD is com", "https://gpu-box.local.example.com/v1", false},
		{"home.arpa lookalike on a routable domain", "https://home.arpa.example.com/v1", false},

		// --- scheme-less input (must NOT be misparsed as a scheme) ---------
		{"scheme-less localhost with port", "localhost:18434", true},
		{"scheme-less private ip with port", "192.168.1.50:8000", true},
		{"scheme-less public host", "api.openai.com", false},

		// --- explicit port on a remote host --------------------------------
		{"public host with explicit port", "https://api.cerebras.ai:443/v1", false},

		// --- remote -------------------------------------------------------
		{"public hosted openai", "https://api.openai.com/v1", false},
		{"public hosted together", "https://api.together.xyz/v1", false},
		{"public ipv4", "http://8.8.8.8:8000", false},
		{"public ipv6", "http://[2606:4700:4700::1111]:8080", false},
		// The reason the predicate PARSES instead of substring-matching: a
		// substring test for "localhost" would call this one LOCAL.
		{"public host merely CONTAINING localhost", "https://localhost.evil.example.com/v1", false},
		{"172.32 is OUTSIDE the 172.16/12 private block", "http://172.32.0.1:8000", false},
		{"100.128 is OUTSIDE the 100.64/10 CGNAT block", "http://100.128.0.1:8000", false},

		// --- RFC 6874 zoned IPv6 literals (the BYPASS class) --------------
		// net.ParseIP returns nil for ANY zoned address (stdlib net/ip.go
		// parseIP: `if err != nil || ip.Zone() != "" { return ..., false }`),
		// and net/url PRESERVES an RFC 6874 %25-escaped zone in Hostname().
		// So a zoned literal fell through to the BARE-HOSTNAME branch, which
		// only asks "does it contain a dot?" — and a colon-bearing IPv6
		// literal never does. A globally-routable address was therefore
		// verdicted LOCAL and constructed a hosted provider with the gate
		// CLOSED. The predicate must parse zones, and any IP-literal SHAPE
		// that still fails to parse must fail CLOSED rather than reach the
		// hostname branch that was never meant to receive colons.
		{"zoned PUBLIC ipv6 must NOT be local", "http://[2606:4700:4700::1111%25eth0]:8080", false},
		{"zoned link-local ipv6 stays local after zone strip", "http://[fe80::1%25eth0]:8080", true},
		{"ipv4-mapped ipv6 loopback", "http://[::ffff:127.0.0.1]:8080", true},
		{"ipv4-mapped ipv6 public", "http://[::ffff:8.8.8.8]:8080", false},

		// --- behaviour PINS (documented, deliberately unchanged) ----------
		// These two rows assert what the predicate ACTUALLY does today; neither
		// behaviour was altered by the .local work above, and both are recorded
		// here so a future change to either is a visible, deliberate decision
		// rather than an unnoticed side effect.
		//
		// TRAILING-DOT FQDN: "localhost." is the fully-qualified root-anchored
		// form of "localhost" and resolves identically in the DNS, but this
		// predicate compares host STRINGS and does not strip the root label, so
		// the trailing dot misses both the "localhost" equality test and the
		// ".localhost" suffix test, contains a dot, and is verdicted REMOTE.
		// That is fail-CLOSED (a refusal, never an admission), which is the safe
		// direction for a gate — an operator hitting it can drop the trailing
		// dot. Documented, not silently relied upon.
		{"trailing-dot FQDN localhost. is NOT recognised (fail-closed)", "http://localhost./v1", false},
		// LITERAL UNESCAPED '%' IN HOST: '%' introduces a percent-escape in a
		// URL, so "gpu%box" is an invalid escape sequence ("%bo" is not two hex
		// digits) and url.Parse errors outright — the predicate's documented
		// "an unparseable URL is NOT local" contract, reached here before the
		// separate ContainsAny(host, ":%") fail-closed branch that exists for
		// literals which DO parse.
		{"literal unescaped percent in host is unparseable (fail-closed)", "http://gpu%box.local:8000", false},

		// --- DOTLESS IPv4 FORMS (the SSRF-filter BYPASS class) ------------
		// An IPv4 address needs no dots. Measured against libc getaddrinfo on
		// the host this change was made on:
		//
		//     134744072    -> 8.8.8.8      (decimal-integer form  — PUBLIC)
		//     0x08080808   -> 8.8.8.8      (hex form              — PUBLIC)
		//     2130706433   -> 127.0.0.1    (decimal loopback)
		//     gpu-box      -> UNRESOLVED   (control: an ordinary dotless LAN name)
		//
		// The bare-hostname branch at the end of the predicate asks only "does
		// it contain a dot?", so BOTH public forms above were verdicted LOCAL
		// and walked straight past the gate. Go's own PURE resolver rejects
		// these spellings, so the dial fails today — but under the cgo resolver
		// (GODEBUG=netdns=cgo, a macOS build with cgo, an nsswitch configuration
		// that forces cgo) Go calls getaddrinfo and the address resolves. Latent
		// rather than live, but a real bypass of a security control, so the
		// predicate fails CLOSED on the SHAPE rather than on what happens to
		// resolve in one runtime configuration.
		{"dotless decimal-integer IPv4 (8.8.8.8) is NOT local", "http://134744072:8000", false},
		{"dotless hex IPv4 (8.8.8.8) is NOT local", "http://0x08080808:8000", false},
		{"dotless hex IPv4 with uppercase 0X prefix is NOT local", "http://0X08080808:8000", false},
		// Refusing the decimal-integer spelling of LOOPBACK is the deliberate
		// fail-closed half of the trade: nobody writes 127.0.0.1 this way, and
		// admitting the shape at all is exactly what re-opens the bypass above.
		{"dotless decimal-integer loopback is refused fail-closed", "http://2130706433:8000", false},
		// POSITIVE CONTROL — without this row the new rule could be "satisfied"
		// by refusing every dotless host, which would break ordinary LAN serving.
		{"ordinary dotless LAN name stays LOCAL (positive control)", "http://gpu-box:8000", true},

		// --- fail-closed inputs -------------------------------------------
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"unparseable control character in host", "http://exa\x7fmple.com", false},
		{"unparseable bad percent escape in host", "http://%zz/v1", false},
		{"scheme with no host", "http://", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := isLocalEndpointURL(tc.url); got != tc.want {
				t.Fatalf("isLocalEndpointURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestCloudGateClosed_LocalOpenAICompatibleStillConstructs is the REGRESSION
// GUARD for the trap in this change: NewOpenAICompatibleProvider is the
// constructor the LOCAL HelixLLM coder route and every local OpenAI-compatible
// server (llama.cpp, vLLM, LM Studio, LocalAI) go through. Gating it on
// provider name, or blanket-refusing while the gate is closed, would kill local
// serving — the opposite of the mandate. Local construction MUST be completely
// unaffected by a closed gate.
//
// 127.0.0.1:1 is used deliberately: nothing listens there, so the
// constructor's best-effort model-discovery probe fails fast (connection
// refused) and is only logged. This test asserts the GATE does not refuse;
// provider health is not its subject.
func TestCloudGateClosed_LocalOpenAICompatibleStillConstructs(t *testing.T) {
	closeCloudGateForTest(t)

	locals := []struct {
		name    string
		baseURL string
	}{
		// The local HelixLLM coder route (internal/server/llm_generate.go).
		{"helixllm", "http://localhost:18434"},
		// A local llama-server.
		{"llamacpp", "http://127.0.0.1:1"},
		// A local vLLM server.
		{"vllm", "http://127.0.0.1:1"},
		// A LAN-hosted LM Studio. Dialled at LOOPBACK, not at a real RFC1918
		// address: this constructor runs discoverModels() synchronously, so a
		// routable LAN address here put SYN/ARP traffic onto the operator's own
		// network on every test run (and stalled on ARP timeout when nothing
		// answered). That LAN address classification is proven with zero packets
		// by the pure isLocalEndpointURL table above ("private 192.168/16 LAN gpu
		// box"); what THIS test needs from the endpoint is only that the
		// constructor consults the predicate and does not refuse a local one.
		{"lmstudio", "http://127.0.0.1:1"},
		// No BaseURL at all → resolved through the name-default table, which
		// is loopback for every entry.
		{"localai", ""},
	}

	for _, l := range locals {
		prov, err := NewOpenAICompatibleProvider(l.name, OpenAICompatibleConfig{
			BaseURL:          l.baseURL,
			StreamingSupport: true,
			// Bounds the constructor's best-effort model-discovery probe. Without
			// it the LAN address below has no listener to refuse the connection
			// and the probe would sit on its own 10s context deadline. Discovery
			// failure is logged, never returned, so this does not affect the
			// gate assertion — it only keeps the test fast and offline-safe.
			Timeout: 50 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("local provider %q (base URL %q) was refused with the cloud gate "+
				"closed: %v — the gate must never block a LOCAL endpoint; local-only "+
				"serving is what the gate exists to protect", l.name, l.baseURL, err)
		}
		if prov == nil {
			t.Fatalf("local provider %q constructed nil without an error", l.name)
		}
		_ = prov.Close()
	}
}

// TestCloudGateClosed_RemoteOpenAICompatibleRefused closes the bypass itself:
// hosted OpenAI-compatible providers built directly through this constructor
// (the dynamic verifier catalogue and the hosted catalogue both do exactly
// that) MUST be refused while the gate is closed, with the SAME
// ErrCloudDisabled sentinel NewCloudProvider uses so callers branch on one
// error identity.
//
// .invalid hosts (RFC 2606) are used so a passing test can never depend on —
// or generate — real network egress to a hosted provider.
func TestCloudGateClosed_RemoteOpenAICompatibleRefused(t *testing.T) {
	closeCloudGateForTest(t)

	hosted := []struct {
		name    string
		baseURL string
	}{
		{"cerebras", "https://api.cerebras.invalid/v1"},
		{"together", "https://api.together.invalid/v1"},
		{"fireworks", "https://api.fireworks.invalid/inference/v1"},
		{"publicip", "http://8.8.8.8:8000/v1"},
	}

	for _, h := range hosted {
		prov, err := NewOpenAICompatibleProvider(h.name, OpenAICompatibleConfig{
			BaseURL:          h.baseURL,
			APIKey:           "test-key-not-a-real-credential",
			StreamingSupport: true,
			Timeout:          50 * time.Millisecond,
		})
		if err == nil {
			if prov != nil {
				_ = prov.Close()
			}
			t.Fatalf("hosted provider %q (%q) CONSTRUCTED although the cloud gate is "+
				"closed — this is the bypass: hosted OpenAI-compatible providers reach "+
				"this constructor without passing NewCloudProvider, so the gate must "+
				"also refuse here", h.name, h.baseURL)
		}
		if !errors.Is(err, ErrCloudDisabled) {
			t.Fatalf("hosted provider %q refusal error = %v, want it to wrap "+
				"ErrCloudDisabled so callers can branch on one sentinel", h.name, err)
		}
		if prov != nil {
			t.Fatalf("hosted provider %q returned a non-nil provider alongside the "+
				"gate refusal — a refused construction must yield nothing usable", h.name)
		}
	}
}

// TestCloudGateOpen_RemoteOpenAICompatibleConstructs is the positive side: when
// the operator explicitly sets llm.cloud.enabled true, the gate must step out
// of the way. Construction may still fail for genuine reasons (that is
// provider-level honesty), but the refusal MUST NOT be the gate's.
func TestCloudGateOpen_RemoteOpenAICompatibleConstructs(t *testing.T) {
	openCloudGateForTest(t) // shared helper, provider_factory_test.go

	prov, err := NewOpenAICompatibleProvider("cerebras", OpenAICompatibleConfig{
		BaseURL:          "https://api.cerebras.invalid/v1",
		APIKey:           "test-key-not-a-real-credential",
		StreamingSupport: true,
		Timeout:          50 * time.Millisecond,
	})
	if err != nil && errors.Is(err, ErrCloudDisabled) {
		t.Fatalf("gate refused a hosted provider although llm.cloud.enabled is true: %v", err)
	}
	if err != nil {
		t.Fatalf("hosted provider construction failed for a non-gate reason: %v "+
			"(model discovery is best-effort and must not fail construction)", err)
	}
	if prov == nil {
		t.Fatalf("hosted provider constructed nil without an error with the gate open")
	}
	_ = prov.Close()
}

// TestCloudGateClosed_DynamicCatalogueBuildsNothing exercises the ACTUAL
// reported bypass path end-to-end rather than only the constructor:
// BuildDynamicOpenAICompatibleProviders is one of the two call sites that
// reach NewOpenAICompatibleProvider WITHOUT passing NewCloudProvider. Before
// the gate was added at the constructor, this call built REAL hosted providers
// with llm.cloud.enabled false whenever a hosted key was exported — which is
// what the TUI and desktop then registered.
//
// The key is deliberately present (a dummy, never a real credential —
// CONST-042) so this test proves the GATE stops the build, not a missing key:
// with the key absent the builder would return 0 providers for an entirely
// different reason and the assertion would be a bluff.
func TestCloudGateClosed_DynamicCatalogueBuildsNothing(t *testing.T) {
	closeCloudGateForTest(t)
	t.Setenv("CEREBRAS_API_KEY", "cb-dummy-real-looking-value-123")

	recs := []verifier.VerifierProvider{
		{Name: "cerebras", APIURL: "https://verifier-says.cerebras.example/v1", IsActive: true, Status: "active"},
	}

	built := BuildDynamicOpenAICompatibleProviders(recs)
	for _, p := range built {
		_ = p.Close()
	}
	if len(built) != 0 {
		t.Fatalf("BuildDynamicOpenAICompatibleProviders built %d hosted provider(s) "+
			"with the cloud gate CLOSED and CEREBRAS_API_KEY exported — this is the "+
			"reported bypass: the gate must reach this path too", len(built))
	}
}

// TestCloudGateOpen_DynamicCatalogueBuildsProvider is the paired positive side
// of the test above: it proves the zero-count assertion there is caused by the
// GATE and not by some unrelated skip in the builder (an absent key, an
// inactive record, a blank api_url). Same inputs, gate OPEN, one provider
// built.
func TestCloudGateOpen_DynamicCatalogueBuildsProvider(t *testing.T) {
	openCloudGateForTest(t) // shared helper, provider_factory_test.go
	t.Setenv("CEREBRAS_API_KEY", "cb-dummy-real-looking-value-123")

	recs := []verifier.VerifierProvider{
		{Name: "cerebras", APIURL: "https://verifier-says.cerebras.example/v1", IsActive: true, Status: "active"},
	}

	built := BuildDynamicOpenAICompatibleProviders(recs)
	for _, p := range built {
		_ = p.Close()
	}
	if len(built) != 1 {
		t.Fatalf("BuildDynamicOpenAICompatibleProviders built %d providers with the "+
			"gate OPEN and the key present, want 1 — if this is 0 the gate-closed "+
			"assertion in TestCloudGateClosed_DynamicCatalogueBuildsNothing proves "+
			"nothing about the gate", len(built))
	}
}
