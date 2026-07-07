package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type routeCaptureProvider struct {
	name  string
	err   error
	calls []routeCaptureCall
}

type routeCaptureCall struct {
	model string
	media []string
}

func (p *routeCaptureProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	media := []string(nil)
	for _, message := range messages {
		media = append(media, message.Media...)
	}
	p.calls = append(p.calls, routeCaptureCall{model: model, media: media})
	if p.err != nil {
		return nil, p.err
	}
	return &providers.LLMResponse{Content: p.name + " response", FinishReason: "stop"}, nil
}

func (p *routeCaptureProvider) GetDefaultModel() string {
	return p.name
}

func newRouteTestLoop() *AgentLoop {
	return &AgentLoop{
		fallback:   providers.NewFallbackChain(providers.NewCooldownTracker()),
		retrySleep: func(d time.Duration) {},
	}
}

func newRouteTestAgent(textProvider, visionProvider providers.LLMProvider) *AgentInstance {
	if visionProvider == nil {
		visionProvider = textProvider
	}
	return &AgentInstance{
		ID:            "test-agent",
		Model:         "text-model",
		MaxIterations: 1,
		MaxTokens:     1024,
		Temperature:   0.7,
		Provider:      textProvider,
		Tools:         tools.NewToolRegistry(),
		TextRoute: modelRoute{
			Name: "text",
			Candidates: []modelRouteCandidate{{
				Alias:        "text",
				ProviderName: "openai",
				Model:        "text-model",
				Provider:     textProvider,
			}},
		},
		VisionRoute: modelRoute{
			Name: "vision",
			Candidates: []modelRouteCandidate{{
				Alias:          "vision",
				ProviderName:   "openai",
				Model:          "vision-model",
				Provider:       visionProvider,
				SupportsVision: true,
			}},
		},
	}
}

func TestRunLLMIteration_TextOnlyUsesTextRoute(t *testing.T) {
	textProvider := &routeCaptureProvider{name: "text"}
	visionProvider := &routeCaptureProvider{name: "vision"}
	agent := newRouteTestAgent(textProvider, visionProvider)

	got, _, err := newRouteTestLoop().runLLMIteration(context.Background(), agent, []providers.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "hello"},
	}, processOptions{})
	if err != nil {
		t.Fatalf("runLLMIteration() error = %v", err)
	}
	if got != "text response" {
		t.Fatalf("response = %q, want text response", got)
	}
	if len(textProvider.calls) != 1 {
		t.Fatalf("text calls = %d, want 1", len(textProvider.calls))
	}
	if textProvider.calls[0].model != "text-model" {
		t.Fatalf("text model = %q, want text-model", textProvider.calls[0].model)
	}
	if len(visionProvider.calls) != 0 {
		t.Fatalf("vision calls = %d, want 0", len(visionProvider.calls))
	}
}

func TestRunLLMIteration_ImageUsesVisionRouteAndKeepsMedia(t *testing.T) {
	textProvider := &routeCaptureProvider{name: "text"}
	visionProvider := &routeCaptureProvider{name: "vision"}
	agent := newRouteTestAgent(textProvider, visionProvider)

	got, _, err := newRouteTestLoop().runLLMIteration(context.Background(), agent, []providers.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "describe this", Media: []string{"data:image/png;base64,abc"}},
	}, processOptions{CurrentImageMedia: []string{"media://image"}})
	if err != nil {
		t.Fatalf("runLLMIteration() error = %v", err)
	}
	if got != "vision response" {
		t.Fatalf("response = %q, want vision response", got)
	}
	if len(textProvider.calls) != 0 {
		t.Fatalf("text calls = %d, want 0", len(textProvider.calls))
	}
	if len(visionProvider.calls) != 1 {
		t.Fatalf("vision calls = %d, want 1", len(visionProvider.calls))
	}
	if visionProvider.calls[0].model != "vision-model" {
		t.Fatalf("vision model = %q, want vision-model", visionProvider.calls[0].model)
	}
	if len(visionProvider.calls[0].media) != 1 || visionProvider.calls[0].media[0] != "data:image/png;base64,abc" {
		t.Fatalf("vision media = %#v, want image data URL", visionProvider.calls[0].media)
	}
}

func TestRunLLMIteration_ImageWithoutVisionRouteKeepsTextRoute(t *testing.T) {
	textProvider := &routeCaptureProvider{name: "text"}
	agent := newRouteTestAgent(textProvider, nil)
	agent.VisionRoute = modelRoute{Name: "vision"}

	got, _, err := newRouteTestLoop().runLLMIteration(context.Background(), agent, []providers.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "describe this", Media: []string{"data:image/png;base64,abc"}},
	}, processOptions{})
	if err != nil {
		t.Fatalf("runLLMIteration() error = %v", err)
	}
	if got != "text response" {
		t.Fatalf("response = %q, want text response", got)
	}
	if len(textProvider.calls) != 1 {
		t.Fatalf("text calls = %d, want 1", len(textProvider.calls))
	}
}

