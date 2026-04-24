//go:build realimage

package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/media"
)

func TestGenerateImageTool_RealTextGeneration(t *testing.T) {
	if os.Getenv("CPA_API_KEY") == "" && os.Getenv("PICOCLAW_HOME") == "" {
		t.Skip("set CPA_API_KEY or PICOCLAW_HOME to run real image generation")
	}

	store := media.NewFileMediaStore()
	tool := NewGenerateImageTool(t.TempDir(), false)
	tool.SetMediaStore(store)

	started := time.Now()
	result := tool.Execute(context.Background(), map[string]any{
		"prompt":          "A minimal smoke-test image: a small red cube centered on a white background.",
		"aspect_ratio":    "1:1",
		"quality":         "low",
		"background":      "opaque",
		"timeout_seconds": float64(300),
	})
	if result.IsError {
		t.Fatalf("generate_image failed after %s: %s", time.Since(started).Round(time.Second), result.ForLLM)
	}
	if len(result.Media) == 0 {
		t.Fatalf("generate_image returned no media after %s", time.Since(started).Round(time.Second))
	}

	for _, ref := range result.Media {
		path, meta, err := store.ResolveWithMeta(ref)
		if err != nil {
			t.Fatalf("resolve generated media %s: %v", ref, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat generated media %s: %v", path, err)
		}
		t.Logf("generated media ref=%s path=%s bytes=%d content_type=%s duration=%s", ref, path, info.Size(), meta.ContentType, time.Since(started).Round(time.Second))
	}
}

func TestGenerateImageTool_RealHighQualityTextGeneration(t *testing.T) {
	if os.Getenv("CPA_API_KEY") == "" && os.Getenv("PICOCLAW_HOME") == "" {
		t.Skip("set CPA_API_KEY or PICOCLAW_HOME to run real image generation")
	}

	store := media.NewFileMediaStore()
	tool := NewGenerateImageTool(t.TempDir(), false)
	tool.SetMediaStore(store)

	started := time.Now()
	result := tool.Execute(context.Background(), map[string]any{
		"prompt":          "A premium product advertising photo of one green square pistachio cake on a clean beige studio background, realistic commercial photography, sharp texture, elegant softbox lighting.",
		"aspect_ratio":    "4:5",
		"quality":         "high",
		"background":      "opaque",
		"timeout_seconds": float64(300),
	})
	if result.IsError {
		t.Fatalf("generate_image failed after %s: %s", time.Since(started).Round(time.Second), result.ForLLM)
	}
	if len(result.Media) == 0 {
		t.Fatalf("generate_image returned no media after %s", time.Since(started).Round(time.Second))
	}

	for _, ref := range result.Media {
		path, meta, err := store.ResolveWithMeta(ref)
		if err != nil {
			t.Fatalf("resolve generated media %s: %v", ref, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat generated media %s: %v", path, err)
		}
		t.Logf("generated media ref=%s path=%s bytes=%d content_type=%s duration=%s", ref, path, info.Size(), meta.ContentType, time.Since(started).Round(time.Second))
	}
}
