//go:build whatsapp_native

package whatsapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestCollectIncomingMediaAttachmentsIncludesAudioDocumentAndVideo(t *testing.T) {
	msg := &waE2E.Message{
		AudioMessage:    &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg")},
		DocumentMessage: &waE2E.DocumentMessage{Mimetype: proto.String("application/pdf")},
		VideoMessage:    &waE2E.VideoMessage{Mimetype: proto.String("video/mp4")},
	}

	attachments := collectIncomingMediaAttachments(msg)
	if len(attachments) != 3 {
		t.Fatalf("expected 3 attachments, got %d", len(attachments))
	}

	if attachments[0].kind != "audio" || attachments[0].mimeType != "audio/ogg" {
		t.Fatalf("expected audio attachment first, got kind=%q mime=%q", attachments[0].kind, attachments[0].mimeType)
	}

	if attachments[1].kind != "video" || attachments[1].mimeType != "video/mp4" {
		t.Fatalf("expected video attachment second, got kind=%q mime=%q", attachments[1].kind, attachments[1].mimeType)
	}

	if attachments[2].kind != "file" || attachments[2].mimeType != "application/pdf" {
		t.Fatalf("expected document attachment third, got kind=%q mime=%q", attachments[2].kind, attachments[2].mimeType)
	}

	if attachments[0].prefix != "wa-audio" || attachments[1].prefix != "wa-video" || attachments[2].prefix != "wa-document" {
		t.Fatalf("unexpected prefixes: %#v", []string{attachments[0].prefix, attachments[1].prefix, attachments[2].prefix})
	}
}

func TestStoreIncomingMedia_ReplacesBinWithContentTypeExtension(t *testing.T) {
	ch, err := NewWhatsAppNativeChannel(config.WhatsAppConfig{}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	channel := ch.(*WhatsAppNativeChannel)
	store := media.NewFileMediaStore()
	channel.SetMediaStore(store)

	path := filepath.Join(t.TempDir(), "wa-audio.bin")
	if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	ref := channel.storeIncomingMedia(path, "audio/ogg; codecs=opus", "audio", "msg-1", "scope-1")
	if ref == "" {
		t.Fatal("expected media ref")
	}

	_, meta, err := store.ResolveWithMeta(ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Ext(meta.Filename); got != ".ogg" {
		t.Fatalf("stored filename extension = %q, want .ogg (filename=%q)", got, meta.Filename)
	}
}
