package llm

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// cloud_gate_koboldai_test.go — guards the KoboldAI arm of the W2c-1 cloud
// gate (operator mandate 2026-09-05, local-only adaptive serving).
//
// THE DEFECT THIS CLOSES. KoboldAI was listed in factory.go's
// isCloudGateExemptProviderType under the "LOCAL BY IDENTITY" leg, alongside
// Ollama and llama.cpp. That leg is sound for those two and UNSOUND for
// KoboldAI, and the discriminator is a CREDENTIAL PATH, not a name:
//
//   - ollama_provider.go and llamacpp_provider.go contain ZERO occurrences of
//     APIKey / Authorization / Bearer. They cannot carry a secret anywhere,
//     so exempting them by identity leaks nothing no matter what endpoint a
//     caller supplies.
//   - koboldai_provider.go sets `Authorization: Bearer <APIKey>` on every one
//     of its four outbound requests (lines 252, 316, 420, 462), and
//     factory.go builds KoboldAIConfig{BaseURL: config.Endpoint, APIKey:
//     config.APIKey} straight from caller-supplied values. It also did NOT
//     self-gate: the file had zero occurrences of cloudGate /
//     isLocalEndpointURL / ErrCloudDisabled.
//
// Identity-exemption plus a credential path plus a caller-chosen endpoint is
// exactly a credential-exfiltration primitive: with the gate CLOSED (the
// mandated default), NewProvider({koboldai, Endpoint: <public host>, APIKey:
// <secret>}) constructed successfully and shipped the bearer token to that
// public host. The gate's whole purpose is that nothing reaches a hosted
// endpoint by default; here it was not merely reached, it was reached WITH A
// SECRET ATTACHED.
//
// THE FIX. KoboldAI moves off the identity-exempt leg and onto the same
// "self-gates one level down on endpoint LOCALITY" leg the OpenAI-compatible
// family uses. The check lives inside NewKoboldAIProvider (koboldai_provider.go)
// rather than only in the factory arm, so a DIRECT caller of the exported
// constructor is covered too — there are four such call sites in this package's
// tests today and nothing stops a fifth being written in production code.
//
// §11.4.115 POLARITY. RED_MODE is not implemented here, for the same reason
// documented at length in cloud_gate_closed_test.go: the historical defect is
// the ABSENCE of a guard clause, and the only lever a test in this package has
// (SetCloudEnabled) opens the real gate, which is correct behaviour rather
// than the defect. The RED observation was captured by EXECUTION against the
// pre-fix tree instead — TestNewProvider_KoboldAI_RefusedAtRemoteEndpoint
// FAILED on both remote rows before koboldai_provider.go was gated, and the
// captured output is recorded in the accompanying task report. That is a real
// RED-on-the-broken-artifact observation, not a synthetic one.

