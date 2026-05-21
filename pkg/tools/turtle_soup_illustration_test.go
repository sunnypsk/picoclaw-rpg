package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type turtleSoupReviewProvider struct {
	responses []string
	calls     [][]providers.Message
	models    []string
}

func (p *turtleSoupReviewProvider) Chat(
	_ context.Context,
	messages []providers.Message,
	_ []providers.ToolDefinition,
	model string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	cloned := make([]providers.Message, len(messages))
	copy(cloned, messages)
	p.calls = append(p.calls, cloned)
	p.models = append(p.models, model)
	response := `{"safe":true}`
	if len(p.responses) > 0 {
		response = p.responses[0]
		p.responses = p.responses[1:]
	}
	return &providers.LLMResponse{Content: response}, nil
}

func (p *turtleSoupReviewProvider) GetDefaultModel() string {
	return "mock-model"
}

type fakeTurtleSoupImageGenerator struct {
	result *ToolResult
	calls  []map[string]any
}

func (g *fakeTurtleSoupImageGenerator) Execute(_ context.Context, args map[string]any) *ToolResult {
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	g.calls = append(g.calls, cloned)
	return g.result
}

func TestTurtleSoupToolStartReturnsReviewedIllustrationMedia(t *testing.T) {
	engine := turtlesoup.NewEngine(turtlesoup.NewStore(t.TempDir()), []turtlesoup.Puzzle{{
		ID:       "test",
		Surface:  "public taxi scene",
		Solution: "hidden police station rescue",
	}})
	store := media.NewFileMediaStore()
	ref, _ := storeTestIllustration(t, store, true)
	imageGenerator := &fakeTurtleSoupImageGenerator{result: MediaResult("generated", []string{ref})}
	provider := &turtleSoupReviewProvider{responses: []string{`{"safe":true}`, `{"safe":true}`}}
	tool := NewTurtleSoupTool(engine, nil, "")
	tool.setStartIllustrator(turtleSoupStartIllustrator{
		provider:      provider,
		modelResolver: func() string { return "review-model" },
		imageTool:     imageGenerator,
		mediaStore:    func() media.MediaStore { return store },
	})
	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", "session-1", nil)

	result := tool.Execute(ctx, map[string]any{"action": "start"})
	if result == nil || result.IsError {
		t.Fatalf("start result error = %+v", result)
	}
	if len(result.Media) != 1 || result.Media[0] != ref {
		t.Fatalf("media = %v, want [%s]", result.Media, ref)
	}
	if strings.Contains(result.ForLLM, "hidden police station rescue") {
		t.Fatalf("visible start result leaked solution: %q", result.ForLLM)
	}
	if len(imageGenerator.calls) != 1 {
		t.Fatalf("image generator calls = %d, want 1", len(imageGenerator.calls))
	}
	prompt, _ := imageGenerator.calls[0]["prompt"].(string)
	if !strings.Contains(prompt, "public taxi scene") {
		t.Fatalf("image prompt should include public surface, got %q", prompt)
	}
	if strings.Contains(prompt, "hidden police station rescue") {
		t.Fatalf("image prompt leaked hidden solution: %q", prompt)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("review calls = %d, want prompt and image review", len(provider.calls))
	}
	if len(provider.calls[1][1].Media) != 1 || !strings.HasPrefix(provider.calls[1][1].Media[0], "data:image/png;base64,") {
		t.Fatalf("image review should receive generated image data URL, got %+v", provider.calls[1][1].Media)
	}
	if len(provider.models) != 2 || provider.models[0] != "review-model" || provider.models[1] != "review-model" {
		t.Fatalf("review models = %v, want review-model twice", provider.models)
	}
}

