package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
		if len(content) != 3 {
			t.Fatalf("unexpected content length: %d", len(content))
		}
		if content[1].(map[string]any)["text"] != "aspect_ratio: 16:9" {
			t.Fatalf("missing aspect ratio part: %#v", content[1])
		}
		imagePart := content[2].(map[string]any)
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
		"aspect_ratio": "16:9",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected one media ref, got %v", result.Media)
	}
}

func TestGenerateImageTool_UsesImagesGenerationEndpointForImageAPIModels(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["model"] != "gpt-image-2" {
			t.Fatalf("unexpected model: %#v", payload["model"])
		}
		if payload["size"] != "1536x864" {
			t.Fatalf("unexpected inferred size: %#v", payload["size"])
		}
		prompt, _ := payload["prompt"].(string)
		if !strings.Contains(prompt, "Target aspect ratio: 16:9") {
			t.Fatalf("expected prompt aspect ratio hint, got %q", prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"b64_json": base64.StdEncoding.EncodeToString([]byte("image-api-image")),
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
			return "gpt-image-2"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{
		"prompt":       "cat",
		"aspect_ratio": "16:9",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected one media ref, got %v", result.Media)
	}
}

func TestGenerateImageTool_SendsDefaultUserAgent(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("User-Agent"); got != defaultImageUserAgent {
			t.Fatalf("unexpected user-agent: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"b64_json": base64.StdEncoding.EncodeToString([]byte("image-api-image")),
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
			return "gpt-image-2"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}

func TestGenerateImageTool_RetriesHighQualityImageAPIAsLowOnProviderTimeouts(t *testing.T) {
	timeoutStatuses := []int{http.StatusRequestTimeout, http.StatusGatewayTimeout, 524}
	for _, status := range timeoutStatuses {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			workspace := t.TempDir()
			store := media.NewFileMediaStore()
			var qualities []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/images/generations" {
					http.NotFound(w, r)
					return
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode payload: %v", err)
				}
				quality, _ := payload["quality"].(string)
				qualities = append(qualities, quality)
				if len(qualities) == 1 {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":"timeout"}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{{
						"b64_json": base64.StdEncoding.EncodeToString([]byte("retried-image")),
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
					return "gpt-image-2"
				default:
					return ""
				}
			}

			result := tool.Execute(context.Background(), map[string]any{
				"prompt":  "cat",
				"quality": "high",
			})
			if result.IsError {
				t.Fatalf("unexpected error: %s", result.ForLLM)
			}
			if len(qualities) != 2 {
				t.Fatalf("expected two attempts, got %d", len(qualities))
			}
			if qualities[0] != "high" || qualities[1] != "low" {
				t.Fatalf("unexpected quality attempts: %#v", qualities)
			}
		})
	}
}