// TestNewProvider_KoboldAI_RefusedAtRemoteEndpoint is the security guard. With
// the gate CLOSED, a KoboldAI provider aimed at a REMOTE endpoint must be
// refused before construction, so no bearer token is ever assembled for a
// public host.
//
// Both endpoints are deliberately non-egressing: `.invalid` is RFC 2606
// reserved and never resolves, and 203.0.113.0/24 is RFC 5737 TEST-NET-3. Both
// are nonetheless classified REMOTE by isLocalEndpointURL through exactly the
// same code paths a real hosted host takes (dotted DNS name; public IP
// literal), so the guard exercises the production predicate without the test
// making a real outbound connection to anyone's infrastructure.
func TestNewProvider_KoboldAI_RefusedAtRemoteEndpoint(t *testing.T) {
	closeCloudGateForTest(t) // cloud_gate_endpoint_locality_test.go

	remotes := []struct {
		name     string
		endpoint string
	}{
		{"public DNS name", "https://kobold.invalid/api"},
		{"public IP literal", "https://203.0.113.10:1"},
	}

	for _, tc := range remotes {
		t.Run(tc.name, func(t *testing.T) {
			prov, err := NewProvider(ProviderConfigEntry{
				Type:     ProviderTypeKoboldAI,
				Endpoint: tc.endpoint,
				APIKey:   "not-a-real-credential-but-treated-as-one",
				Enabled:  true,
				// Bounded so a pre-fix (ungated) run cannot hang on the dial
				// it should never have been allowed to make.
				Parameters: map[string]interface{}{"timeout": float64(2)},
			})
			if err == nil {
				if prov != nil {
					_ = prov.Close()
				}
				t.Fatalf("NewProvider(koboldai, endpoint=%q) CONSTRUCTED with the cloud "+
					"gate closed. KoboldAI attaches `Authorization: Bearer <APIKey>` to "+
					"every outbound request, so this construction ships the supplied "+
					"credential to a public host that the gate exists to keep us away from",
					tc.endpoint)
			}
			if !errors.Is(err, ErrCloudDisabled) {
				t.Fatalf("NewProvider(koboldai, endpoint=%q) refusal error = %v, want it to "+
					"wrap ErrCloudDisabled so callers branch on the SAME sentinel they "+
					"already use for every other gate refusal", tc.endpoint, err)
			}
			// A refused construction must yield nothing usable. This assertion
			// is load-bearing beyond tidiness: the factory arm returns a
			// CONCRETE *KoboldAIProvider, and `return NewKoboldAIProvider(cfg)`
			// from a function declared (Provider, error) would wrap a nil
			// pointer in a NON-NIL interface — a caller's `if prov != nil`
			// would pass and then panic on first use.
			if prov != nil {
				t.Fatalf("NewProvider(koboldai, endpoint=%q) returned a non-nil provider "+
					"alongside the gate refusal", tc.endpoint)
			}
		})
	}
}

// TestNewProvider_KoboldAI_AllowedAtLocalEndpoint is the paired false-refusal
// guard (§11.4.201): the fix must gate on LOCALITY, and gating a local
// KoboldAI would break exactly the local-serving route the gate exists to
// protect. The EMPTY-endpoint row is the one that would be silently missed by
// a naive fix that gates on config.Endpoint: KoboldAI falls back to its own
// default http://localhost:5001 (koboldai_provider.go getAPIURL), so the gate
// must judge the EFFECTIVE base URL the provider will actually dial, not the
// possibly-empty configured one. Gating on the raw empty string would fail
// closed and refuse the single most common local configuration there is.
func TestNewProvider_KoboldAI_AllowedAtLocalEndpoint(t *testing.T) {
	closeCloudGateForTest(t)

	locals := []struct {
		name     string
		endpoint string
	}{
		{"explicit loopback", "http://127.0.0.1:5001"},
		{"explicit localhost name", "http://localhost:5001"},
		// Dialled at LOOPBACK. NewKoboldAIProvider runs discoverModels()
		// synchronously, so a real RFC1918 address here sent traffic to
		// 192.168.1.50:5001 on the operator's LAN — a port something could
		// genuinely answer — and otherwise stalled for the full 2s timeout.
		// The LAN-is-local classification is proven packet-free by the
		// isLocalEndpointURL table in cloud_gate_endpoint_locality_test.go.
		{"LAN private address (dialled at loopback — see note)", "http://127.0.0.1:5001"},
		// Empty → the provider's own http://localhost:5001 default.
		{"empty endpoint falls back to the localhost default", ""},
	}

	for _, tc := range locals {
		t.Run(tc.name, func(t *testing.T) {
			prov, err := NewProvider(ProviderConfigEntry{
				Type:     ProviderTypeKoboldAI,
				Endpoint: tc.endpoint,
				APIKey:   "local-server-key",
				Enabled:  true,
				// Model discovery is best-effort and only logged, so nothing
				// need be listening; the short timeout keeps a refused
				// connection from lingering.
				Parameters: map[string]interface{}{"timeout": float64(2)},
			})
			if err != nil {
				t.Fatalf("local KoboldAI (endpoint %q) refused with the gate closed: %v "+
					"— the gate must never break local serving", tc.endpoint, err)
			}
			if prov == nil {
				t.Fatalf("local KoboldAI (endpoint %q) constructed nil without an error", tc.endpoint)
			}
			_ = prov.Close()
		})
	}
}

