package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestSyncWorkspaceDefaults_UpdatesManagedFilesButNotStateOrMemory(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents v1\n")
	writeWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul v1\n")
	writeWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity v1\n")

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: defaultWorkspace,
				Model:     "test-model",
			},
		},
	}

	provider := &mockProvider{}
	NewAgentInstance(&config.AgentConfig{ID: "auto-1", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	writeWorkspaceFile(t, autoWorkspace, "STATE.md", "# custom state\n")
	writeWorkspaceFile(t, autoWorkspace, "memory/MEMORY.md", "# custom memory\n")
	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents v2\n")
	writeWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul v2\n")

	NewAgentInstance(&config.AgentConfig{ID: "auto-1", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	assertFileContent(t, filepath.Join(autoWorkspace, "AGENTS.md"), "# source agents v2\n")
	assertFileContent(t, filepath.Join(autoWorkspace, "SOUL.md"), "# source soul v2\n")
	assertFileContent(t, filepath.Join(autoWorkspace, "STATE.md"), "# custom state\n")
	assertFileContent(t, filepath.Join(autoWorkspace, "memory", "MEMORY.md"), "# custom memory\n")

	if _, err := os.Stat(filepath.Join(autoWorkspace, bootstrapMetadataFilename)); err != nil {
		t.Fatalf("expected bootstrap metadata to exist: %v", err)
	}
}

func TestSyncWorkspaceDefaults_DoesNotOverwriteLocalManagedEdits(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents v1\n")
	writeWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul\n")
	writeWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity\n")

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: defaultWorkspace,
				Model:     "test-model",
			},
		},
	}

	provider := &mockProvider{}
	NewAgentInstance(&config.AgentConfig{ID: "auto-1", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	writeWorkspaceFile(t, autoWorkspace, "AGENTS.md", "# local agents\n")
	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents v2\n")

	report, err := SyncWorkspaceDefaults(autoWorkspace, &cfg.Agents.Defaults, WorkspaceDefaultsSyncOptions{})
	if err != nil {
		t.Fatalf("SyncWorkspaceDefaults: %v", err)
	}
	if !containsString(report.Preserved, "AGENTS.md") {
		t.Fatalf("expected AGENTS.md to be preserved, got %#v", report.Preserved)
	}
	assertFileContent(t, filepath.Join(autoWorkspace, "AGENTS.md"), "# local agents\n")
}

func TestSyncWorkspaceDefaults_LegacyConflictRequiresForce(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents\n")
	writeWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul\n")
	writeWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity\n")
	writeWorkspaceFile(t, autoWorkspace, "AGENTS.md", "# legacy agents\n")

	defaults := &config.AgentDefaults{
		Workspace: defaultWorkspace,
		Model:     "test-model",
	}

	report, err := SyncWorkspaceDefaults(autoWorkspace, defaults, WorkspaceDefaultsSyncOptions{})
	if err != nil {
		t.Fatalf("SyncWorkspaceDefaults safe mode: %v", err)
	}
	if !containsString(report.Conflicts, "AGENTS.md") {
		t.Fatalf("expected AGENTS.md conflict, got %#v", report.Conflicts)
	}
	assertFileContent(t, filepath.Join(autoWorkspace, "AGENTS.md"), "# legacy agents\n")

	report, err = SyncWorkspaceDefaults(autoWorkspace, defaults, WorkspaceDefaultsSyncOptions{ForceLegacy: true})
	if err != nil {
		t.Fatalf("SyncWorkspaceDefaults force mode: %v", err)
	}
	if !containsString(report.Updated, "AGENTS.md") {
		t.Fatalf("expected AGENTS.md update, got %#v", report.Updated)
	}
	assertFileContent(t, filepath.Join(autoWorkspace, "AGENTS.md"), "# source agents\n")
}

func TestSyncWorkspaceDefaults_DeletesManagedSkillsAndPreservesLocalEdits(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents\n")
	writeWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul\n")
	writeWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity\n")
	writeWorkspaceFile(t, defaultWorkspace, "skills/weather/SKILL.md", "# weather\n")
	writeWorkspaceFile(t, defaultWorkspace, "skills/weather/scripts/run.sh", "echo source\n")

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: defaultWorkspace,
				Model:     "test-model",
			},
		},
	}

	provider := &mockProvider{}
	NewAgentInstance(&config.AgentConfig{ID: "auto-1", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	writeWorkspaceFile(t, autoWorkspace, "skills/weather/scripts/run.sh", "echo local\n")
	if err := os.Remove(filepath.Join(defaultWorkspace, "skills", "weather", "SKILL.md")); err != nil {
		t.Fatalf("remove source SKILL.md: %v", err)
	}
	if err := os.Remove(filepath.Join(defaultWorkspace, "skills", "weather", "scripts", "run.sh")); err != nil {
		t.Fatalf("remove source run.sh: %v", err)
	}

	report, err := SyncWorkspaceDefaults(autoWorkspace, &cfg.Agents.Defaults, WorkspaceDefaultsSyncOptions{})
	if err != nil {
		t.Fatalf("SyncWorkspaceDefaults: %v", err)
	}
	if !containsString(report.Deleted, "skills/weather/SKILL.md") {
		t.Fatalf("expected skill deletion, got %#v", report.Deleted)
	}
	if !containsString(report.Preserved, "skills/weather/scripts/run.sh") {
		t.Fatalf("expected local skill edit to be preserved, got %#v", report.Preserved)
	}

	if _, err := os.Stat(filepath.Join(autoWorkspace, "skills", "weather", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected managed skill file to be removed, err=%v", err)
	}
	assertFileContent(t, filepath.Join(autoWorkspace, "skills", "weather", "scripts", "run.sh"), "echo local\n")
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("content of %s = %q, want %q", path, string(data), want)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
