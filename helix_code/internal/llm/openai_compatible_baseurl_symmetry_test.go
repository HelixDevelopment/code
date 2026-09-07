package llm

// openai_compatible_baseurl_symmetry_test.go — guard for "the gate judges the
// URL that will actually be dialled".
//
// THE DEFECT. NewOpenAICompatibleProvider's W2c-1 check resolved the effective
// base URL with `TrimSpace(BaseURL) == ""`, so a whitespace-only BaseURL was
// judged as the backend's localhost default and PASSED the gate. getAPIURL then
// resolved the SAME field with `BaseURL == ""`, so the provider went on to dial
// the literal `"  " + endpoint`. Nothing leaked — whitespace is not a host, and
// the dial simply fails — but the gate and the dial disagreed about one input,
// which is precisely the asymmetry the sibling KoboldAI gate's own comment
// argues against, and it produced a request URL nothing could serve.
//
// Both halves now resolve through effectiveOpenAICompatibleBaseURL, so the URL
// the gate judges is byte-for-byte the URL getAPIURL builds.

import (
	"strings"
	"testing"
)

func TestEffectiveBaseURL_GateAndDialAgree(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		configured string
		want       string
	}{
		{"explicit url is used verbatim", "vllm", "http://10.0.0.5:8000", "http://10.0.0.5:8000"},
		{"empty falls back to the named local default", "vllm", "", "http://localhost:8000"},
		// The defect's input. Whitespace-only used to mean "default" to the
		// gate and "the literal whitespace" to the dial.
		{"whitespace-only falls back to the named local default", "vllm", "   ", "http://localhost:8000"},
		{"tab and newline count as whitespace too", "lmstudio", "\t\n ", "http://localhost:1234"},
		{"surrounding whitespace is trimmed off a real url", "vllm", "  http://10.0.0.5:8000  ", "http://10.0.0.5:8000"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveOpenAICompatibleBaseURL(tc.provider, tc.configured)
			if got != tc.want {
				t.Fatalf("effectiveOpenAICompatibleBaseURL(%q, %q) = %q, want %q",
					tc.provider, tc.configured, got, tc.want)
			}
		})
	}
}

// TestWhitespaceBaseURL_GateVerdictMatchesDialledURL is the end-to-end half: it
// constructs a REAL provider with the gate CLOSED and a whitespace-only base
// URL, and asserts BOTH that the gate admitted it (it resolves to loopback) AND
// that the URL it will dial is that same loopback address rather than a
// whitespace-prefixed malformed one.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): revert getAPIURL to
// `baseURL := p.config.BaseURL; if baseURL == "" { ... }` — construction still
// succeeds, and the dialled-URL assertion below fails on "  /v1/models".
func TestWhitespaceBaseURL_GateVerdictMatchesDialledURL(t *testing.T) {
	prev := CloudEnabled()
	SetCloudEnabled(false)
	t.Cleanup(func() { SetCloudEnabled(prev) })

	// Port 1: nothing listens, so the constructor's best-effort model probe
	// fails fast and is only logged. The subject here is the URL, not health.
	p, err := NewOpenAICompatibleProvider("vllm", OpenAICompatibleConfig{BaseURL: "   "})
	if err != nil {
		t.Fatalf("construction refused: %v — a whitespace-only base URL resolves "+
			"to the backend's LOOPBACK default, which the gate must admit", err)
	}
	if p == nil {
		t.Fatal("construction returned (nil, nil)")
	}
	t.Cleanup(func() { _ = p.Close() })

	got := p.getAPIURL("/v1/models")
	if strings.TrimSpace(got) != got {
		t.Fatalf("getAPIURL(%q) = %q — the dialled URL carries the raw whitespace "+
			"the gate had already resolved away. The gate judged a DIFFERENT URL "+
			"than the one being dialled, which is the defect this guards.",
			"/v1/models", got)
	}
	if want := "http://localhost:8000/v1/models"; got != want {
		t.Fatalf("getAPIURL(\"/v1/models\") = %q, want %q — the gate admitted this "+
			"provider because it resolved to that loopback default, so that is the "+
			"address it must dial", got, want)
	}
}

// TestKoboldAIBlankEndpoint_RefusalSaysBlank pins the wording half of the same
// fix. KoboldAI's own resolution is deliberately NOT changed — a blank endpoint
// stays fail-closed there — but reporting it as `endpoint "  " is not local`
// reads as though some host had been examined and judged foreign, which sends
// an operator looking for the wrong problem.
func TestKoboldAIBlankEndpoint_RefusalSaysBlank(t *testing.T) {
	prev := CloudEnabled()
	SetCloudEnabled(false)
	t.Cleanup(func() { SetCloudEnabled(prev) })

	_, err := NewKoboldAIProvider(KoboldAIConfig{BaseURL: "   "})
	if err == nil {
		t.Fatal("a blank KoboldAI endpoint must still be refused fail-closed with " +
			"the gate closed — this fix changes the wording, not the verdict")
	}
	if !strings.Contains(err.Error(), "blank") {
		t.Fatalf("refusal message %q does not describe the endpoint as blank; it "+
			"reads as though a host was examined and judged remote", err.Error())
	}

	// The remote case must keep naming the offending endpoint verbatim.
	_, remoteErr := NewKoboldAIProvider(KoboldAIConfig{BaseURL: "https://api.together.xyz"})
	if remoteErr == nil {
		t.Fatal("a remote KoboldAI endpoint must be refused with the gate closed")
	}
	if !strings.Contains(remoteErr.Error(), "api.together.xyz") {
		t.Fatalf("refusal message %q no longer names the offending endpoint", remoteErr.Error())
	}
}
