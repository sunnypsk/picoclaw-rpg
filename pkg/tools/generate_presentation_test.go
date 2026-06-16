package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/media"
)

func TestGeneratePresentationTool_GeneratesOfflineZipPackage(t *testing.T) {
	workspace := t.TempDir()
	tool := NewGeneratePresentationTool(workspace, true)
	tool.now = func() time.Time { return time.Date(2026, 6, 16, 10, 30, 0, 0, time.UTC) }
	store := media.NewFileMediaStore()
	tool.SetMediaStore(store)

	result := tool.Execute(
		WithToolContext(context.Background(), "telegram", "chat-1"),
		map[string]any{
			"title": "Q3 Strategy Review",
			"slides": []any{
				map[string]any{
					"layout":   "cover",
					"title":    "Q3 Strategy Review",
					"subtitle": "A focused plan for the next quarter",
				},
				map[string]any{
					"layout": "title-bullets",
					"title":  "Priorities",
					"bullets": []any{
						"Improve activation",
						"Shorten response time",
						"Make reporting easier to understand",
					},
				},
			},
		},
	)

	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected ZIP media ref, got %#v", result.Media)
	}

	deckDir := filepath.Join(workspace, "generated-presentations", "q3-strategy-review-20260616-103000")
	indexPath := filepath.Join(deckDir, "index.html")
	zipPath := filepath.Join(deckDir, "q3-strategy-review.zip")
	assertFileExists(t, indexPath)
	assertFileExists(t, filepath.Join(deckDir, "deck.json"))
	assertFileExists(t, filepath.Join(deckDir, "README.txt"))
	assertFileExists(t, filepath.Join(deckDir, "assets", "anime.umd.min.js"))
	assertFileExists(t, filepath.Join(deckDir, "assets", "anime.LICENSE.md"))
	assertFileExists(t, zipPath)

	html := readTestFile(t, indexPath)
	for _, forbidden := range []string{"https://", "http://", "fetch(", `type="module"`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("generated HTML contains forbidden dependency %q", forbidden)
		}
	}
	for _, want := range []string{
		"assets/anime.umd.min.js",
		"window.anime.animate",
		"prefers-reduced-motion",
		"ArrowRight",
		"Q3 Strategy Review",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("generated HTML missing %q", want)
		}
	}

	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open ZIP: %v", err)
	}
	defer zipReader.Close()
	names := make(map[string]bool)
	for _, file := range zipReader.File {
		names[file.Name] = true
		if strings.HasSuffix(file.Name, ".zip") {
			t.Fatalf("ZIP should not include itself, got %s", file.Name)
		}
	}
	for _, want := range []string{
		"q3-strategy-review-20260616-103000/index.html",
		"q3-strategy-review-20260616-103000/assets/anime.umd.min.js",
		"q3-strategy-review-20260616-103000/deck.json",
	} {
		if !names[want] {
			t.Fatalf("ZIP missing %s; entries=%v", want, names)
		}
	}
}

func TestGeneratePresentationTool_UsesUniqueDirectoryForSameSecondOutputs(t *testing.T) {
	workspace := t.TempDir()
	tool := NewGeneratePresentationTool(workspace, true)
	tool.now = func() time.Time { return time.Date(2026, 6, 16, 10, 30, 0, 0, time.UTC) }
	args := map[string]any{
		"title":  "Repeat Deck",
		"output": "folder",
		"slides": []any{
			map[string]any{
				"layout":   "cover",
				"title":    "Repeat Deck",
				"subtitle": "The same deck generated twice in one second",
			},
		},
	}

	first := tool.Execute(context.Background(), args)
	if first.IsError {
		t.Fatalf("first Execute() returned error: %s", first.ForLLM)
	}
	firstFolder := decodeToolResultString(t, first, "folder")
	assertFileExists(t, filepath.Join(firstFolder, "index.html"))
	if filepath.Base(firstFolder) != "repeat-deck-20260616-103000" {
		t.Fatalf("first folder should keep the stable base name, got %s", firstFolder)
	}
	if err := os.WriteFile(filepath.Join(firstFolder, "assets", "images", "stale.png"), testPNGBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}

	second := tool.Execute(context.Background(), args)
	if second.IsError {
		t.Fatalf("second Execute() returned error: %s", second.ForLLM)
	}
	secondFolder := decodeToolResultString(t, second, "folder")
	assertFileExists(t, filepath.Join(secondFolder, "index.html"))
	if secondFolder == firstFolder {
		t.Fatalf("expected second output to use a unique folder, got %s", secondFolder)
	}
	if !strings.HasPrefix(filepath.Base(secondFolder), "repeat-deck-20260616-103000-") {
		t.Fatalf("second folder should use the same base with a uniqueness suffix, got %s", secondFolder)
	}
	if _, err := os.Stat(filepath.Join(secondFolder, "assets", "images", "stale.png")); err == nil {
		t.Fatalf("second output reused stale image assets from first output")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat stale asset in second output: %v", err)
	}
}

