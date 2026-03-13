package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestSyncDefaultsCommandDryRun(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace")
	legacyWorkspace := filepath.Join(root, "workspace-legacy")
	configPath := filepath.Join(root, "config.json")

	writeCommandWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents\n")
	writeCommandWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul\n")
	writeCommandWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity\n")
	writeCommandWorkspaceFile(t, legacyWorkspace, "AGENTS.md", "# legacy agents\n")

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = defaultWorkspace
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	t.Setenv("PICOCLAW_CONFIG", configPath)

	cmd := newSyncDefaultsCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute dry-run: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("conflicts: AGENTS.md")) {
		t.Fatalf("expected conflict output, got:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("Dry run 1 workspace(s)")) {
		t.Fatalf("expected dry-run summary, got:\n%s", out.String())
	}
	assertCommandFileContent(t, filepath.Join(legacyWorkspace, "AGENTS.md"), "# legacy agents\n")
}

func TestSyncDefaultsCommandForceLegacy(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace")
	legacyWorkspace := filepath.Join(root, "workspace-legacy")
	configPath := filepath.Join(root, "config.json")

	writeCommandWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents\n")
	writeCommandWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul\n")
	writeCommandWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity\n")
	writeCommandWorkspaceFile(t, legacyWorkspace, "AGENTS.md", "# legacy agents\n")

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = defaultWorkspace
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	t.Setenv("PICOCLAW_CONFIG", configPath)

	cmd := newSyncDefaultsCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--force-legacy"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute force-legacy: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("updated: AGENTS.md")) {
		t.Fatalf("expected update output, got:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("Applied 1 workspace(s)")) {
		t.Fatalf("expected apply summary, got:\n%s", out.String())
	}
	assertCommandFileContent(t, filepath.Join(legacyWorkspace, "AGENTS.md"), "# source agents\n")
}

func writeCommandWorkspaceFile(t *testing.T, workspace, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(workspace, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func assertCommandFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("content of %s = %q, want %q", path, string(data), want)
	}
}
