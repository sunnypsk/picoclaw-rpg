package tools

import (
	"archive/zip"
	"context"
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
