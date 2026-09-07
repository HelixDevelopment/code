// Package wirecorpus loads the committed corpus of REAL captured
// OpenAI-compatible wire responses at <repo>/helix_code/testdata/toolcall_wire_corpus.
//
// WHY THIS EXISTS (the measurement, §11.4.6 — do not delete this without
// re-measuring). Three guards used to assert on a LIVE model's tool-calling
// output. That output is NOT deterministic. Measured 2026-09-07 against the
// HelixLLM gateway at https://127.0.0.1:8443/v1 — 12 byte-identical POSTs,
// temperature 0, max_tokens 64, one fixed prompt and one fixed nonce:
//
//	8/12  finish_reason=tool_calls   tool_calls PRESENT
//	4/12  finish_reason=length       tool_calls ABSENT
//
// Two distinct outcomes for an identical request at temperature 0. The split
// was reproduced independently by running the affected guard five times in a
// row: PASS, FAIL, PASS, PASS, PASS. Retries, longer timeouts and larger token
// budgets cannot fix that — the non-determinism is in the system under test,
// not in the harness. So no assertion on a single live tool-calling outcome
// can ever be deterministic, and the operator's "all proofs MUST BE FULLY
// DETERMINISTIC" mandate forbids making one the default verdict.
//
// The resolution is record-and-replay, NOT fixture-writing. Every byte in this
// corpus was captured from the real gateway / real coder by
// helix_code/testdata/toolcall_wire_corpus/capture.py (the §11.4.77
// regeneration mechanism), including BOTH observed gateway outcomes — the
// tool_calls one AND the finish_reason=length one — so the corpus records an
// honest picture of a non-deterministic backend rather than a snapshot that
// quietly pretends the backend is deterministic. Replaying those bytes through
// the SAME production parsing/facade code path keeps the anti-bluff property
// (the bytes are real captured evidence per §11.4.5) while making the verdict
// a function of our code alone.
//
// ANTI-BLUFF SEAM (§11.4.107(10)): Load verifies the sha256 of every recorded
// body against provenance.json before returning it. A hand-edited "fixture"
// — the exact failure mode that would turn this corpus back into the thing it
// replaced — fails the load loudly instead of quietly passing.
//
// HONEST BOUNDARY (§11.4.6): a replay proves OUR parsing, routing and wire
// facades are correct for wire shapes the backend really produced. It cannot
// prove the backend still produces them today. That is why the live guards are
// kept as opt-in distribution probes rather than deleted — see
// TestGatewayLive_ToolCallingCapabilityDivergence.
package wirecorpus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// dirName is the corpus directory, relative to the inner Go module root.
const dirName = "testdata/toolcall_wire_corpus"

// Recorded body filenames. Named constants so a typo is a compile error rather
// than a silently-skipped assertion.
const (
	GatewayModels          = "gateway_models.json"
	GatewayWeatherToolCall = "gateway_weather_tool_calls.json"
	GatewayWeatherNoTool   = "gateway_weather_no_tool_calls.json"
	CoderModels            = "coder_models.json"
	CoderWeatherFencedStop = "coder_weather_fenced_stop.json"
)

// GatewayAdd names the idx-th recorded `add` tool-call response. The corpus
// holds one distinct real response per concurrent slot of the D6 guard, so a
// replay can still detect a response being matched to the wrong request.
func GatewayAdd(idx int) string { return fmt.Sprintf("gateway_add_%02d.json", idx) }

// Provenance mirrors the subset of provenance.json the guards rely on.
type Provenance struct {
	CapturedAtUTC   string `json:"captured_at_utc"`
	GatewayEndpoint string `json:"gateway_endpoint"`
	CoderEndpoint   string `json:"coder_endpoint"`
	GatewayModel    string `json:"gateway_model"`
	CoderModel      string `json:"coder_model"`
	FrozenNonce     string `json:"frozen_nonce"`
	Concurrency     int    `json:"concurrency"`

	NondeterminismMeasurement struct {
		Requests                            int    `json:"requests"`
		ToolCallsPresent                    int    `json:"tool_calls_present"`
		FinishReasonLengthToolCallsAbsent   int    `json:"finish_reason_length_tool_calls_absent"`
		Verdict                             string `json:"verdict"`
	} `json:"nondeterminism_measurement"`

	Files map[string]struct {
		SHA256 string `json:"sha256"`
		Bytes  int    `json:"bytes"`
	} `json:"files"`
}

// Corpus is a loaded, integrity-verified recording set.
type Corpus struct {
	Dir        string
	Provenance Provenance
	bodies     map[string][]byte
}

// Load reads and verifies the corpus. It FAILS the test (never skips, never
// degrades) when the corpus is missing or when any recorded body's sha256 no
// longer matches provenance.json: a corpus that cannot be trusted must not
// silently produce a green verdict.
func Load(t *testing.T) *Corpus {
	t.Helper()

	dir, err := locate()
	if err != nil {
		t.Fatalf("wirecorpus: %v (regenerate with "+
			"helix_code/testdata/toolcall_wire_corpus/capture.py)", err)
	}

	provBytes, err := os.ReadFile(filepath.Join(dir, "provenance.json"))
	if err != nil {
		t.Fatalf("wirecorpus: reading provenance.json: %v", err)
	}
	var prov Provenance
	if err := json.Unmarshal(provBytes, &prov); err != nil {
		t.Fatalf("wirecorpus: provenance.json is not valid JSON: %v", err)
	}
	if prov.CapturedAtUTC == "" || prov.GatewayEndpoint == "" || len(prov.Files) == 0 {
		t.Fatalf("wirecorpus: provenance.json does not record a real capture "+
			"(captured_at=%q gateway=%q files=%d) — a corpus with no provenance "+
			"is indistinguishable from a hand-written fixture",
			prov.CapturedAtUTC, prov.GatewayEndpoint, len(prov.Files))
	}

	c := &Corpus{Dir: dir, Provenance: prov, bodies: map[string][]byte{}}
	for name, meta := range prov.Files {
		raw, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Fatalf("wirecorpus: recorded body %q listed in provenance is missing: %v", name, readErr)
		}
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != meta.SHA256 {
			t.Fatalf("wirecorpus: recorded body %q has been MODIFIED since capture\n"+
				"  provenance sha256 = %s\n  on-disk    sha256 = %s\n"+
				"These bytes are captured evidence (§11.4.5), not an editable fixture. "+
				"Re-record with helix_code/testdata/toolcall_wire_corpus/capture.py "+
				"instead of editing them by hand.", name, meta.SHA256, got)
		}
		c.bodies[name] = raw
	}
	return c
}

// Body returns the verified bytes of one recorded response.
func (c *Corpus) Body(t *testing.T, name string) []byte {
	t.Helper()
	raw, ok := c.bodies[name]
	if !ok {
		t.Fatalf("wirecorpus: no recorded body named %q in %s", name, c.Dir)
	}
	return raw
}

// locate walks up from the working directory looking for the corpus. Tests run
// with cwd set to their own package directory, which sits at different depths,
// so the location is discovered rather than hardcoded (§11.4.111 / §11.4.177 —
// no operator's checkout path baked into tracked code).
func locate() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; {
		candidate := filepath.Join(dir, dirName)
		if _, statErr := os.Stat(filepath.Join(candidate, "provenance.json")); statErr == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("corpus directory %q not found by walking up from %s", dirName, cwd)
		}
		dir = parent
	}
}
