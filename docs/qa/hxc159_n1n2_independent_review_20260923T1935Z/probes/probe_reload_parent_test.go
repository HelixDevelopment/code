package agent

import (
	"os"
	"path/filepath"
	"testing"

	"dev.helix.code/internal/commands"
)

// Mirrors cmd/cli/main.go buildSubsystems / `skills` dispatcher sequence at
// THIS tree's revision, then exercises the hot-reload (SkillsWatcher /
// `skills reload`) path on the retained project loader.
func TestRV_HotReload_ProjectOverrideRemoved(t *testing.T) {
	legacy, proj := t.TempDir(), t.TempDir()
	reg := commands.NewSkillRegistry()
	_ = LoadExternalTierIntoRegistry(reg, t.TempDir())
	_ = commands.NewSkillLoader(reg, legacy, "").Load()
	p := filepath.Join(proj, "conventional-commit.md")
	if err := os.WriteFile(p, []byte("---\ndescription: mine\n---\nPROJECT OVERRIDE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loader := commands.NewSkillLoader(reg, proj, "")
	RV_SKIP(loader)
	_ = loader.Load()
	s, _ := reg.Get("conventional-commit")
	t.Logf("RV-RELOAD before delete: body=%q loaderLoaded=%d", s.Body(), len(loader.Loaded()))
	_ = os.Remove(p)
	_ = loader.Reload()
	s2, ok := reg.Get("conventional-commit")
	src := ""
	if ok {
		src = s2.SourcePath()
	}
	t.Logf("RV-RELOAD after delete+Reload: present=%v src=%q loaderLoaded=%d", ok, src, len(loader.Loaded()))
}
func RV_SKIP(l *commands.SkillLoader) {}
