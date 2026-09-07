package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_LoadFromYAML(t *testing.T) {
	yaml := []byte(`
servers:
  - name: brave
    transport: stdio
    command: ["npx", "@modelcontextprotocol/server-brave-search"]
    env:
      BRAVE_API_KEY: ${BRAVE_API_KEY}
    alwaysLoad: true
  - name: cloudflare
    transport: sse
    url: https://example.com/post
    sseURL: https://example.com/sse
    oauth:
      enabled: true
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yml"), yaml, 0644))
	t.Setenv("BRAVE_API_KEY", "k-1")
	cfg, err := LoadConfig(filepath.Join(dir, "mcp.yml"))
	require.NoError(t, err)
	require.Len(t, cfg.Servers, 2)
	assert.Equal(t, "brave", cfg.Servers[0].Name)
	assert.Equal(t, TransportStdio, cfg.Servers[0].Transport)
	assert.Equal(t, "k-1", cfg.Servers[0].Env["BRAVE_API_KEY"])
	assert.True(t, cfg.Servers[0].AlwaysLoad)
	assert.Equal(t, TransportSSE, cfg.Servers[1].Transport)
	assert.True(t, cfg.Servers[1].OAuth.Enabled)
}

func TestConfig_ProjectOverridesUser(t *testing.T) {
	user := []byte(`
servers:
  - name: brave
    transport: stdio
    command: ["from-user"]
  - name: only-user
    transport: stdio
    command: ["x"]
`)
	project := []byte(`
servers:
  - name: brave
    transport: stdio
    command: ["from-project"]
  - name: only-project
    transport: stdio
    command: ["y"]
`)
	dir := t.TempDir()
	uPath := filepath.Join(dir, "user.yml")
	pPath := filepath.Join(dir, "project.yml")
	require.NoError(t, os.WriteFile(uPath, user, 0644))
	require.NoError(t, os.WriteFile(pPath, project, 0644))

	cfg, err := LoadMerged(uPath, pPath)
	require.NoError(t, err)
	specs := map[string]ServerSpec{}
	for _, s := range cfg.Servers {
		specs[s.Name] = s
	}
	require.Len(t, cfg.Servers, 3)
	assert.Equal(t, []string{"from-project"}, specs["brave"].Command)
	assert.Equal(t, []string{"x"}, specs["only-user"].Command)
	assert.Equal(t, []string{"y"}, specs["only-project"].Command)
}

func TestConfig_ValidateRequiresTransport(t *testing.T) {
	yaml := []byte("servers:\n  - name: x\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.yml")
	require.NoError(t, os.WriteFile(path, yaml, 0644))
	_, err := LoadConfig(path)
	require.Error(t, err)
}

func TestConfig_SaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.yml")
	cfg := &Config{
		Servers: []ServerSpec{
			{Name: "a", Transport: TransportStdio, Command: []string{"echo"}, AlwaysLoad: true},
		},
	}
	require.NoError(t, SaveConfig(path, cfg))
	got, err := LoadConfig(path)
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	assert.Equal(t, "a", got.Servers[0].Name)
	assert.True(t, got.Servers[0].AlwaysLoad)
}

func TestConfig_MissingEnvVarExpandsToEmpty(t *testing.T) {
	yaml := []byte(`
servers:
  - name: a
    transport: stdio
    command: ["echo"]
    env:
      MY_KEY: "before-${P1F06_T10_NOT_SET}-after"
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yml"), yaml, 0644))
	// Save/restore: a bare os.Unsetenv leaks to every later test in the
	// binary and makes verdicts depend on -shuffle ordering.
	if prev, had := os.LookupEnv("P1F06_T10_NOT_SET"); had {
		t.Cleanup(func() { _ = os.Setenv("P1F06_T10_NOT_SET", prev) })
	}
	_ = os.Unsetenv("P1F06_T10_NOT_SET")
	cfg, err := LoadConfig(filepath.Join(dir, "mcp.yml"))
	require.NoError(t, err)
	require.Len(t, cfg.Servers, 1)
	assert.Equal(t, "before--after", cfg.Servers[0].Env["MY_KEY"])
}