func TestTurtleSoupToolSuppressesUnsafeGeneratedIllustration(t *testing.T) {
	engine := turtlesoup.NewEngine(turtlesoup.NewStore(t.TempDir()), []turtlesoup.Puzzle{{
		ID:       "test",
		Surface:  "public taxi scene",
		Solution: "hidden police station rescue",
	}})
	store := media.NewFileMediaStore()
	ref, imagePath := storeTestIllustration(t, store, true)
	imageGenerator := &fakeTurtleSoupImageGenerator{result: MediaResult("generated", []string{ref})}
	provider := &turtleSoupReviewProvider{responses: []string{`{"safe":true}`, `{"safe":false}`}}
	tool := NewTurtleSoupTool(engine, nil, "")
	tool.setStartIllustrator(turtleSoupStartIllustrator{
		provider:      provider,
		modelResolver: func() string { return "review-model" },
		imageTool:     imageGenerator,
		mediaStore:    func() media.MediaStore { return store },
	})
	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", "session-1", nil)

	result := tool.Execute(ctx, map[string]any{"action": "start"})
	if result == nil || result.IsError {
		t.Fatalf("start result error = %+v", result)
	}
	if len(result.Media) != 0 {
		t.Fatalf("unsafe illustration should be suppressed, got media %v", result.Media)
	}
	if !strings.Contains(result.ForLLM, "public taxi scene") {
		t.Fatalf("game should still start with public surface, got %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "安全") {
		t.Fatalf("unsafe illustration should add a safety note, got %q", result.ForLLM)
	}
	if _, err := os.Stat(imagePath); !os.IsNotExist(err) {
		t.Fatalf("owned unsafe generated media should be deleted, stat err = %v", err)
	}
}

func TestTurtleSoupToolDoesNotIllustrateAlreadyActiveStart(t *testing.T) {
	engine := turtlesoup.NewEngine(turtlesoup.NewStore(t.TempDir()), []turtlesoup.Puzzle{{
		ID:       "test",
		Surface:  "public taxi scene",
		Solution: "hidden police station rescue",
	}})
	store := media.NewFileMediaStore()
	ref, _ := storeTestIllustration(t, store, true)
	imageGenerator := &fakeTurtleSoupImageGenerator{result: MediaResult("generated", []string{ref})}
	provider := &turtleSoupReviewProvider{responses: []string{`{"safe":true}`, `{"safe":true}`}}
	tool := NewTurtleSoupTool(engine, nil, "")
	tool.setStartIllustrator(turtleSoupStartIllustrator{
		provider:      provider,
		modelResolver: func() string { return "review-model" },
		imageTool:     imageGenerator,
		mediaStore:    func() media.MediaStore { return store },
	})
	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", "session-1", nil)

	first := tool.Execute(ctx, map[string]any{"action": "start"})
	if first == nil || first.IsError || len(first.Media) != 1 {
		t.Fatalf("first start result = %+v", first)
	}
	second := tool.Execute(ctx, map[string]any{"action": "start"})
	if second == nil || second.IsError {
		t.Fatalf("second start result = %+v", second)
	}
	if len(second.Media) != 0 {
		t.Fatalf("already-active start should not return media, got %v", second.Media)
	}
	if len(imageGenerator.calls) != 1 {
		t.Fatalf("image generator calls = %d, want only initial start", len(imageGenerator.calls))
	}
}

func TestTurtleSoupToolDoesNotIllustrateNonStartActions(t *testing.T) {
	engine := turtlesoup.NewEngine(turtlesoup.NewStore(t.TempDir()), []turtlesoup.Puzzle{{
		ID:       "test",
		Surface:  "public taxi scene",
		Solution: "hidden police station rescue",
		Hints:    []string{"first hint"},
	}})
	sessionKey := "session-1"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	store := media.NewFileMediaStore()
	ref, _ := storeTestIllustration(t, store, true)
	imageGenerator := &fakeTurtleSoupImageGenerator{result: MediaResult("generated", []string{ref})}
	reviewProvider := &turtleSoupReviewProvider{responses: []string{`{"safe":true}`, `{"safe":true}`}}
	judgeProvider := &turtleSoupToolProvider{response: `{"kind":"question","label":"yes"}`}
	tool := NewTurtleSoupTool(engine, judgeProvider, "judge-model")
	tool.setStartIllustrator(turtleSoupStartIllustrator{
		provider:      reviewProvider,
		modelResolver: func() string { return "review-model" },
		imageTool:     imageGenerator,
		mediaStore:    func() media.MediaStore { return store },
	})
	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", sessionKey, nil)

	for _, args := range []map[string]any{
		{"action": "hint"},
		{"action": "status"},
		{"action": "turn", "message": "is the driver involved?"},
	} {
		result := tool.Execute(ctx, args)
		if result == nil || result.IsError {
			t.Fatalf("tool result for %v = %+v", args, result)
		}
		if len(result.Media) != 0 {
			t.Fatalf("non-start action %v should not return media, got %v", args, result.Media)
		}
	}
	if len(imageGenerator.calls) != 0 {
		t.Fatalf("image generator calls = %d, want none for non-start actions", len(imageGenerator.calls))
	}
	if len(reviewProvider.calls) != 0 {
		t.Fatalf("review calls = %d, want none for non-start actions", len(reviewProvider.calls))
	}
}

func storeTestIllustration(t *testing.T, store media.MediaStore, owned bool) (string, string) {
	t.Helper()
	imagePath := filepath.Join(t.TempDir(), "illustration.png")
	if err := os.WriteFile(imagePath, testPNGBytes(t), 0o600); err != nil {
		t.Fatalf("WriteFile(image) error = %v", err)
	}
	ref, err := store.Store(imagePath, media.MediaMeta{
		Filename:    "illustration.png",
		ContentType: "image/png",
		Source:      "test",
		Owned:       owned,
	}, "test")
	if err != nil {
		t.Fatalf("Store(image) error = %v", err)
	}
	return ref, imagePath
}
