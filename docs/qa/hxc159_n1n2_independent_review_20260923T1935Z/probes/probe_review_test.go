package agent

// Independent reviewer probe (HXC-159 N1/N2 review of 97e8c852). NOT committed
// to the tree; injected via `go test -overlay` into BOTH the parent and the
// fixed tree so the same source measures both.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"dev.helix.code/internal/commands"
)

func rvWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rvNames(reg *commands.SkillRegistry) []string {
	var out []string
	for _, s := range reg.List() {
		out = append(out, s.Name()+"<"+s.SourcePath()+">")
	}
	sort.Strings(out)
	return out
}

// A: N1 — independently-authored flat-file fixture, 3 packaged + 31 flat.
func TestRV_N1_FlatFiles(t *testing.T) {
	dir := t.TempDir()
	descs := []string{"orbital mechanics trajectory planner", "sourdough fermentation schedule helper", "kernel module signing checklist"}
	for i, d := range descs {
		rvWrite(t, filepath.Join(dir, fmt.Sprintf("pk-%d", i), "SKILL.md"), "---\ndescription: "+d+"\n---\nbody pk\n")
	}
	for i := 0; i < 31; i++ {
		rvWrite(t, filepath.Join(dir, fmt.Sprintf("rogue_%c%02d.md", 'a'+i%26, i)), "---\ndescription: rogue flat\ntriggers:\n  - \"(?i)rogue\"\n---\nROGUE\n")
	}
	// flat file sharing a packaged skill's name
	rvWrite(t, filepath.Join(dir, "pk-0.md"), "---\ndescription: shadow flat\n---\nSHADOW\n")

	reg := commands.NewSkillRegistry()
	err := LoadExternalTierIntoRegistry(reg, dir)
	flat := 0
	for _, s := range reg.List() {
		if strings.HasPrefix(s.Name(), "rogue_") {
			flat++
		}
	}
	pk0, _ := reg.Get("pk-0")
	pk0body := ""
	if pk0 != nil {
		pk0body = pk0.Body()
	}
	t.Logf("RV-N1 direct: err=%v total=%d flatRegistered=%d pk0Body=%q", err, len(reg.List()), flat, pk0body)

	t.Setenv(externalSkillsDirEnvVar, dir)
	reg2, disp, err2 := LoadSkillsAndDispatcher([]string{dir})
	flat2 := 0
	for _, s := range reg2.List() {
		if strings.HasPrefix(s.Name(), "rogue_") {
			flat2++
		}
	}
	out, matched := DispatchSkill(disp, "please rogue now")
	// control needle: prove the fixture really is a set of files commands.SkillLoader recognises
	ctl := commands.NewSkillRegistry()
	_ = commands.NewSkillLoader(ctl, dir, "").Load()
	cflat := 0
	for _, s := range ctl.List() {
		if strings.HasPrefix(s.Name(), "rogue_") {
			cflat++
		}
	}
	t.Logf("RV-N1 CONTROL (plain commands.SkillLoader over same dir): flatRecognised=%d", cflat)
	reg3, _, _ := LoadSkillsAndDispatcher([]string{dir + "/"})
	f3 := 0
	for _, s := range reg3.List() {
		if strings.HasPrefix(s.Name(), "rogue_") {
			f3++
		}
	}
	t.Logf("RV-N1 spelling-variant (dir+\"/\", pre-existing class): flatRegistered=%d", f3)
	t.Logf("RV-N1 via LoadSkillsAndDispatcher: err=%v total=%d flatRegistered=%d rogueTriggerMatched=%v out=%q", err2, len(reg2.List()), flat2, matched, out)
}

// B: N2 — independently-authored deep-nesting fixture.
func TestRV_N2_Nested(t *testing.T) {
	dir := t.TempDir()
	const d = "harmonize invoice currency conversion rounding rules"
	rvWrite(t, filepath.Join(dir, "mmm-top", "SKILL.md"), "---\ndescription: "+d+"\n---\nTOP BODY\n")
	rvWrite(t, filepath.Join(dir, "lib", "x", "aaa-deep", "SKILL.md"), "---\ndescription: "+d+"\ntriggers:\n  - (?i)^deepgo$\n---\nDEEP BODY {{ARG1}}\n")
	rvWrite(t, filepath.Join(dir, "zzz-other", "SKILL.md"), "---\ndescription: unrelated weather radar calibration\n---\nOTHER\n")

	reg := commands.NewSkillRegistry()
	err := LoadExternalTierIntoRegistry(reg, dir)
	_, top := reg.Get("mmm-top")
	deep, deepOK := reg.Get("aaa-deep")
	body := ""
	if deepOK {
		body = deep.Body() + " src=" + deep.SourcePath()
	}
	_, other := reg.Get("zzz-other")
	t.Logf("RV-N2 direct: err=%v top(loser)=%v deep(winner)=%v deepBody=%q other=%v all=%v", err, top, deepOK, body, other, rvNames(reg))

	t.Setenv(externalSkillsDirEnvVar, dir)
	reg2, disp, err2 := LoadSkillsAndDispatcher([]string{dir})
	_, top2 := reg2.Get("mmm-top")
	_, deep2 := reg2.Get("aaa-deep")
	out, matched := DispatchSkill(disp, "deepgo")
	t.Logf("RV-N2 via LSAD: err=%v top=%v deep=%v deepTriggerMatched=%v out=%q", err2, top2, deep2, matched, out)
}

