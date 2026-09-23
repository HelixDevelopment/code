package skillparity

import (
	"testing"
	"time"

	"dev.helix.code/internal/skillconformance"
)

func manifest(consumer string, surface skillconformance.LoaderAPISurface, skills []skillconformance.SkillReport, optional []skillconformance.OptionalCapability) skillconformance.CapabilityManifest {
	return skillconformance.CapabilityManifest{
		SchemaVersion:        skillconformance.SchemaVersion,
		Consumer:             consumer,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		LoaderAPISurface:     surface,
		Skills:               skills,
		OptionalCapabilities: optional,
	}
}

// TestDiff_IdenticalReportsOK is the golden-good case: two reports with
// the same surface and the same skill set produce an empty, OK diff.
func TestDiff_IdenticalReportsOK(t *testing.T) {
	surface := skillconformance.LoaderAPISurface{SourceRegistration: true}
	skills := []skillconformance.SkillReport{{Qualified: "fixture.a", Source: "fixture", Trust: "first-party"}}
	a := manifest("helix_code", surface, skills, nil)
	b := manifest("helix_agent", surface, skills, nil)

	d := Diff(a, b)
	if !d.OK() {
		t.Fatalf("identical reports must diff OK, got: %+v", d)
	}
}

// TestDiff_UnmarkedSurfaceAsymmetryFails: T-P7.02.2's paired-mutation
// contract, surface half. A real, measured asymmetry (helix_code lacks
// capability_declaration; helix_agent has it) with NO matching
// optional_capabilities entry on either side must FAIL.
func TestDiff_UnmarkedSurfaceAsymmetryFails(t *testing.T) {
	a := manifest("helix_code", skillconformance.LoaderAPISurface{CapabilityDeclaration: false}, nil, nil)
	b := manifest("helix_agent", skillconformance.LoaderAPISurface{CapabilityDeclaration: true}, nil, nil)

	d := Diff(a, b)
	if d.OK() {
		t.Fatal("an unmarked loader_api_surface asymmetry must fail the diff")
	}
	if len(d.SurfaceAsymmetries) != 1 || d.SurfaceAsymmetries[0].Key != "capability_declaration" {
		t.Fatalf("expected exactly one capability_declaration surface asymmetry, got: %+v", d.SurfaceAsymmetries)
	}
}

// TestDiff_MarkedSurfaceAsymmetryPasses: the SAME asymmetry as above, but
// BOTH sides now carry a matching, reasoned optional_capabilities entry
// (T-P7.01.3) — the diff must pass.
func TestDiff_MarkedSurfaceAsymmetryPasses(t *testing.T) {
	opt := []skillconformance.OptionalCapability{{Key: "capability_declaration", Reason: "not yet wired at the helix_code consumer seam this cycle; tracked follow-up"}}
	a := manifest("helix_code", skillconformance.LoaderAPISurface{CapabilityDeclaration: false}, nil, opt)
	b := manifest("helix_agent", skillconformance.LoaderAPISurface{CapabilityDeclaration: true}, nil, opt)

	d := Diff(a, b)
	if !d.OK() {
		t.Fatalf("a mutually-marked asymmetry must pass, got: %+v", d)
	}
}

// TestDiff_SkillRegisteredInOnlyOneConsumerFails is T-P7.02.2's literal
// scenario: "register a skill in one consumer only -> the gate MUST
// FAIL." No optional_capabilities entry covers it.
func TestDiff_SkillRegisteredInOnlyOneConsumerFails(t *testing.T) {
	surface := skillconformance.LoaderAPISurface{}
	a := manifest("helix_code", surface, []skillconformance.SkillReport{
		{Qualified: "fixture.only-in-a", Source: "fixture", Trust: "first-party"},
	}, nil)
	b := manifest("helix_agent", surface, nil, nil)

	d := Diff(a, b)
	if d.OK() {
		t.Fatal("a skill registered in only one consumer must fail the diff")
	}
	if len(d.SkillAsymmetries) != 1 || d.SkillAsymmetries[0].Qualified != "fixture.only-in-a" {
		t.Fatalf("expected exactly one skill asymmetry for fixture.only-in-a, got: %+v", d.SkillAsymmetries)
	}
}

// TestDiff_MarkedSkillAsymmetryPasses: same scenario, but the skill's
// qualified name is explicitly marked optional on both sides.
func TestDiff_MarkedSkillAsymmetryPasses(t *testing.T) {
	surface := skillconformance.LoaderAPISurface{}
	opt := []skillconformance.OptionalCapability{{Key: "fixture.only-in-a", Reason: "consumer-optional fixture skill, intentionally not mirrored"}}
	a := manifest("helix_code", surface, []skillconformance.SkillReport{
		{Qualified: "fixture.only-in-a", Source: "fixture", Trust: "first-party"},
	}, opt)
	b := manifest("helix_agent", surface, nil, opt)

	d := Diff(a, b)
	if !d.OK() {
		t.Fatalf("a mutually-marked skill asymmetry must pass, got: %+v", d)
	}
}

// TestDiff_OneSidedMarkIsNotEnough: an optional_capabilities entry on
// ONLY ONE side is not a mutual explanation — both reports must agree
// the asymmetry is expected, or the diff still fails. This guards
// against a consumer silently declaring its own gap "optional" without
// the other side's agreement.
func TestDiff_OneSidedMarkIsNotEnough(t *testing.T) {
	surface := skillconformance.LoaderAPISurface{}
	optA := []skillconformance.OptionalCapability{{Key: "fixture.only-in-a", Reason: "a says this is fine"}}
	a := manifest("helix_code", surface, []skillconformance.SkillReport{
		{Qualified: "fixture.only-in-a", Source: "fixture", Trust: "first-party"},
	}, optA)
	b := manifest("helix_agent", surface, nil, nil) // no matching mark on b's side

	d := Diff(a, b)
	if d.OK() {
		t.Fatal("a one-sided optional mark must not be enough to pass the diff")
	}
}
