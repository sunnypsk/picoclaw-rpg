package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/media"
)

func TestPrivateMediaCannotBeReadOrSentByFileTools(t *testing.T) {
	workspace := t.TempDir()
	private := filepath.Join(workspace, "private-media")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "index.json")
	if err := os.WriteFile(path, []byte(`{"private":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ProtectMediaDirectory(private); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { privateMediaDirectories.Delete(private) })
	for _, restrict := range []bool{false, true} {
		tool := NewReadFileTool(workspace, restrict, []*regexp.Regexp{regexp.MustCompile(".*")})
		if result := tool.Execute(context.Background(), map[string]any{"path": path}); !result.IsError {
			t.Fatal("private index readable")
		}
		list := NewListDirTool(workspace, restrict)
		if result := list.Execute(context.Background(), map[string]any{"path": private}); !result.IsError {
			t.Fatal("private directory listed")
		}
		send := NewSendFileTool(workspace, restrict)
		send.SetMediaStore(media.NewFileMediaStore())
		sent := false
		send.SetSendCallback(func(context.Context, bus.OutboundMediaMessage) error { sent = true; return nil })
		ctx := WithToolContext(context.Background(), "whatsapp_native", "other-group")
		if result := send.Execute(ctx, map[string]any{"path": path}); !result.IsError || sent {
			t.Fatal("private file sent")
		}
	}
}
