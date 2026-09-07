package llm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// openai_compatible_gateway_tls_test.go — regression guards for HXC-002-F3-03
// (gap ledger docs/qa/2026-09-05-gap-ledger.md): the TLS-trust half of the
// HelixLLM gateway route (gateway https://127.0.0.1:8443/v1, self-signed,
// CA at repo-root submodules/helix_llm/certs/cert.pem).
//
// BEFORE this work, OpenAICompatibleConfig carried NO TLS/CA field at all —
// NewOpenAICompatibleProvider always built a bare `&http.Client{Timeout:
// config.Timeout}`, so HTTPS to a self-signed endpoint like the gateway
// failed certificate verification unconditionally.
//
// CURRENT STATE (verified 2026-09-06, superseding this file's original
// forward-looking header). The CA field HAS LANDED: it is
// `OpenAICompatibleConfig.CACertFile string` (openai_compatible_provider.go:79)
// and it is wired into the constructed client's transport as
// `TLSClientConfig: &tls.Config{RootCAs: pool}` (around line 290), with the
// empty-value case still taking the plain-client path. The guards below
// therefore RUN — they are no longer waiting on anything.
//
// The original header described the field as not-yet-landed and referred to a
// grep returning zero hits. That reflection was accurate when written and is
// now STALE, so it has been rewritten rather than left to mislead a reader
// into believing this file is inert.
//
// WHY THE REFLECTION LOOKUP REMAINS. The field NAME was owned by a concurrent
// stream when these guards were authored, so they locate it by reflection over
// a closed candidate-name list (gatewayCACandidateFieldNames) instead of a
// literal reference. "CACertFile" is in that list, so the lookup resolves on
// the first pass today. The indirection is kept deliberately: it costs one map
// lookup, it keeps this file compiling if the field is ever renamed, and its
// SKIP path is the honest fallback for that case — never a fake PASS.
//
// Run:
//
//	cd helix_code && go test -v -run TestOpenAICompatibleProvider_GatewayTLS ./internal/llm/...

// gatewayCACandidateFieldNames is the closed set of plausible exported field
// names the concurrent stream might add to OpenAICompatibleConfig for the CA
// trust anchor. Tried in order; the first settable match wins. Extend this
// list (never rename it away) if the landed field name is not among them —
// that is the ONLY maintenance this file should ever need for the field-name
// question.
var gatewayCACandidateFieldNames = []string{
	"CAFile", "CACertFile", "CACertPath", "CAPath", "TLSCAFile", "TLSCACertFile",
	"TLSCAPath", "CABundleFile", "CABundle", "RootCAFile", "RootCAPath",
	"CAPEMFile", "TrustedCAFile", "ServerCAFile", "CertPoolFile", "CustomCAFile",
	"TLSCAPEMFile", "CACertificateFile",
}

// trySetGatewayCAField attempts to set path (a filesystem path to a PEM CA
// certificate) onto the first field of cfg whose name appears in
// gatewayCACandidateFieldNames. Supports both a string (file-path) field and
// a []byte (raw PEM content) field — whichever shape the concurrent stream
// chose. Returns the field name it set and true on success; false if no
// candidate field exists yet (compile-safe: this is a runtime reflection
// probe, not a compile-time reference).
func trySetGatewayCAField(cfg *OpenAICompatibleConfig, path string) (string, bool) {
	v := reflect.ValueOf(cfg).Elem()
	for _, name := range gatewayCACandidateFieldNames {
		f := v.FieldByName(name)
		if !f.IsValid() || !f.CanSet() {
			continue
		}
		switch {
		case f.Kind() == reflect.String:
			f.SetString(path)
			return name, true
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.Uint8:
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			f.SetBytes(data)
			return name, true
		}
	}
	return "", false
}

