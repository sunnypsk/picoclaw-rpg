package ui

import (
	"testing"

	picoclawconfig "github.com/sipeed/picoclaw/pkg/config"
)

func TestSelectedModelIndexSkipsStaticRows(t *testing.T) {
	menu := NewMenu("Models", []MenuItem{
		{Label: "Back"},
		{Label: "Add model"},
		{Label: "text"},
		{Label: "vision"},
	})

	tests := []struct {
		name      string
		row       int
		wantIndex int
		wantOK    bool
	}{
		{name: "back row", row: 0, wantOK: false},
		{name: "add row", row: 1, wantOK: false},
		{name: "first model", row: 2, wantIndex: 0, wantOK: true},
		{name: "second model", row: 3, wantIndex: 1, wantOK: true},
		{name: "past end", row: 4, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			menu.Select(tt.row, 0)
			got, ok := selectedModelIndex(menu, 2)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.wantIndex {
				t.Fatalf("index = %d, want %d", got, tt.wantIndex)
			}
		})
	}
}

func TestSyncRenamedModelReferencesUpdatesSelectedAliases(t *testing.T) {
	state := &appState{
		config: &picoclawconfig.Config{
			Agents: picoclawconfig.AgentsConfig{
				Defaults: picoclawconfig.AgentDefaults{
					ModelName:       "old",
					VisionModelName: "old",
				},
			},
			ModelList: []picoclawconfig.ModelConfig{
				{ModelName: "new", Model: "openai/gpt-5.2", SupportsVision: true},
			},
		},
	}

	state.syncRenamedModelReferences("old", 0)

	if got := state.config.Agents.Defaults.ModelName; got != "new" {
		t.Fatalf("ModelName = %q, want new", got)
	}
	if got := state.config.Agents.Defaults.VisionModelName; got != "new" {
		t.Fatalf("VisionModelName = %q, want new", got)
	}
}

func TestSyncRenamedModelReferencesKeepsAliasWhenLoadBalanceEntryRemains(t *testing.T) {
	state := &appState{
		config: &picoclawconfig.Config{
			Agents: picoclawconfig.AgentsConfig{
				Defaults: picoclawconfig.AgentDefaults{
					ModelName:       "shared",
					VisionModelName: "shared",
				},
			},
			ModelList: []picoclawconfig.ModelConfig{
				{ModelName: "renamed", Model: "openai/gpt-5.2", SupportsVision: true},
				{ModelName: "shared", Model: "openai/gpt-5.2-alt", SupportsVision: true},
			},
		},
	}

	state.syncRenamedModelReferences("shared", 0)

	if got := state.config.Agents.Defaults.ModelName; got != "shared" {
		t.Fatalf("ModelName = %q, want shared", got)
	}
	if got := state.config.Agents.Defaults.VisionModelName; got != "shared" {
		t.Fatalf("VisionModelName = %q, want shared", got)
	}
}