func TestGenerateImageTool_RetriesDefaultQualityImageAPIAsLowOn524(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	var qualities []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		quality, _ := payload["quality"].(string)
		qualities = append(qualities, quality)
		if len(qualities) == 1 {
			w.WriteHeader(524)
			_, _ = w.Write([]byte(`{"error":"timeout"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"b64_json": base64.StdEncoding.EncodeToString([]byte("retried-image")),
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
			return "gpt-image-2"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(qualities) != 2 {
		t.Fatalf("expected two attempts, got %d", len(qualities))
	}
	if qualities[0] != "" || qualities[1] != "low" {
		t.Fatalf("unexpected quality attempts: %#v", qualities)
	}
}

func TestGenerateImageTool_DoesNotRetryLowQualitySquareImageAPIOn524(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			http.NotFound(w, r)
			return
		}
		attempts++
		w.WriteHeader(524)
		_, _ = w.Write([]byte(`{"error":"timeout"}`))
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
			return "gpt-image-2"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{
		"prompt":       "red cube",
		"aspect_ratio": "1:1",
		"quality":      "low",
	})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected one attempt, got %d", attempts)
	}
}

func TestGenerateImageTool_UsesImagesEditEndpointForImageAPIModels(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	sourceImage := filepath.Join(workspace, "source.png")
	if err := os.WriteFile(sourceImage, []byte("input-image"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if got := r.FormValue("model"); got != "gpt-image-2" {
			t.Fatalf("unexpected model: %q", got)
		}
		if got := r.FormValue("size"); got != "1024x1024" {
			t.Fatalf("unexpected size: %q", got)
		}
		if got := r.FormValue("prompt"); !strings.Contains(got, "Target aspect ratio: 1:1") {
			t.Fatalf("unexpected prompt: %q", got)
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) != 1 {
			t.Fatalf("expected one multipart image, got %d", len(files))
		}
		handle, err := files[0].Open()
		if err != nil {
			t.Fatalf("open multipart image: %v", err)
		}
		defer handle.Close()
		data, err := io.ReadAll(handle)
		if err != nil {
			t.Fatalf("read multipart image: %v", err)
		}
		if string(data) != "input-image" {
			t.Fatalf("unexpected multipart image bytes: %q", string(data))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"b64_json": base64.StdEncoding.EncodeToString([]byte("edited-image")),
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
			return "gpt-image-2"
		default:
			return ""
		}
	}

	result := tool.Execute(context.Background(), map[string]any{
		"prompt":       "edit this",
		"image":        sourceImage,
		"aspect_ratio": "1:1",
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

func TestGenerateImageTool_DedupesDuplicateInputAliases(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	tool := NewGenerateImageTool(workspace, false)
	tool.SetMediaStore(store)

	imagePath := filepath.Join(workspace, "photo.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(imagePath, media.MediaMeta{
		Filename:    "photo.png",
		ContentType: "image/png",
	}, "scope")
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := tool.resolveInputImages(context.Background(), map[string]any{
		"image":        ref,
		"input_image":  ref,
		"input_images": []any{ref},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 || resolved[0] != imagePath {
		t.Fatalf("resolved inputs = %v, want [%q]", resolved, imagePath)
	}
}

func TestGenerateImageTool_ResolvesMediaCurrentForSingleCurrentImage(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	tool := NewGenerateImageTool(workspace, false)
	tool.SetMediaStore(store)

	imagePath := filepath.Join(workspace, "photo.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(imagePath, media.MediaMeta{
		Filename:    "photo.png",
		ContentType: "image/png",
	}, "scope")
	if err != nil {
		t.Fatal(err)
	}

	ctx := WithToolExecutionContext(context.Background(), "whatsapp_native", "chat-1", "msg-1", "user-1", "session-1", []string{ref})
	resolved, err := tool.resolveInputImages(ctx, map[string]any{"image": "media://current"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 || resolved[0] != imagePath {
		t.Fatalf("resolved inputs = %v, want [%q]", resolved, imagePath)
	}
}

func TestGenerateImageTool_AllowsMediaRefOutsideWorkspaceWhenRestricted(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "upload.png")
	if err := os.WriteFile(outside, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := media.NewFileMediaStore()
	ref, err := store.Store(outside, media.MediaMeta{
		Filename:    "upload.png",
		ContentType: "image/png",
	}, "scope")
	if err != nil {
		t.Fatal(err)
	}

	tool := NewGenerateImageTool(workspace, true)
	tool.SetMediaStore(store)

	resolved, err := tool.resolveInputRef(ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != filepath.Clean(outside) {
		t.Fatalf("resolveInputRef(%q) = %q, want %q", ref, resolved, filepath.Clean(outside))
	}
}

func TestGenerateImageTool_RejectsMediaCurrentWithoutCurrentImage(t *testing.T) {
	tool := NewGenerateImageTool(t.TempDir(), false)
	tool.SetMediaStore(media.NewFileMediaStore())

	_, err := tool.resolveInputImages(context.Background(), map[string]any{"image": "media://current"})
	if err == nil {
		t.Fatal("expected error for missing current image")
	}
	if !strings.Contains(err.Error(), "no current inbound image") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateImageTool_RejectsMediaCurrentWhenMultipleCurrentImagesExist(t *testing.T) {
	workspace := t.TempDir()
	store := media.NewFileMediaStore()
	tool := NewGenerateImageTool(workspace, false)
	tool.SetMediaStore(store)

	first := filepath.Join(workspace, "a.png")
	second := filepath.Join(workspace, "b.png")
	if err := os.WriteFile(first, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	refA, err := store.Store(first, media.MediaMeta{Filename: "a.png", ContentType: "image/png"}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	refB, err := store.Store(second, media.MediaMeta{Filename: "b.png", ContentType: "image/png"}, "scope")
	if err != nil {
		t.Fatal(err)
	}

	ctx := WithToolExecutionContext(context.Background(), "whatsapp_native", "chat-1", "msg-1", "user-1", "session-1", []string{refA, refB})
	_, err = tool.resolveInputImages(ctx, map[string]any{"image": "media://current"})
	if err == nil {
		t.Fatal("expected error for multiple current images")
	}
	if !strings.Contains(err.Error(), "multiple current inbound images") {
		t.Fatalf("unexpected error: %v", err)
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