func TestGeneratePresentationTool_CopiesMediaImageIntoPackage(t *testing.T) {
	workspace := t.TempDir()
	tool := NewGeneratePresentationTool(workspace, true)
	tool.now = func() time.Time { return time.Date(2026, 6, 16, 11, 0, 0, 0, time.UTC) }
	store := media.NewFileMediaStore()
	tool.SetMediaStore(store)

	imagePath := filepath.Join(t.TempDir(), "hero.png")
	if err := os.WriteFile(imagePath, testPNGBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(imagePath, media.MediaMeta{
		Filename:    "hero.png",
		ContentType: "image/png",
		Source:      "test",
		Owned:       false,
	}, "test")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", "session-1", []string{ref})
	result := tool.Execute(ctx, map[string]any{
		"title":  "Visual Brief",
		"output": "folder",
		"slides": []any{
			map[string]any{
				"layout": "image-hero",
				"title":  "The product becomes visible",
				"body":   "A simple visual makes the story easier to follow.",
				"image": map[string]any{
					"src": "media://current",
					"alt": "Product hero visual",
				},
			},
		},
	})
	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}

	deckDir := filepath.Join(workspace, "generated-presentations", "visual-brief-20260616-110000")
	imageCopy := filepath.Join(deckDir, "assets", "images", "slide-01-hero.png")
	assertFileExists(t, imageCopy)
	html := readTestFile(t, filepath.Join(deckDir, "index.html"))
	if !strings.Contains(html, "assets/images/slide-01-hero.png") {
		t.Fatalf("generated HTML does not reference copied image")
	}
	if strings.Contains(html, ref) {
		t.Fatalf("generated HTML should not expose media ref")
	}
	deckJSON := readTestFile(t, filepath.Join(deckDir, "deck.json"))
	if !strings.Contains(deckJSON, `"src": "assets/images/slide-01-hero.png"`) {
		t.Fatalf("deck metadata does not reference copied image: %s", deckJSON)
	}
	if strings.Contains(deckJSON, ref) {
		t.Fatalf("deck metadata should not expose media ref")
	}
}

func TestGeneratePresentationTool_RendersClassroomThemeAndMotion(t *testing.T) {
	workspace := t.TempDir()
	tool := NewGeneratePresentationTool(workspace, true)
	tool.now = func() time.Time { return time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC) }

	result := tool.Execute(context.Background(), map[string]any{
		"title":  "Lesson Deck",
		"theme":  "classroom",
		"output": "folder",
		"slides": []any{
			map[string]any{
				"layout":    "cover",
				"title":     "Lesson Deck",
				"subtitle":  "A clear classroom introduction",
				"animation": "spotlight",
			},
			map[string]any{
				"layout":    "timeline",
				"title":     "Three steps",
				"animation": "draw-line",
				"items": []any{
					map[string]any{"label": "01", "title": "Start", "body": "Set the goal."},
					map[string]any{"label": "02", "title": "Practice", "body": "Try the move."},
					map[string]any{"label": "03", "title": "Reflect", "body": "Name what improved."},
				},
			},
		},
	})
	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}

	html := readTestFile(t, filepath.Join(
		workspace,
		"generated-presentations",
		"lesson-deck-20260616-120000",
		"index.html",
	))
	for _, want := range []string{
		"theme-classroom",
		`data-animation="spotlight"`,
		`class="timeline-rule"`,
		"animateSlideShell",
		"animateSlideDetails",
		"scaleX",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("generated HTML missing %q", want)
		}
	}
}

