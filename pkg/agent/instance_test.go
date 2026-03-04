package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestNewAgentInstance_UsesDefaultsTemperatureAndMaxTokens(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         1234,
				MaxToolIterations: 5,
			},
		},
	}

	configuredTemp := 1.0
	cfg.Agents.Defaults.Temperature = &configuredTemp

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if agent.MaxTokens != 1234 {
		t.Fatalf("MaxTokens = %d, want %d", agent.MaxTokens, 1234)
	}
	if agent.Temperature != 1.0 {
		t.Fatalf("Temperature = %f, want %f", agent.Temperature, 1.0)
	}
}

func TestNewAgentInstance_DefaultsTemperatureWhenZero(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         1234,
				MaxToolIterations: 5,
			},
		},
	}

	configuredTemp := 0.0
	cfg.Agents.Defaults.Temperature = &configuredTemp

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if agent.Temperature != 0.0 {
		t.Fatalf("Temperature = %f, want %f", agent.Temperature, 0.0)
	}
}

func TestNewAgentInstance_DefaultsTemperatureWhenUnset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         1234,
				MaxToolIterations: 5,
			},
		},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if agent.Temperature != 0.7 {
		t.Fatalf("Temperature = %f, want %f", agent.Temperature, 0.7)
	}
}

func TestNewAgentInstance_ResolveCandidatesFromModelListAlias(t *testing.T) {
	tests := []struct {
		name         string
		aliasName    string
		modelName    string
		apiBase      string
		wantProvider string
		wantModel    string
	}{
		{
			name:         "alias with provider prefix",
			aliasName:    "step-3.5-flash",
			modelName:    "openrouter/stepfun/step-3.5-flash:free",
			apiBase:      "https://openrouter.ai/api/v1",
			wantProvider: "openrouter",
			wantModel:    "stepfun/step-3.5-flash:free",
		},
		{
			name:         "alias without provider prefix",
			aliasName:    "glm-5",
			modelName:    "glm-5",
			apiBase:      "https://api.z.ai/api/coding/paas/v4",
			wantProvider: "openai",
			wantModel:    "glm-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			cfg := &config.Config{
				Agents: config.AgentsConfig{
					Defaults: config.AgentDefaults{
						Workspace: tmpDir,
						Model:     tt.aliasName,
					},
				},
				ModelList: []config.ModelConfig{
					{
						ModelName: tt.aliasName,
						Model:     tt.modelName,
						APIBase:   tt.apiBase,
					},
				},
			}

			provider := &mockProvider{}
			agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

			if len(agent.Candidates) != 1 {
				t.Fatalf("len(Candidates) = %d, want 1", len(agent.Candidates))
			}
			if agent.Candidates[0].Provider != tt.wantProvider {
				t.Fatalf("candidate provider = %q, want %q", agent.Candidates[0].Provider, tt.wantProvider)
			}
			if agent.Candidates[0].Model != tt.wantModel {
				t.Fatalf("candidate model = %q, want %q", agent.Candidates[0].Model, tt.wantModel)
			}
		})
	}
}

func TestNewAgentInstance_SeedsBootstrapFilesFromDefaultWorkspace(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	sourceFiles := map[string]string{
		"AGENTS.md":        "# source agents\n",
		"SOUL.md":          "# source soul\n",
		"IDENTITY.md":      "# source identity\n",
		"STATE.md":         "# source state\n",
		"memory/MEMORY.md": "# source memory\n",
	}
	for rel, content := range sourceFiles {
		writeWorkspaceFile(t, defaultWorkspace, rel, content)
	}

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: defaultWorkspace,
				Model:     "test-model",
			},
		},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(&config.AgentConfig{ID: "auto-1", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	if agent.Workspace != autoWorkspace {
		t.Fatalf("workspace = %q, want %q", agent.Workspace, autoWorkspace)
	}

	for rel, want := range sourceFiles {
		targetPath := filepath.Join(autoWorkspace, filepath.FromSlash(rel))
		data, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(data) != want {
			t.Fatalf("content of %s = %q, want %q", rel, string(data), want)
		}
	}

	if _, err := os.Stat(filepath.Join(autoWorkspace, "USER.md")); !os.IsNotExist(err) {
		t.Fatalf("USER.md should not be seeded, got err=%v", err)
	}
}

func TestNewAgentInstance_SeedsFallbackBootstrapFilesWhenDefaultWorkspaceMissing(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	if err := os.MkdirAll(defaultWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir default workspace: %v", err)
	}

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: defaultWorkspace,
				Model:     "test-model",
			},
		},
	}

	provider := &mockProvider{}
	NewAgentInstance(&config.AgentConfig{ID: "auto-2", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	for _, rel := range workspaceBootstrapFiles {
		path := filepath.Join(autoWorkspace, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
		if len(data) == 0 {
			t.Fatalf("expected %s to be non-empty", rel)
		}
	}

	memoryPath := filepath.Join(autoWorkspace, "memory", "MEMORY.md")
	memoryData, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("read memory template: %v", err)
	}
	memoryText := string(memoryData)
	if !strings.Contains(memoryText, "user_timezone: to be confirmed") {
		t.Fatalf("memory template missing timezone placeholder")
	}
	if !strings.Contains(memoryText, "preferred_language: to be confirmed") {
		t.Fatalf("memory template missing preferred language placeholder")
	}
}

func TestNewAgentInstance_DoesNotOverwriteExistingBootstrapFile(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source\n")
	writeWorkspaceFile(t, autoWorkspace, "AGENTS.md", "# custom\n")

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: defaultWorkspace,
				Model:     "test-model",
			},
		},
	}

	provider := &mockProvider{}
	NewAgentInstance(&config.AgentConfig{ID: "auto-3", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	data, err := os.ReadFile(filepath.Join(autoWorkspace, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read custom AGENTS.md: %v", err)
	}
	if string(data) != "# custom\n" {
		t.Fatalf("AGENTS.md was overwritten: got %q", string(data))
	}
}

func writeWorkspaceFile(t *testing.T, workspace, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(workspace, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}