// TestNewKoboldAIProvider_DirectConstructionIsGated proves the gate sits in
// the exported CONSTRUCTOR and not only in the factory arm, so the four
// existing direct callers of NewKoboldAIProvider in this package — and any
// future production caller — cannot route around it. Without this, "the
// factory is gated" would be a claim about one call site rather than about the
// provider.
func TestNewKoboldAIProvider_DirectConstructionIsGated(t *testing.T) {
	closeCloudGateForTest(t)

	prov, err := NewKoboldAIProvider(KoboldAIConfig{
		BaseURL: "https://kobold.invalid/api",
		APIKey:  "not-a-real-credential-but-treated-as-one",
	})
	if err == nil {
		if prov != nil {
			_ = prov.Close()
		}
		t.Fatal("NewKoboldAIProvider CONSTRUCTED a remote-endpoint provider with the " +
			"cloud gate closed — gating only the factory arm leaves the exported " +
			"constructor as an open back door around it")
	}
	if !errors.Is(err, ErrCloudDisabled) {
		t.Fatalf("NewKoboldAIProvider refusal error = %v, want it to wrap ErrCloudDisabled", err)
	}
	if prov != nil {
		t.Fatal("NewKoboldAIProvider returned a non-nil provider alongside the refusal")
	}
}

// TestNewProvider_KoboldAI_RemoteAllowedWhenGateOpen keeps the refusals above
// honest (§11.4.201 false-refusal / positive-control): it proves the remote
// rows are refused BY THE GATE and not by some unrelated failure that would
// reject those endpoints in any state. With the gate OPEN the operator has
// explicitly accepted cloud egress, and the same call must not be gate-refused.
func TestNewProvider_KoboldAI_RemoteAllowedWhenGateOpen(t *testing.T) {
	openCloudGateForTest(t) // provider_factory_test.go

	prov, err := NewProvider(ProviderConfigEntry{
		Type:       ProviderTypeKoboldAI,
		Endpoint:   "https://kobold.invalid/api",
		APIKey:     "not-a-real-credential-but-treated-as-one",
		Enabled:    true,
		Parameters: map[string]interface{}{"timeout": float64(2)},
	})
	if err != nil && errors.Is(err, ErrCloudDisabled) {
		t.Fatalf("gate refused KoboldAI although llm.cloud.enabled is true: %v", err)
	}
	if err != nil {
		t.Fatalf("NewProvider(koboldai) failed for a NON-gate reason with the gate open: "+
			"%v — if this were the only way the closed-gate assertions could fail, they "+
			"would prove nothing about the gate", err)
	}
	if prov == nil {
		t.Fatal("NewProvider(koboldai) constructed nil without an error with the gate open")
	}
	_ = prov.Close()
}