func TestRunLLMIteration_TextVisionModelHandlesImagesWithoutSeparateVisionRoute(t *testing.T) {
	textProvider := &routeCaptureProvider{name: "text"}
	agent := newRouteTestAgent(textProvider, nil)
	agent.VisionRoute = modelRoute{Name: "vision"}
	agent.TextRoute.Candidates[0].SupportsVision = true

	got, _, err := newRouteTestLoop().runLLMIteration(context.Background(), agent, []providers.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "describe this", Media: []string{"data:image/png;base64,abc"}},
	}, processOptions{CurrentImageMedia: []string{"media://image"}})
	if err != nil {
		t.Fatalf("runLLMIteration() error = %v", err)
	}
	if got != "text response" {
		t.Fatalf("response = %q, want text response", got)
	}
	if len(textProvider.calls) != 1 {
		t.Fatalf("text calls = %d, want 1", len(textProvider.calls))
	}
}

func TestRunLLMIteration_ImageUnsupportedRetriesVisionRoute(t *testing.T) {
	textProvider := &routeCaptureProvider{
		name: "text",
		err:  errors.New("model does not support image input"),
	}
	visionProvider := &routeCaptureProvider{name: "vision"}
	agent := newRouteTestAgent(textProvider, visionProvider)
	agent.TextRoute.Candidates[0].SupportsVision = true
	agent.VisionRoute.Candidates[0].ProviderName = "gemini"

	got, _, err := newRouteTestLoop().runLLMIteration(context.Background(), agent, []providers.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "describe this", Media: []string{"data:image/png;base64,abc"}},
	}, processOptions{})
	if err != nil {
		t.Fatalf("runLLMIteration() error = %v", err)
	}
	if got != "vision response" {
		t.Fatalf("response = %q, want vision response", got)
	}
	if len(textProvider.calls) != 1 {
		t.Fatalf("text calls = %d, want 1", len(textProvider.calls))
	}
	if len(visionProvider.calls) != 1 {
		t.Fatalf("vision calls = %d, want 1", len(visionProvider.calls))
	}
}

func TestRunLLMIteration_FallbackCandidatesUseOwnProviders(t *testing.T) {
	firstProvider := &routeCaptureProvider{name: "first", err: errors.New("rate limit exceeded")}
	secondProvider := &routeCaptureProvider{name: "second"}
	agent := newRouteTestAgent(firstProvider, nil)
	agent.TextRoute = modelRoute{
		Name: "text",
		Candidates: []modelRouteCandidate{
			{
				Alias:        "first",
				ProviderName: "openai",
				Model:        "first-model",
				Provider:     firstProvider,
			},
			{
				Alias:        "second",
				ProviderName: "anthropic",
				Model:        "second-model",
				Provider:     secondProvider,
			},
		},
	}
	agent.Candidates = agent.TextRoute.fallbackCandidates()

	got, _, err := newRouteTestLoop().runLLMIteration(context.Background(), agent, []providers.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "hello"},
	}, processOptions{})
	if err != nil {
		t.Fatalf("runLLMIteration() error = %v", err)
	}
	if got != "second response" {
		t.Fatalf("response = %q, want second response", got)
	}
	if len(firstProvider.calls) != 1 {
		t.Fatalf("first calls = %d, want 1", len(firstProvider.calls))
	}
	if len(secondProvider.calls) != 1 {
		t.Fatalf("second calls = %d, want 1", len(secondProvider.calls))
	}
	if secondProvider.calls[0].model != "second-model" {
		t.Fatalf("second model = %q, want second-model", secondProvider.calls[0].model)
	}
}

func TestBuildModelRoute_UsesProviderInstancePerModelConfig(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{Workspace: t.TempDir()},
		},
		ModelList: []config.ModelConfig{
			{
				ModelName:      "text",
				Model:          "deepseek/deepseek-chat",
				APIBase:        "https://api.deepseek.test/v1",
				SupportsVision: false,
			},
			{
				ModelName:      "vision",
				Model:          "openai/gpt-4o",
				APIBase:        "https://api.openai.test/v1",
				SupportsVision: true,
			},
		},
	}

	textRoute, err := buildModelRoute("text", cfg, "", "text", nil, &routeCaptureProvider{name: "default"}, false)
	if err != nil {
		t.Fatalf("build text route: %v", err)
	}
	visionRoute, err := buildModelRoute("vision", cfg, "", "vision", nil, &routeCaptureProvider{name: "default"}, true)
	if err != nil {
		t.Fatalf("build vision route: %v", err)
	}

	if textRoute.primary().ProviderName != "deepseek" || textRoute.primary().Model != "deepseek-chat" {
		t.Fatalf("text route candidate = %s/%s, want deepseek/deepseek-chat",
			textRoute.primary().ProviderName, textRoute.primary().Model)
	}
	if visionRoute.primary().ProviderName != "openai" || visionRoute.primary().Model != "gpt-4o" {
		t.Fatalf("vision route candidate = %s/%s, want openai/gpt-4o",
			visionRoute.primary().ProviderName, visionRoute.primary().Model)
	}
	if textRoute.primary().Provider == visionRoute.primary().Provider {
		t.Fatal("text and vision routes should use distinct provider instances")
	}
}
