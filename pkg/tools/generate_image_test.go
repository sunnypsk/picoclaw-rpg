package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/media"
)

func TestGenerateImageTool_RequiresEnv(t *testing.T) {
	tool := NewGenerateImageTool(t.TempDir(), false)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.getenv = func(string) string { return "" }

	result := tool.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(result.ForLLM, "CPA_API_KEY") {
		t.Fatalf("expected missing env message, got %q", result.ForLLM)
	}
}

func TestGenerateImageTool_LoadsCPAEnvFromPicoclawHome(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("CPA_API_KEY=file-key\nCPA_API_BASE=https://example.invalid\nCPA_IMAGE_MODEL=file-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := NewGenerateImageTool(t.TempDir(), false)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.getenv = func(name string) string {
		if name == "PICOCLAW_HOME" {
			return home
		}
		return ""
	}

	if got := tool.lookupEnv("CPA_API_KEY"); got != "file-key" {
		t.Fatalf("lookupEnv(CPA_API_KEY) = %q, want file-key", got)
	}
	if got := tool.lookupEnv("CPA_API_BASE"); got != "https://example.invalid" {
		t.Fatalf("lookupEnv(CPA_API_BASE) = %q", got)
	}
	if got := tool.lookupEnv("CPA_IMAGE_MODEL"); got != "file-model" {
		t.Fatalf("lookupEnv(CPA_IMAGE_MODEL) = %q", got)
	}
}

func TestGenerateImageTool_ProcessEnvOverridesEnvFile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("CPA_API_KEY=file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := NewGenerateImageTool(t.TempDir(), false)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.getenv = func(name string) string {
		switch name {
		case "PICOCLAW_HOME":
			return home
		case "CPA_API_KEY":
			return "process-key"
		default:
			return ""
		}
	}

	if got := tool.lookupEnv("CPA_API_KEY"); got != "process-key" {
		t.Fatalf("lookupEnv(CPA_API_KEY) = %q, want process-key", got)
	}
}

func TestGenerateImageTool_StoresURLResult(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	imageBytes := []byte("pngdata")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method %s", r.Method)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload["model"] != "test-model" {
				t.Fatalf("unexpected model: %v", payload["model"])
			}
			messages, ok := payload["messages"].([]any)
			if !ok || len(messages) != 1 {
				t.Fatalf("unexpected messages: %#v", payload["messages"])
			}
			msg, ok := messages[0].(map[string]any)
			if !ok || msg["role"] != "user" {
				t.Fatalf("unexpected message: %#v", messages[0])
			}
			content, ok := msg["content"].([]any)
			if !ok || len(content) != 1 {
				t.Fatalf("unexpected content: %#v", msg["content"])
			}
			part, ok := content[0].(map[string]any)
			if !ok || part["type"] != "text" || part["text"] != "cat" {
				t.Fatalf("unexpected content part: %#v", content[0])
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"url": server.URL + "/generated.png"}},
			})
		case "/generated.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tool := NewGenerateImageTool(workspace, false)
	tool.SetMediaStore(store)
	tool.getenv = func(name string) string {
		switch name {
		case "CPA_API_KEY":
			return "test-key"
		case "CPA_API_BASE":
			return server.URL
		case "CPA_IMAGE_MODEL":
			return "test-model"
		default:
			return ""
		}
	}

	result := tool.Execute(WithToolContext(context.Background(), "telegram", "chat-1"), map[string]any{"prompt": "cat"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected one media ref, got %v", result.Media)
	}
	path, meta, err := store.ResolveWithMeta(result.Media[0])
	if err != nil {
		t.Fatalf("resolve media ref: %v", err)
	}
	if meta.Source != "tool:generate_image" {
		t.Fatalf("unexpected media source %q", meta.Source)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored image: %v", err)
	}
	if string(data) != string(imageBytes) {
		t.Fatalf("unexpected stored image bytes: %q", string(data))
	}
}

