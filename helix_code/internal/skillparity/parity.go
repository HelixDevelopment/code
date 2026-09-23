// Package skillparity implements the HXC-159 T-P7.02 cross-consumer
// parity gate (gate CM-CROSS-CONSUMER-PARITY, R-07): it diffs the two
// capability manifests HelixCode and HelixAgent each publish
// (T-P7.01) and fails on any asymmetry neither side's manifest
// explicitly marks as consumer-optional (T-P7.01.3).
package skillparity

import (
	"sort"

	"dev.helix.code/internal/skillconformance"
)

// SurfaceAsymmetry is one loader_api_surface flag disagreeing between the
// two reports.
type SurfaceAsymmetry struct {
	Key      string `json:"key"`
	AValue   bool   `json:"a_value"`
	BValue   bool   `json:"b_value"`
	Consumer string `json:"-"` // unused placeholder kept out of JSON; A/B carry the values directly
}

// SkillAsymmetry is one skill present in exactly one report's skills[].
type SkillAsymmetry struct {
	Qualified string `json:"qualified_name"`
	PresentIn string `json:"present_in"` // the Consumer field of the report that HAS it
	MissingIn string `json:"missing_in"` // the Consumer field of the report that LACKS it
}

// Result is the full diff outcome.
type Result struct {
	AConsumer          string             `json:"a_consumer"`
	BConsumer          string             `json:"b_consumer"`
	SurfaceAsymmetries []SurfaceAsymmetry `json:"surface_asymmetries"`
	SkillAsymmetries   []SkillAsymmetry   `json:"skill_asymmetries"`
}

// OK reports whether the diff is empty — i.e. every asymmetry found was
// explicitly, mutually marked optional, or there were none at all.
func (r Result) OK() bool {
	return len(r.SurfaceAsymmetries) == 0 && len(r.SkillAsymmetries) == 0
}

// isMutuallyMarked reports whether key appears in BOTH manifests'
// OptionalCapabilities with a non-empty reason. A one-sided mark is
// deliberately NOT sufficient (TestDiff_OneSidedMarkIsNotEnough):
// exempting a real capability gap must be an agreement both consumers'
// reports carry, never one consumer unilaterally excusing itself.
func isMutuallyMarked(key string, a, b skillconformance.CapabilityManifest) bool {
	return markedIn(key, a) && markedIn(key, b)
}

func markedIn(key string, m skillconformance.CapabilityManifest) bool {
	for _, oc := range m.OptionalCapabilities {
		if oc.Key == key && oc.Reason != "" {
			return true
		}
	}
	return false
}

func surfaceFlags(s skillconformance.LoaderAPISurface) map[string]bool {
	return map[string]bool{
		"source_registration":        s.SourceRegistration,
		"namespaced_identity":        s.NamespacedIdentity,
		"capability_declaration":     s.CapabilityDeclaration,
		"capability_enforcement":     s.CapabilityEnforcement,
		"allowlist_activation":       s.AllowlistActivation,
		"audit_log":                  s.AuditLog,
		"content_hash_provenance":    s.ContentHashProvenance,
		"malformed_skill_visibility": s.MalformedSkillVisibility,
	}
}

// Diff compares a and b's loader_api_surface and skills[] and returns
// every asymmetry NOT mutually marked optional by both reports.
// Deterministic output order (§11.4.50): both slices are sorted by key.
func Diff(a, b skillconformance.CapabilityManifest) Result {
	res := Result{AConsumer: a.Consumer, BConsumer: b.Consumer}

	af, bf := surfaceFlags(a.LoaderAPISurface), surfaceFlags(b.LoaderAPISurface)
	var surfaceKeys []string
	for k := range af {
		surfaceKeys = append(surfaceKeys, k)
	}
	sort.Strings(surfaceKeys)
	for _, k := range surfaceKeys {
		if af[k] != bf[k] && !isMutuallyMarked(k, a, b) {
			res.SurfaceAsymmetries = append(res.SurfaceAsymmetries, SurfaceAsymmetry{
				Key: k, AValue: af[k], BValue: bf[k],
			})
		}
	}

	inA := map[string]bool{}
	for _, s := range a.Skills {
		inA[s.Qualified] = true
	}
	inB := map[string]bool{}
	for _, s := range b.Skills {
		inB[s.Qualified] = true
	}
	var names []string
	seen := map[string]bool{}
	for n := range inA {
		if !seen[n] {
			names = append(names, n)
			seen[n] = true
		}
	}
	for n := range inB {
		if !seen[n] {
			names = append(names, n)
			seen[n] = true
		}
	}
	sort.Strings(names)
	for _, n := range names {
		hasA, hasB := inA[n], inB[n]
		if hasA == hasB {
			continue
		}
		if isMutuallyMarked(n, a, b) {
			continue
		}
		asym := SkillAsymmetry{Qualified: n}
		if hasA {
			asym.PresentIn, asym.MissingIn = a.Consumer, b.Consumer
		} else {
			asym.PresentIn, asym.MissingIn = b.Consumer, a.Consumer
		}
		res.SkillAsymmetries = append(res.SkillAsymmetries, asym)
	}

	return res
}