// TestNewProvider_ExemptOpenAICompatibleRefusedAtHostedEndpoint makes the
// "SELF-GATING ONE LEVEL DOWN" leg of isCloudGateExemptProviderType DIRECTLY
// falsifiable (Finding 3). Before this test, that leg's safety was asserted
// only in a doc comment and verified only transitively through
// NewOpenAICompatibleProvider's own tests: nothing proved that an exempt type
// routed through NewProvider actually reaches the downstream locality gate.
// A refactor that dropped the downstream check, or an arm rewired to a
// constructor that never had one, would have left the exemption list silently
// waving hosted providers through with no test failing.
//
// ROUND-7 A-6 — EVERY self-gating exempt type is driven, not a sample.
//
// The guard previously drove TWO of the twelve types in the "self-gating one
// level down" case of isCloudGateExemptProviderType. That is the same
// structural bluff as the round-7 A1 half-assertion: a guard whose scope is a
// hardcoded SUBSET of the real list looks green while the untested members
// carry the risk. Xiaomi is the concrete instance — it is on the exempt list
// and its default endpoint is HOSTED (https://api.xiaomimimo.com/v1), so it is
// exactly the arm where a missing downstream gate would matter most, and
// nothing drove it.
//
// selfGatingExemptTypesFromSource parses the list out of factory.go, so the
// set below cannot drift from the set the production switch actually exempts:
// adding a type to the switch without adding coverage now FAILS.
func TestNewProvider_ExemptOpenAICompatibleRefusedAtHostedEndpoint(t *testing.T) {
	closeCloudGateForTest(t)

	exempt := selfGatingExemptProviderTypes(t)
	if len(exempt) < 2 {
		t.Fatalf("premise broken: parsed %d self-gating exempt types out of factory.go; the "+
			"switch was renamed or restructured and this guard has stopped checking anything",
			len(exempt))
	}
	t.Logf("driving %d self-gating exempt types parsed from factory.go: %v", len(exempt), exempt)

	for _, pt := range exempt {
		t.Run(string(pt), func(t *testing.T) {
			prov, err := NewProvider(ProviderConfigEntry{
				Type:     pt,
				Endpoint: "https://api.example.com/v1",
				APIKey:   "not-a-real-credential-but-treated-as-one",
				Enabled:  true,
			})
			if err == nil {
				if prov != nil {
					_ = prov.Close()
				}
				t.Fatalf("%s CONSTRUCTED at a HOSTED endpoint with the gate closed. This "+
					"type is exempt from the identity check in NewProvider ONLY because it "+
					"is supposed to self-gate on endpoint locality one level down — that "+
					"downstream gate is now missing or bypassed, so the exemption list is "+
					"waving a hosted provider straight through", pt)
			}
			if !errors.Is(err, ErrCloudDisabled) {
				t.Fatalf("%s refusal error = %v, want it to wrap ErrCloudDisabled", pt, err)
			}
			if prov != nil {
				t.Fatalf("%s returned a non-nil provider alongside the gate refusal", pt)
			}
		})
	}
}

// selfGatingExemptProviderTypes returns the ProviderType constants listed in
// EVERY case clause of isCloudGateExemptProviderType AFTER the first — the
// "self-gating one level down" group, i.e. the types whose safety depends on a
// downstream endpoint-locality check rather than on being local by identity.
//
// The FIRST clause (Ollama, llama.cpp) is deliberately excluded: those are
// exempt because they are local BY IDENTITY, so they are expected to construct
// at any endpoint and driving them here would assert the opposite of their
// contract.
//
// This parses the production source rather than restating it, because a
// restated list is exactly what let eleven of twelve types go undriven.
//
// ROUND-8 N-2 — this used to read clauses[1] and fatal only when there were
// FEWER than two clauses. A THIRD case clause, added for a new self-gating
// family, would have been silently undriven while the guard stayed green: the
// exact shape of the defect it exists to prevent, one level up. It now takes
// clauses[1:], so every clause after the identity-exempt one is driven.
//
// The switch-shape assertion below closes the other half of the same hole. A
// contributor who writes the new exemption as an `if` BEFORE the switch —
// `if t == ProviderTypeNew { return true }` — adds no case clause at all, so a
// clause-counting guard cannot see it. Pinning the body to exactly one switch
// statement (and nothing else that can return true) turns that into a FAILURE
// that names its own remedy rather than a silent gap.
func selfGatingExemptProviderTypes(t *testing.T) []ProviderType {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "factory.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing factory.go: %v", err)
	}
	var clauses []*ast.CaseClause
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "isCloudGateExemptProviderType" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if cc, ok := n.(*ast.CaseClause); ok && len(cc.List) > 0 {
				clauses = append(clauses, cc)
			}
			return true
		})
	}
	if len(clauses) < 2 {
		t.Fatalf("expected at least two non-default case clauses in "+
			"isCloudGateExemptProviderType, found %d", len(clauses))
	}

	// The function body must be exactly ONE switch statement, so that reading
	// its case clauses is the same thing as reading the exemption rule. An
	// `if` guard added before or after the switch would exempt a type this
	// parse cannot see.
	assertBodyIsASingleSwitch(t, "isCloudGateExemptProviderType", file)

	var out []ProviderType
	for i, clause := range clauses[1:] {
		for _, expr := range clause.List {
			ident, ok := expr.(*ast.Ident)
			if !ok {
				t.Fatalf("unexpected non-identifier %T in self-gating exempt clause %d", expr, i+1)
			}
			pt, ok := providerTypeByConstName[ident.Name]
			if !ok {
				t.Fatalf("factory.go exempts %s but this test cannot map that constant to a "+
					"ProviderType value. Add it to providerTypeByConstName — an unmapped constant "+
					"is an exempt type going undriven, which is the defect this guard exists for.",
					ident.Name)
			}
			out = append(out, pt)
		}
	}
	return out
}