func TestGenerateImageTool_IncludesNestedWorkspaceImageAndOptionsInChatPayload(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	sourceImage := filepath.Join(workspace, "skills", "generate-image", "assets", "momonga_refs_sheet.png")
	if err := os.MkdirAll(filepath.Dir(sourceImage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceImage, []byte("input-image"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		messages := payload["messages"].([]any)
		msg := messages[0].(map[string]any)
		content := msg["content"].([]any)
		if len(content) != 4 {
			t.Fatalf("unexpected content length: %d", len(content))
		}
		if content[1].(map[string]any)["text"] != "size: 1536x1024" {
			t.Fatalf("missing size part: %#v", content[1])
		}
		if content[2].(map[string]any)["text"] != "aspect_ratio: 16:9" {
			t.Fatalf("missing aspect ratio part: %#v", content[2])
		}
		imagePart := content[3].(map[string]any)
		if imagePart["type"] != "image_url" {
			t.Fatalf("unexpected image part type: %#v", imagePart)
		}
		imageURL := imagePart["image_url"].(map[string]any)["url"].(string)
		if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
			t.Fatalf("unexpected image url: %s", imageURL)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString([]byte("inline-image"))}},
		})
	}))
	defer server.Close()

	tool := NewGenerateImageTool(workspace, true)
	tool.SetMediaStore(store)
	tool.getenv = func(name string) string {
		switch name {
		case "CPA_API_KEY":
			return "test-key"
		case "CPA_API_BASE":
			return server.URL
		case "CPA_IMAGE_MODEL":
			return "test-model"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{
		"prompt":       "edit this",
		"image":        filepath.ToSlash(filepath.Join("skills", "generate-image", "assets", "momonga_refs_sheet.png")),
		"size":         "1536x1024",
		"aspect_ratio": "16:9",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected one media ref, got %v", result.Media)
	}
}

func TestGenerateImageTool_StoresNestedMessageImageURLResult(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	imageDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("nested-image"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"images": []map[string]any{{
						"image_url": map[string]any{
							"url": imageDataURL,
						},
					}},
				},
			}},
		})
	}))
	defer server.Close()

	tool := NewGenerateImageTool(workspace, false)
	tool.SetMediaStore(store)
	tool.getenv = func(name string) string {
		switch name {
		case "CPA_API_KEY":
			return "test-key"
		case "CPA_API_BASE":
			return server.URL
		case "CPA_IMAGE_MODEL":
			return "test-model"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected one media ref, got %v", result.Media)
	}
	path, _, err := store.ResolveWithMeta(result.Media[0])
	if err != nil {
		t.Fatalf("resolve media ref: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored image: %v", err)
	}
	if string(data) != "nested-image" {
		t.Fatalf("unexpected stored nested image bytes: %q", string(data))
	}
}

func TestGenerateImageTool_StoresB64JSONResult(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	imageBytes := []byte("inline-image")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(imageBytes)}},
		})
	}))
	defer server.Close()

	tool := NewGenerateImageTool(workspace, false)
	tool.SetMediaStore(store)
	tool.getenv = func(name string) string {
		switch name {
		case "CPA_API_KEY":
			return "test-key"
		case "CPA_API_BASE":
			return server.URL
		case "CPA_IMAGE_MODEL":
			return "test-model"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected one media ref, got %v", result.Media)
	}
	path, _, err := store.ResolveWithMeta(result.Media[0])
	if err != nil {
		t.Fatalf("resolve media ref: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored image: %v", err)
	}
	if string(data) != string(imageBytes) {
		t.Fatalf("unexpected stored inline image bytes: %q", string(data))
	}
}

func TestGenerateImageTool_RejectsEmptyResult(t *testing.T) {
	tool := NewGenerateImageTool(t.TempDir(), false)
	tool.SetMediaStore(media.NewFileMediaStore())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()
	tool.getenv = func(name string) string {
		switch name {
		case "CPA_API_KEY":
			return "test-key"
		case "CPA_API_BASE":
			return server.URL
		case "CPA_IMAGE_MODEL":
			return "test-model"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(result.ForLLM, "usable image output") {
		t.Fatalf("unexpected error message: %q", result.ForLLM)
	}
}

func TestGenerateImageTool_RejectsMultipleInputImages(t *testing.T) {
	workspace := t.TempDir()
	tool := NewGenerateImageTool(workspace, false)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.getenv = func(name string) string {
		switch name {
		case "CPA_API_KEY", "CPA_API_BASE", "CPA_IMAGE_MODEL":
			return "x"
		default:
			return ""
		}
	}
	first := filepath.Join(workspace, "a.png")
	second := filepath.Join(workspace, "b.png")
	if err := os.WriteFile(first, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := tool.Execute(context.Background(), map[string]any{
		"prompt":       "edit",
		"input_images": []any{"a.png", "b.png"},
	})
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(result.ForLLM, "multiple input images") {
		t.Fatalf("unexpected error message: %q", result.ForLLM)
	}
}

func TestGenerateImageTool_RejectsAbsolutePathOutsideWorkspaceWhenRestricted(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := NewGenerateImageTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())

	_, err := tool.resolveInputRef(outside)
	if err == nil {
		t.Fatal("expected absolute path outside workspace to be rejected")
	}
	if !strings.Contains(err.Error(), "outside the workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateImageTool_RejectsSymlinkEscapeWhenRestricted(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(outsideDir, "secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "escape.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	tool := NewGenerateImageTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())

	_, err := tool.resolveInputRef("escape.png")
	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if !strings.Contains(err.Error(), "symlink resolves outside workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}
