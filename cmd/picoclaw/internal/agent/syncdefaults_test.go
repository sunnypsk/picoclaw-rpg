package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal/onboard"
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
	if !bytes.Contains(out.Bytes(), []byte("Dry run 2 workspace(s)")) {
		t.Fatalf("expected dry-run summary, got:\n%s", out.String())
	}
	assertCommandFileContent(t, filepath.Join(defaultWorkspace, "AGENTS.md"), "# source agents\n")
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

	wantAgents := embeddedCommandWorkspaceFile(t, "AGENTS.md")

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
	if !bytes.Contains(out.Bytes(), []byte("Applied 2 workspace(s)")) {
		t.Fatalf("expected apply summary, got:\n%s", out.String())
	}
	assertCommandFileContent(t, filepath.Join(defaultWorkspace, "AGENTS.md"), wantAgents)
	assertCommandFileContent(t, filepath.Join(legacyWorkspace, "AGENTS.md"), wantAgents)
}

func TestSyncDefaultsCommandUsesDefaultWorkspaceAsSourceOfTruthAfterRefresh(t *testing.T) {
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
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--force-legacy"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("initial Execute force-legacy: %v", err)
	}

	writeCommandWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# custom default agents\n")

	cmd = newSyncDefaultsCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--force-legacy"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second Execute force-legacy: %v", err)
	}

	assertCommandFileContent(t, filepath.Join(defaultWorkspace, "AGENTS.md"), "# custom default agents\n")
	assertCommandFileContent(t, filepath.Join(legacyWorkspace, "AGENTS.md"), "# custom default agents\n")
}

func TestSyncDefaultsCommandCopiesManagedSkillIntoExistingWorkspace(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace")
	legacyWorkspace := filepath.Join(root, "workspace-legacy")
	configPath := filepath.Join(root, "config.json")

	writeCommandWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents\n")
	writeCommandWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul\n")
	writeCommandWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity\n")
	writeCommandWorkspaceFile(t, defaultWorkspace, "skills/edge-tts/SKILL.md", "---\nname: edge-tts\ndescription: edge tts\n---\n# Edge TTS\n")
	writeCommandWorkspaceFile(t, legacyWorkspace, "AGENTS.md", "# legacy agents\n")

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = defaultWorkspace
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	t.Setenv("PICOCLAW_CONFIG", configPath)

	cmd := newSyncDefaultsCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--force-legacy"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute force-legacy: %v", err)
	}

	assertCommandFileContent(
		t,
		filepath.Join(legacyWorkspace, "skills", "edge-tts", "SKILL.md"),
		"---\nname: edge-tts\ndescription: edge tts\n---\n# Edge TTS\n",
	)
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

func embeddedCommandWorkspaceFile(t *testing.T, relPath string) string {
	t.Helper()
	targetDir := t.TempDir()
	if err := onboard.CopyEmbeddedWorkspaceTemplates(targetDir); err != nil {
		t.Fatalf("CopyEmbeddedWorkspaceTemplates: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(targetDir, filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read embedded %s: %v", relPath, err)
	}
	return string(data)
}