// assertBodyIsASingleSwitch pins the named function's body to exactly one
// statement, and that statement to a switch.
//
// Why this shape and not something cleverer: the guard above learns the
// exemption rule by reading case clauses, so anything that can return true
// WITHOUT being a case clause is invisible to it. Rather than try to
// enumerate every such construct, this asserts the only shape in which the
// clause-reading is complete. A contributor who genuinely needs another shape
// gets a failure that says so, instead of an exemption nobody drives.
func assertBodyIsASingleSwitch(t *testing.T, funcName string, file *ast.File) {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName || fn.Body == nil {
			continue
		}
		if len(fn.Body.List) != 1 {
			t.Fatalf("%s's body has %d top-level statements; this guard reads the exemption "+
				"rule out of the case clauses of a single switch, so any other statement "+
				"(an early `if t == ProviderTypeNew { return true }`, say) exempts a type "+
				"the guard cannot see and therefore never drives. Fold the new exemption "+
				"into the switch, or teach this guard the new shape deliberately.",
				funcName, len(fn.Body.List))
		}
		if _, ok := fn.Body.List[0].(*ast.SwitchStmt); !ok {
			t.Fatalf("%s's single body statement is %T, not a switch; see the note above",
				funcName, fn.Body.List[0])
		}
		return
	}
	t.Fatalf("%s not found while asserting its shape — it was renamed or removed, and this "+
		"guard has stopped checking anything", funcName)
}

// providerTypeByConstName maps the SOURCE IDENTIFIER of each self-gating
// exempt constant to its value. Go offers no reflection from a constant's name
// to its value, so this mapping is explicit — and the parser above FAILS on
// any identifier missing from it, so it cannot silently fall behind.
var providerTypeByConstName = map[string]ProviderType{
	"ProviderTypeKoboldAI":  ProviderTypeKoboldAI,
	"ProviderTypeVLLM":      ProviderTypeVLLM,
	"ProviderTypeLocalAI":   ProviderTypeLocalAI,
	"ProviderTypeFastChat":  ProviderTypeFastChat,
	"ProviderTypeTextGen":   ProviderTypeTextGen,
	"ProviderTypeLMStudio":  ProviderTypeLMStudio,
	"ProviderTypeJan":       ProviderTypeJan,
	"ProviderTypeGPT4All":   ProviderTypeGPT4All,
	"ProviderTypeTabbyAPI":  ProviderTypeTabbyAPI,
	"ProviderTypeMLX":       ProviderTypeMLX,
	"ProviderTypeMistralRS": ProviderTypeMistralRS,
	"ProviderTypeXiaomi":    ProviderTypeXiaomi,
}
