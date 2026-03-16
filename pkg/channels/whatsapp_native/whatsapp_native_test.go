//go:build whatsapp_native

package whatsapp

import (
	"testing"

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