// C: fidelity — every external-tier skill as registered vs the yaml.v3
// parse of the SAME manifest file (what a disk-loaded skill would carry).
func TestRV_Fidelity_RealCorpus(t *testing.T) {
	dir := os.Getenv("RV_REAL_EXT")
	if dir == "" {
		t.Skip("RV_REAL_EXT unset")
	}
	reg := commands.NewSkillRegistry()
	if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, s := range reg.List() {
		raw, err := os.ReadFile(filepath.Join(dir, s.Name(), "SKILL.md"))
		if err != nil {
			t.Logf("RV-FID %s: cannot read manifest (%v) src=%s", s.Name(), err, s.SourcePath())
			continue
		}
		ref, err := commands.ParseSkillForTest(s.Name(), string(raw), "ref")
		if err != nil {
			t.Logf("RV-FID %s: ref parse err %v", s.Name(), err)
			continue
		}
		t.Logf("RV-FID %s src=%s descEq=%v bodyEq=%v isoEq=%v triggersEq=%v",
			s.Name(), s.SourcePath(), s.Description() == ref.Description(), s.Body() == ref.Body(),
			s.RequiresIsolation() == ref.RequiresIsolation(), reflect.DeepEqual(s.TriggerPatterns(), ref.TriggerPatterns()))
		if !reflect.DeepEqual(s.TriggerPatterns(), ref.TriggerPatterns()) {
			t.Logf("   got  triggers=%q", s.TriggerPatterns())
			t.Logf("   want triggers=%q", ref.TriggerPatterns())
		}
		if s.Description() != ref.Description() {
			t.Logf("   got  desc=%q", s.Description())
			t.Logf("   want desc=%q", ref.Description())
		}
	}
}

// D: end-to-end trigger dispatch through LoadSkillsAndDispatcher with the
// real external tier in the production AllSkillsDirs position.
func TestRV_Dispatch_RealCorpus(t *testing.T) {
	dir := os.Getenv("RV_REAL_EXT")
	if dir == "" {
		t.Skip("RV_REAL_EXT unset")
	}
	t.Setenv(externalSkillsDirEnvVar, dir)
	empty1, empty2, proj := t.TempDir(), t.TempDir(), t.TempDir()
	reg, disp, err := LoadSkillsAndDispatcher([]string{empty1, empty2, dir, proj})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"action-prefix-system", "media-validator", "multitrack", "skill-catalog", "conventional-commit"} {
		if s, ok := reg.Get(n); ok {
			t.Logf("RV-DISP %s src=%s triggers=%q", n, s.SourcePath(), s.TriggerPatterns())
		}
	}
	for _, p := range []string{"how do i add a new directive", "what does the BACKGROUND prefix do"} {
		_, matched := DispatchSkill(disp, p)
		t.Logf("RV-DISP prompt=%q matched=%v", p, matched)
	}
}

// E: builtins present when external is the ONLY dir; F: earlier override
// survives later unrelated dir; G: root-level SKILL.md in external dir.
func TestRV_Builtins(t *testing.T) {
	ext := t.TempDir()
	rvWrite(t, filepath.Join(ext, "solo", "SKILL.md"), "---\ndescription: solitary lighthouse keeper log\n---\nSOLO\n")
	t.Setenv(externalSkillsDirEnvVar, ext)
	reg, _, err := LoadSkillsAndDispatcher([]string{ext})
	nb := 0
	for _, s := range reg.List() {
		if strings.HasPrefix(s.SourcePath(), "builtin:") {
			nb++
		}
	}
	_, solo := reg.Get("solo")
	t.Logf("RV-E externalOnly: err=%v builtins=%d solo=%v", err, nb, solo)

	d1, d2, d3 := t.TempDir(), t.TempDir(), t.TempDir()
	rvWrite(t, filepath.Join(d1, "media-validator", "SKILL.md"), "---\ndescription: mine\n---\nMY OVERRIDE\n")
	rvWrite(t, filepath.Join(d2, "unrelated.md"), "---\ndescription: u\n---\nU\n")
	rvWrite(t, filepath.Join(d3, "multitrack.md"), "---\ndescription: flat override\n---\nFLAT MT\n")
	reg2, _, err2 := LoadSkillsAndDispatcher([]string{d1, d2, ext, d3})
	mv, _ := reg2.Get("media-validator")
	mt, _ := reg2.Get("multitrack")
	nb2 := 0
	for _, s := range reg2.List() {
		if strings.HasPrefix(s.SourcePath(), "builtin:") {
			nb2++
		}
	}
	t.Logf("RV-F err=%v media-validator=%q multitrack=%q builtinsRemaining=%d", err2, mv.Body(), mt.Body(), nb2)

	root := t.TempDir()
	rvWrite(t, filepath.Join(root, "SKILL.md"), "---\ndescription: root level manifest oddity\n---\nROOTBODY\n")
	rvWrite(t, filepath.Join(root, "child", "SKILL.md"), "---\ndescription: child skill in tier\n---\nCHILD\n")
	reg3 := commands.NewSkillRegistry()
	err3 := LoadExternalTierIntoRegistry(reg3, root)
	t.Logf("RV-G rootManifest: err=%v registered=%v tierBase=%s", err3, rvNames(reg3), filepath.Base(root))
}
