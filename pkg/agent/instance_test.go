package agent

import (
	"bytes"
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

func TestNewAgentInstance_UsesConfiguredSummarizeThresholds(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:                 tmpDir,
				Model:                     "test-model",
				MaxTokens:                 1234,
				MaxToolIterations:         5,
				SummarizeMessageThreshold: 9,
				SummarizeTokenPercent:     66,
			},
		},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if agent.SummarizeMessageThreshold != 9 {
		t.Fatalf("SummarizeMessageThreshold = %d, want %d", agent.SummarizeMessageThreshold, 9)
	}
	if agent.SummarizeTokenPercent != 66 {
		t.Fatalf("SummarizeTokenPercent = %d, want %d", agent.SummarizeTokenPercent, 66)
	}
}

func TestNewAgentInstance_DefaultsSummarizeThresholdsWhenUnset(t *testing.T) {
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

	if agent.SummarizeMessageThreshold != 20 {
		t.Fatalf("SummarizeMessageThreshold = %d, want %d", agent.SummarizeMessageThreshold, 20)
	}
	if agent.SummarizeTokenPercent != 75 {
		t.Fatalf("SummarizeTokenPercent = %d, want %d", agent.SummarizeTokenPercent, 75)
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

	sourceSkillFiles := map[string]string{
		"skills/weather/SKILL.md":           "# source weather\n",
		"skills/weather/scripts/run.sh":     "echo source\n",
		"skills/weather/references/doc.txt": "hello\n",
	}
	for rel, content := range sourceSkillFiles {
		writeWorkspaceFile(t, defaultWorkspace, rel, content)
	}
	sourceSkillAssetRel := "skills/generate-image/assets/momonga_refs_sheet.png"
	sourceSkillAsset := []byte{0x89, 0x50, 0x4E, 0x47, 0x01, 0x02, 0x03, 0x04}
	writeWorkspaceBytes(t, defaultWorkspace, sourceSkillAssetRel, sourceSkillAsset)

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

	if info, err := os.Stat(filepath.Join(autoWorkspace, "skills")); err != nil {
		t.Fatalf("skills directory should be created: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("skills path should be a directory")
	}

	for rel, want := range sourceSkillFiles {
		targetPath := filepath.Join(autoWorkspace, filepath.FromSlash(rel))
		data, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(data) != want {
			t.Fatalf("content of %s = %q, want %q", rel, string(data), want)
		}
	}
	assetData, err := os.ReadFile(filepath.Join(autoWorkspace, filepath.FromSlash(sourceSkillAssetRel)))
	if err != nil {
		t.Fatalf("read %s: %v", sourceSkillAssetRel, err)
	}
	if !bytes.Equal(assetData, sourceSkillAsset) {
		t.Fatalf("binary asset content mismatch for %s", sourceSkillAssetRel)
	}

	if _, err := os.Stat(filepath.Join(autoWorkspace, "USER.md")); !os.IsNotExist(err) {
		t.Fatalf("USER.md should not be seeded, got err=%v", err)
	}
}

func TestNewAgentInstance_PersonaPresetOverridesCopiedSoulAndIdentity(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	writeWorkspaceFile(t, defaultWorkspace, "AGENTS.md", "# source agents\n")
	writeWorkspaceFile(t, defaultWorkspace, "SOUL.md", "# source soul\n")
	writeWorkspaceFile(t, defaultWorkspace, "IDENTITY.md", "# source identity\n")
	writeWorkspaceFile(t, defaultWorkspace, "STATE.md", "# source state\n")
	writeWorkspaceFile(t, defaultWorkspace, "memory/MEMORY.md", "# source memory\n")

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:     defaultWorkspace,
				Model:         "test-model",
				PersonaPreset: "momonga",
			},
		},
	}

	provider := &mockProvider{}
	NewAgentInstance(&config.AgentConfig{ID: "auto-persona", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	agentsData, err := os.ReadFile(filepath.Join(autoWorkspace, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(agentsData) != "# source agents\n" {
		t.Fatalf("AGENTS.md should still come from source workspace")
	}

	soulData, err := os.ReadFile(filepath.Join(autoWorkspace, "SOUL.md"))
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	if string(soulData) == "# source soul\n" {
		t.Fatalf("SOUL.md should be overridden by persona preset")
	}
	if !strings.Contains(strings.ToLower(string(soulData)), "momonga") {
		t.Fatalf("SOUL.md should contain momonga persona content")
	}

	identityData, err := os.ReadFile(filepath.Join(autoWorkspace, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("read IDENTITY.md: %v", err)
	}
	if string(identityData) == "# source identity\n" {
		t.Fatalf("IDENTITY.md should be overridden by persona preset")
	}
	if !strings.Contains(strings.ToLower(string(identityData)), "momonga") {
		t.Fatalf("IDENTITY.md should contain momonga persona content")
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

func TestDefaultWorkspaceBootstrapContent_ProactiveGuidanceIncludesAntiRepeatAndWalkImageBias(t *testing.T) {
	agents := defaultWorkspaceBootstrapContent["AGENTS.md"]
	if !strings.Contains(agents, "Do not repeat the same proactive point when the user has not replied") {
		t.Fatalf("expected anti-repeat proactive guidance in fallback AGENTS.md template")
	}
	if !strings.Contains(agents, "treat that as a stronger reason to share a brief life update or a scene image") {
		t.Fatalf("expected walk/image proactive guidance in fallback AGENTS.md template")
	}
	if !strings.Contains(agents, "momonga_refs_sheet.png") {
		t.Fatalf("expected selfie reference guidance in fallback AGENTS.md template")
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

func TestNewAgentInstance_DoesNotOverwriteExistingSkillFiles(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace-main")
	autoWorkspace := filepath.Join(root, "workspace-auto")

	writeWorkspaceFile(t, defaultWorkspace, "skills/weather/SKILL.md", "# source weather\n")
	writeWorkspaceFile(t, defaultWorkspace, "skills/weather/scripts/run.sh", "echo source\n")

	writeWorkspaceFile(t, autoWorkspace, "skills/weather/SKILL.md", "# custom weather\n")

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: defaultWorkspace,
				Model:     "test-model",
			},
		},
	}

	provider := &mockProvider{}
	NewAgentInstance(&config.AgentConfig{ID: "auto-4", Workspace: autoWorkspace}, &cfg.Agents.Defaults, cfg, provider)

	skillData, err := os.ReadFile(filepath.Join(autoWorkspace, "skills", "weather", "SKILL.md"))
	if err != nil {
		t.Fatalf("read custom skill file: %v", err)
	}
	if string(skillData) != "# custom weather\n" {
		t.Fatalf("skill file was overwritten: got %q", string(skillData))
	}

	scriptData, err := os.ReadFile(filepath.Join(autoWorkspace, "skills", "weather", "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("read inherited script file: %v", err)
	}
	if string(scriptData) != "echo source\n" {
		t.Fatalf("missing inherited script content: got %q", string(scriptData))
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

func writeWorkspaceBytes(t *testing.T, workspace, relPath string, content []byte) {
	t.Helper()
	fullPath := filepath.Join(workspace, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func TestResolveAgentWorkspace_UsesDefaultWorkspaceRootForNamedAgents(t *testing.T) {
	defaults := &config.AgentDefaults{Workspace: filepath.Join("/custom", "picoclaw", "workspace")}
	agentCfg := &config.AgentConfig{ID: "helper-bot"}
	got := resolveAgentWorkspace(agentCfg, defaults)
	want := filepath.Join("/custom", "picoclaw", "workspace-helper-bot")
	if got != want {
		t.Fatalf("resolveAgentWorkspace() = %q, want %q", got, want)
	}
}