// setGatewayCAFieldOrSkip is the test-facing wrapper: it sets the CA field or
// honestly SKIPs the calling test with a message naming every candidate tried
// — never a fake pass, never a compile failure — when the concurrent stream
// has not yet landed the field.
func setGatewayCAFieldOrSkip(t *testing.T, cfg *OpenAICompatibleConfig, path string) string {
	t.Helper()
	name, ok := trySetGatewayCAField(cfg, path)
	if !ok {
		// SKIP-OK: #HXC-002-F3-03 — unreachable on the current tree and
		// deliberately retained as an honest fallback. The CA field HAS landed
		// as OpenAICompatibleConfig.CACertFile, which IS in
		// gatewayCACandidateFieldNames, so trySetGatewayCAField resolves and
		// this branch does not execute (confirmed by these guards running
		// rather than skipping). It fires only if that field is renamed to
		// something outside the candidate list, in which case skipping with the
		// list of names tried is the correct outcome: the guard cannot set up
		// its precondition, and reporting PASS without having exercised TLS
		// trust would be precisely the false-success this file exists to
		// prevent. The fix when it fires is to extend the candidate list, never
		// to weaken an assertion.
		t.Skipf("SKIP-OK: #HXC-002-F3-03 — OpenAICompatibleConfig has no recognizable "+
			"CA-trust field (tried: %s). This guard is written against "+
			"HXC-002-F3-03's documented contract; the field is expected to be "+
			"CACertFile. If it has been renamed, add the new name to "+
			"gatewayCACandidateFieldNames and re-run",
			strings.Join(gatewayCACandidateFieldNames, ", "))
	}
	return name
}

// pemEncodeCert wraps a DER certificate in a PEM "CERTIFICATE" block.
func pemEncodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// writeCAPEMFile writes pemBytes to a fresh temp file under t.TempDir() and
// returns its path.
func writeCAPEMFile(t *testing.T, pemBytes []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
	return path
}

// generateUnrelatedSelfSignedCertDER creates a fresh, self-signed CA
// certificate that has NOTHING to do with any httptest server — used to
// prove that a CA pool NOT containing the target endpoint's certificate
// correctly FAILS verification (the negative half of the CA-trust guard).
func generateUnrelatedSelfSignedCertDER(t *testing.T) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "hxc-002-f3-03-unrelated-guard-fixture"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	return der
}

// gatewayTLSFixtureHandler answers /v1/models with a single fixture model —
// enough for GetHealth (which only needs a 200 + a decodable models body) to
// report "healthy".
func gatewayTLSFixtureHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{{"id": "gateway-tls-guard-fixture-model"}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
}