func TestGeneratePresentationTool_AnimationNoneSkipsShellAndDetailMotion(t *testing.T) {
	workspace := t.TempDir()
	tool := NewGeneratePresentationTool(workspace, true)
	tool.now = func() time.Time { return time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC) }

	result := tool.Execute(context.Background(), map[string]any{
		"title":  "Still Deck",
		"output": "folder",
		"slides": []any{
			map[string]any{
				"layout":    "timeline",
				"title":     "No motion steps",
				"animation": "none",
				"items": []any{
					map[string]any{"label": "01", "title": "Plan", "body": "Set the target."},
					map[string]any{"label": "02", "title": "Build", "body": "Make the change."},
					map[string]any{"label": "03", "title": "Check", "body": "Verify the result."},
				},
			},
		},
	})
	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}

	html := readTestFile(t, filepath.Join(
		workspace,
		"generated-presentations",
		"still-deck-20260616-123000",
		"index.html",
	))
	for _, want := range []string{
		`data-animation="none"`,
		"const preset = slide.dataset.animation || 'auto';",
		"if (preset !== 'none')",
		"animateSlideDetails(slide, preset)",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("generated HTML missing %q", want)
		}
	}
	if strings.Contains(html, "animateSlideDetails(slide, slide.dataset.animation || 'auto')") {
		t.Fatalf("generated HTML still calls detail animation with the raw slide preset")
	}
}

func TestGeneratePresentationTool_ReportsQualityWarnings(t *testing.T) {
	tool := NewGeneratePresentationTool(t.TempDir(), true)
	result := tool.Execute(context.Background(), map[string]any{
		"title": "Dense Deck",
		"slides": []any{
			map[string]any{
				"layout": "title-bullets",
				"title":  "This title is intentionally much too long for a polished presentation slide",
				"body":   "This body copy is also intentionally long enough to be a warning because it asks the slide to carry too much explanatory detail in one view, then keeps going with extra detail that should be moved into speaker notes or a separate slide.",
				"bullets": []any{
					"First long bullet that should be shortened before presenting to an audience",
					"Second long bullet that repeats the same problem and adds density",
					"Third bullet",
					"Fourth bullet",
					"Fifth bullet",
					"Sixth bullet",
				},
			},
		},
	})
	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}
	for _, want := range []string{
		"quality_warnings",
		"slide 1 title",
		"slide 1 body",
		"has 6 bullets",
	} {
		if !strings.Contains(result.ForLLM, want) {
			t.Fatalf("expected warning %q in ForLLM, got %s", want, result.ForLLM)
		}
	}
}

func TestGeneratePresentationTool_RejectsRemoteImages(t *testing.T) {
	tool := NewGeneratePresentationTool(t.TempDir(), true)
	result := tool.Execute(context.Background(), map[string]any{
		"title": "Remote Image",
		"slides": []any{
			map[string]any{
				"layout": "image-hero",
				"title":  "Remote",
				"image":  map[string]any{"src": "https://example.com/image.png"},
			},
		},
	})
	if !result.IsError || !strings.Contains(result.ForLLM, "remote image URLs") {
		t.Fatalf("expected remote image error, got %#v", result)
	}
}

func TestGeneratePresentationTool_RejectsUnsafeImagePath(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, testPNGBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewGeneratePresentationTool(workspace, false)
	result := tool.Execute(context.Background(), map[string]any{
		"title": "Unsafe Image",
		"slides": []any{
			map[string]any{
				"layout": "image-hero",
				"title":  "Outside",
				"image":  map[string]any{"src": outside},
			},
		},
	})
	if !result.IsError || !strings.Contains(result.ForLLM, "outside the workspace") {
		t.Fatalf("expected workspace restriction error, got %#v", result)
	}
}

func TestGeneratePresentationTool_ValidatesSlideLimitAndRequiredContent(t *testing.T) {
	tool := NewGeneratePresentationTool(t.TempDir(), true)

	slides := make([]any, maxPresentationSlides+1)
	for i := range slides {
		slides[i] = map[string]any{"layout": "cover", "title": "Slide"}
	}
	result := tool.Execute(context.Background(), map[string]any{"title": "Too Many", "slides": slides})
	if !result.IsError || !strings.Contains(result.ForLLM, "v1 limit") {
		t.Fatalf("expected slide limit error, got %#v", result)
	}

	result = tool.Execute(context.Background(), map[string]any{
		"title":  "Missing Bullets",
		"slides": []any{map[string]any{"layout": "title-bullets", "title": "No bullets"}},
	})
	if !result.IsError || !strings.Contains(result.ForLLM, "requires title and bullets") {
		t.Fatalf("expected content validation error, got %#v", result)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func decodeToolResultString(t *testing.T, result *ToolResult, key string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("decode ForLLM: %v", err)
	}
	value, ok := payload[key].(string)
	if !ok || value == "" {
		t.Fatalf("ForLLM missing string field %q: %s", key, result.ForLLM)
	}
	return value
}
