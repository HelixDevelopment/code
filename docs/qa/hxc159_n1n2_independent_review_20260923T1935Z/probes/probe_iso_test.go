package agent

import (
	"os"
	"path/filepath"
	"testing"

	"dev.helix.code/internal/commands"
)

func TestRV_IsolationFidelity(t *testing.T) {
	for _, v := range []string{"true", "True", "TRUE", "\"true\"", "true # sandbox me"} {
		dir := t.TempDir()
		raw := "---\ndescription: isolation fidelity probe\nrequires_isolation: " + v + "\n---\nBODY\n"
		_ = os.MkdirAll(filepath.Join(dir, "iso"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "iso", "SKILL.md"), []byte(raw), 0o644)
		reg := commands.NewSkillRegistry()
		err := LoadExternalTierIntoRegistry(reg, dir)
		s, ok := reg.Get("iso")
		ref, rerr := commands.ParseSkillForTest("iso", raw, "ref")
		got := ok && s.RequiresIsolation()
		t.Logf("RV-ISO value=%-20q registered=%v err=%v gotIsolation=%v yamlv3Isolation=%v(refErr=%v)", v, ok, err, got, rerr == nil && ref.RequiresIsolation(), rerr)
	}
}