// --- Guard 1: CA trust is real, not skipped -------------------------------
//
// test: TestOpenAICompatibleProvider_GatewayTLS_CATrustIsReal/configured_CA_verifies_successfully
// asserts: GetHealth() against an httptest.NewTLSServer succeeds (err==nil,
//
//	Status=="healthy") once the server's OWN leaf certificate is supplied as
//	the trusted CA via the (reflection-located) config field.
//
// mutation that makes it FAIL: revert/omit the CA-wiring code so
// NewOpenAICompatibleProvider always builds a bare `&http.Client{Timeout:
// config.Timeout}` regardless of the CA field's value — GetHealth would then
// fail with an x509 "certificate signed by unknown authority" error against
// this self-signed TLS fixture, and require.NoError would fail.
//
// test: .../endpoint_cert_NOT_in_pool_must_fail
// asserts: GetHealth() FAILS when the configured CA pool contains a
//
//	DIFFERENT, unrelated self-signed certificate — the fixture server's own
//	leaf is deliberately NOT trusted.
//
// mutation that makes it FAIL: implement the CA wiring so ANY non-empty CA
// field value results in a permissive/empty CertPool (or InsecureSkipVerify)
// rather than a pool containing ONLY the supplied certificate — GetHealth
// would then unexpectedly succeed against the wrong-CA fixture, and
// require.Error would fail.
func TestOpenAICompatibleProvider_GatewayTLS_CATrustIsReal(t *testing.T) {
	ts := httptest.NewTLSServer(gatewayTLSFixtureHandler())
	defer ts.Close()

	t.Run("configured_CA_verifies_successfully", func(t *testing.T) {
		caPath := writeCAPEMFile(t, pemEncodeCert(ts.Certificate().Raw))

		cfg := OpenAICompatibleConfig{BaseURL: ts.URL, Timeout: 5 * time.Second}
		fieldUsed := setGatewayCAFieldOrSkip(t, &cfg, caPath)

		provider, err := NewOpenAICompatibleProvider("gateway-tls-trust-fixture", cfg)
		require.NoError(t, err)
		defer func() { _ = provider.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		health, healthErr := provider.GetHealth(ctx)
		require.NoErrorf(t, healthErr,
			"GetHealth against the TLS test server must succeed once its own leaf "+
				"cert is trusted via config field %q — a REAL TLS verification, "+
				"never a bypass", fieldUsed)
		require.Equal(t, "healthy", health.Status)
		t.Logf("PASS: CA trust via field %q verified the TLS fixture server (status=%s, latency=%s)",
			fieldUsed, health.Status, health.Latency)
	})

	t.Run("endpoint_cert_NOT_in_pool_must_fail", func(t *testing.T) {
		wrongCAPath := writeCAPEMFile(t, pemEncodeCert(generateUnrelatedSelfSignedCertDER(t)))

		cfg := OpenAICompatibleConfig{BaseURL: ts.URL, Timeout: 5 * time.Second}
		fieldUsed := setGatewayCAFieldOrSkip(t, &cfg, wrongCAPath)

		provider, err := NewOpenAICompatibleProvider("gateway-tls-wrong-ca", cfg)
		require.NoError(t, err) // construction must not fail merely because a LATER request will fail verification
		defer func() { _ = provider.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, healthErr := provider.GetHealth(ctx)
		require.Errorf(t, healthErr,
			"an HTTPS endpoint whose certificate is NOT in the configured CA pool "+
				"(field %q) must FAIL verification, never silently succeed", fieldUsed)
		t.Logf("PASS: wrong-CA pool via field %q correctly FAILED verification: %v", fieldUsed, healthErr)
	})
}

// --- Guard 2: empty CA field ⇒ unchanged behaviour ------------------------
//
// test: TestOpenAICompatibleProvider_GatewayTLS_EmptyCAFieldUnchangedBehaviour
// asserts: a config with NO CA field set at all still builds a working plain
//
//	HTTP client exactly as before — GetHealth against a plain httptest.Server
//	(the shape the ~11 pre-existing local backends this provider fronts all
//	use) succeeds and reports "healthy".
//
// mutation that makes it FAIL: make the CA-wiring code path unconditionally
// active (e.g., always construct a custom *http.Transport even when the CA
// field is empty, in a way that mishandles a plain "http://" BaseURL) — this
// guard exercises exactly the "no CA configured" shape the vast majority of
// OpenAICompatibleProvider's existing callers use, and any regression there
// makes GetHealth fail or the whole construction path error.
func TestOpenAICompatibleProvider_GatewayTLS_EmptyCAFieldUnchangedBehaviour(t *testing.T) {
	server := setupOpenAICompatibleTestServer(t)
	defer server.Close()

	cfg := OpenAICompatibleConfig{
		BaseURL:      server.URL,
		DefaultModel: "llama-3-8b",
		Timeout:      5 * time.Second,
		// Deliberately NOT setting any CA/TLS field — this is the "no CA
		// configured" shape every pre-existing OpenAICompatibleProvider caller
		// (VLLM, LMStudio, LocalAI, the local HelixLLM coder route, etc.) uses.
	}
	provider, err := NewOpenAICompatibleProvider("plain-http-unaffected", cfg)
	require.NoError(t, err)
	defer func() { _ = provider.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	health, healthErr := provider.GetHealth(ctx)
	require.NoError(t, healthErr, "an empty/unset CA field must not regress plain-HTTP backends")
	require.Equal(t, "healthy", health.Status)
	t.Logf("PASS: empty-CA config against a plain HTTP fixture behaves unchanged (status=%s)", health.Status)
}

// --- Guard 3: missing / unparseable CA file ⇒ clear error, NEVER a silent
//
//	insecure fallback -------------------------------------------------
//
// test: TestOpenAICompatibleProvider_GatewayTLS_MissingOrUnparseableCA_NeverInsecureFallback
// asserts: for BOTH a nonexistent CA file path and a CA file containing
//
//	unparseable garbage, the provider NEVER ends up trusting the fixture TLS
//	server's self-signed certificate — either construction rejects the bad
//	CA outright (a clear, immediate error — logged and accepted as
//	compliant), or, if construction succeeds, every subsequent GetHealth call
//	against the (still self-signed, still untrusted-by-default) fixture MUST
//	fail.
//
// mutation that makes it FAIL: change the CA-loading code so that a
// read/parse error for the CA file falls back to `InsecureSkipVerify: true`
// (or an equivalent trust-everything transport) instead of propagating the
// error — GetHealth would then unexpectedly SUCCEED against the untrusted
// self-signed fixture, and require.Error would fail. This is the exact bluff
// this guard exists to catch: a bad CA config must never silently widen to
// "trust everything".
func TestOpenAICompatibleProvider_GatewayTLS_MissingOrUnparseableCA_NeverInsecureFallback(t *testing.T) {
	ts := httptest.NewTLSServer(gatewayTLSFixtureHandler())
	defer ts.Close()

	cases := []struct {
		name       string
		caContents string // "" ⇒ path points at a file that does not exist at all
	}{
		{name: "nonexistent_file", caContents: ""},
		{name: "unparseable_content", caContents: "this-is-not-a-pem-certificate\n-----GARBAGE-----\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var caPath string
			if tc.caContents == "" {
				caPath = filepath.Join(t.TempDir(), "does-not-exist.pem")
			} else {
				caPath = writeCAPEMFile(t, []byte(tc.caContents))
			}

			cfg := OpenAICompatibleConfig{BaseURL: ts.URL, Timeout: 5 * time.Second}
			fieldUsed := setGatewayCAFieldOrSkip(t, &cfg, caPath)

			provider, ctorErr := NewOpenAICompatibleProvider("bad-ca-guard-"+tc.name, cfg)
			if ctorErr != nil {
				// A clear construction-time rejection of the bad CA is exactly
				// what this guard requires — nothing left to prove for this case.
				t.Logf("PASS: construction rejected the bad CA (field %q) up front: %v", fieldUsed, ctorErr)
				return
			}
			defer func() { _ = provider.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, healthErr := provider.GetHealth(ctx)
			require.Errorf(t, healthErr,
				"a %s CA file (field %q) must never silently fall back to a "+
					"trust-everything client — GetHealth against the self-signed "+
					"TLS fixture must fail, not succeed", tc.name, fieldUsed)
			t.Logf("PASS: %s CA file (field %q) correctly failed verification: %v", tc.name, fieldUsed, healthErr)
		})
	}
}

// --- Guard 4: the CA path must not silently discard the stdlib transport
//
//	defaults ----------------------------------------------------------
//
// test: TestOpenAICompatibleProvider_CAPathPreservesDefaultTransportSettings
// asserts: when config.CACertFile is set, the resulting *http.Client's
//
//	Transport is a CLONE of http.DefaultTransport with only TLSClientConfig
//	replaced — so ProxyFromEnvironment, the dial and TLS-handshake timeouts,
//	the idle-connection pool limits and ForceAttemptHTTP2 all survive, and
//	the private CA is genuinely in the trust pool.
//
// the defect it pins: newOpenAICompatibleHTTPClient used to hand-build
// `&http.Transport{TLSClientConfig: ...}`. A zero-value http.Transport has
// NONE of those defaults, so merely configuring a private CA also turned off
// proxy support, removed every connect/handshake timeout, made the idle-conn
// pool unbounded, and — because net/http only auto-negotiates HTTP/2 when
// TLSClientConfig is nil — silently downgraded the connection to HTTP/1.1.
// None of that is visible from the call site; it only shows up as a hang
// behind a proxy or an FD leak under load.
//
// this test is not tautological: it compares each field against the LIVE
// http.DefaultTransport rather than against a hardcoded expectation, so it
// keeps meaning if the stdlib changes its defaults.
//
// mutation that makes it FAIL (§1.1): replace the
// `transport := clonedDefaultTransport(name); transport.TLSClientConfig = ...`
// pair in newOpenAICompatibleHTTPClient with the old
// `&http.Transport{TLSClientConfig: tlsConfig}` — Proxy goes nil,
// TLSHandshakeTimeout goes 0, MaxIdleConns goes 0 and ForceAttemptHTTP2 goes
// false, and every assertion below fires.
func TestOpenAICompatibleProvider_CAPathPreservesDefaultTransportSettings(t *testing.T) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Not a defect in the code under test — some other package in this
		// process replaced DefaultTransport, so there are no stdlib defaults
		// to compare against. Honest skip (§11.4.3) rather than a fabricated
		// verdict.
		t.Skipf("SKIP-OK: http.DefaultTransport is %T, not *http.Transport — "+
			"nothing to compare the clone against", http.DefaultTransport)
	}

	// A real, parseable CA so construction takes path (2). Contents are
	// irrelevant to the transport assertions; only that the path is taken.
	caDER := generateUnrelatedSelfSignedCertDER(t)
	caPath := writeCAPEMFile(t, pemEncodeCert(caDER))

	cfg := OpenAICompatibleConfig{
		BaseURL:      "http://127.0.0.1:1", // never dialled by this test
		DefaultModel: "llama-3-8b",
		Timeout:      7 * time.Second,
	}
	setGatewayCAFieldOrSkip(t, &cfg, caPath)

	client, err := newOpenAICompatibleHTTPClient("transport-defaults", cfg)
	require.NoError(t, err)
	require.Equal(t, 7*time.Second, client.Timeout,
		"the deliberately-configured client timeout must survive")

	tr, isTransport := client.Transport.(*http.Transport)
	require.Truef(t, isTransport, "CA path produced Transport %T, want *http.Transport", client.Transport)

	// The whole point of the fix: the private CA is actually installed...
	require.NotNil(t, tr.TLSClientConfig, "CA path must set a TLSClientConfig")
	require.NotNil(t, tr.TLSClientConfig.RootCAs, "CA path must install a root pool")
	require.Equal(t, uint16(tls.VersionTLS12), tr.TLSClientConfig.MinVersion)
	require.False(t, tr.TLSClientConfig.InsecureSkipVerify,
		"verification must never be disabled on the CA path")

	// ...WITHOUT costing the stdlib defaults. Each is compared to the live
	// DefaultTransport, and each corresponds to a concrete failure mode:
	require.NotNil(t, tr.Proxy,
		"Proxy is nil: ProxyFromEnvironment was discarded, so HTTP(S)_PROXY/NO_PROXY "+
			"are ignored and a provider behind a corporate proxy cannot connect")
	require.NotNil(t, tr.DialContext,
		"DialContext is nil: the stdlib dialer (30s connect timeout + keep-alive) "+
			"was discarded, so a black-holed endpoint hangs instead of erroring")
	require.Equal(t, base.TLSHandshakeTimeout, tr.TLSHandshakeTimeout,
		"TLSHandshakeTimeout differs from DefaultTransport: a stalled handshake "+
			"would never time out")
	require.Equal(t, base.ExpectContinueTimeout, tr.ExpectContinueTimeout,
		"ExpectContinueTimeout differs from DefaultTransport")
	require.Equal(t, base.MaxIdleConns, tr.MaxIdleConns,
		"MaxIdleConns differs from DefaultTransport: an unbounded idle pool is an FD leak")
	require.Equal(t, base.IdleConnTimeout, tr.IdleConnTimeout,
		"IdleConnTimeout differs from DefaultTransport: idle connections are never reaped")
	require.Equal(t, base.ForceAttemptHTTP2, tr.ForceAttemptHTTP2,
		"ForceAttemptHTTP2 differs from DefaultTransport: with a custom TLSClientConfig "+
			"net/http does NOT auto-upgrade, so the connection silently drops to HTTP/1.1")

	// A clone, not an alias — this provider's private trust pool must not
	// reach the shared DefaultTransport that every other HTTP client in the
	// process uses.
	//
	// NOTE ON WHAT IS *NOT* ASSERTED HERE, because it was tried and is wrong:
	// snapshotting base.TLSClientConfig before construction and requiring it
	// to be unchanged afterwards FAILS on correct code. Transport.Clone()
	// begins with `t.nextProtoOnce.Do(t.onceSetNextProtoDefaults)`, so cloning
	// DefaultTransport triggers ITS lazy HTTP/2 bootstrap, and that bootstrap
	// assigns DefaultTransport.TLSClientConfig a config carrying NextProtos
	// {h2, http/1.1}. That mutation is the stdlib's, is idempotent, and
	// carries a nil RootCAs — it is not a leak. (Observed directly: the
	// snapshot assertion failed with exactly that config, RootCAs nil.)
	// Asserting on it would make this guard fail for a reason unrelated to the
	// code under test.
	//
	// The property that actually matters is pool identity: OUR pool must not
	// be reachable from the shared transport.
	require.NotSame(t, base, tr, "transport must be a clone, not http.DefaultTransport itself")
	require.NotSame(t, base.TLSClientConfig, tr.TLSClientConfig,
		"the clone shares its TLSClientConfig object with http.DefaultTransport — "+
			"a later mutation of ours would reach every other HTTP client")
	if base.TLSClientConfig != nil {
		require.NotSame(t, tr.TLSClientConfig.RootCAs, base.TLSClientConfig.RootCAs,
			"http.DefaultTransport now trusts this provider's private CA pool — the "+
				"private-CA trust leaked process-wide")
	}

	t.Logf("PASS: CA path preserves DefaultTransport settings "+
		"(proxy=%v dial=%v handshake=%v maxIdle=%d idleTimeout=%v http2=%v)",
		tr.Proxy != nil, tr.DialContext != nil, tr.TLSHandshakeTimeout,
		tr.MaxIdleConns, tr.IdleConnTimeout, tr.ForceAttemptHTTP2)
}
