//go:build whatsapp_native

package whatsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
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

func TestWhatsAppMessageSubtypeMarksPTTAudioAsVoiceNote(t *testing.T) {
	msg := &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			Mimetype: proto.String("audio/ogg"),
			PTT:      proto.Bool(true),
		},
	}

	if got := whatsAppMessageSubtype(msg); got != bus.MessageSubtypeVoiceNote {
		t.Fatalf("voice note subtype = %q, want %q", got, bus.MessageSubtypeVoiceNote)
	}

	msg.AudioMessage.PTT = proto.Bool(false)
	if got := whatsAppMessageSubtype(msg); got != "" {
		t.Fatalf("generic audio subtype = %q, want empty", got)
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

func TestBuildWhatsAppReactionMessage_DirectChatOmitsParticipant(t *testing.T) {
	client := &whatsmeow.Client{Store: &store.Device{}}
	msg := bus.OutboundReactionMessage{
		ChatID:         "123456789@s.whatsapp.net",
		MessageID:      "msg-1",
		TargetSenderID: "123456789@s.whatsapp.net",
		Emoji:          "👍",
	}

	to, waMsg, err := buildWhatsAppReactionMessage(client, msg)
	if err != nil {
		t.Fatalf("buildWhatsAppReactionMessage() error = %v", err)
	}
	if to.String() != msg.ChatID {
		t.Fatalf("reaction chat = %q, want %q", to.String(), msg.ChatID)
	}

	reaction := waMsg.GetReactionMessage()
	if reaction == nil {
		t.Fatal("expected reaction message")
	}
	key := reaction.GetKey()
	if key == nil {
		t.Fatal("expected message key")
	}
	if key.GetRemoteJID() != msg.ChatID {
		t.Fatalf("remote JID = %q, want %q", key.GetRemoteJID(), msg.ChatID)
	}
	if key.GetID() != msg.MessageID {
		t.Fatalf("message ID = %q, want %q", key.GetID(), msg.MessageID)
	}
	if key.GetFromMe() {
		t.Fatal("expected reaction target to be treated as not from me")
	}
	if key.GetParticipant() != "" {
		t.Fatalf("direct chat participant = %q, want empty", key.GetParticipant())
	}
	if reaction.GetText() != msg.Emoji {
		t.Fatalf("reaction emoji = %q, want %q", reaction.GetText(), msg.Emoji)
	}
}

func TestBuildWhatsAppReactionMessage_GroupChatSetsParticipant(t *testing.T) {
	client := &whatsmeow.Client{Store: &store.Device{}}
	msg := bus.OutboundReactionMessage{
		ChatID:         "12345-678@g.us",
		MessageID:      "group-msg-1",
		TargetSenderID: "987654321@s.whatsapp.net",
		Emoji:          "🔥",
	}

	_, waMsg, err := buildWhatsAppReactionMessage(client, msg)
	if err != nil {
		t.Fatalf("buildWhatsAppReactionMessage() error = %v", err)
	}

	reaction := waMsg.GetReactionMessage()
	if reaction == nil || reaction.GetKey() == nil {
		t.Fatal("expected reaction key")
	}
	if reaction.GetKey().GetParticipant() != msg.TargetSenderID {
		t.Fatalf("group participant = %q, want %q", reaction.GetKey().GetParticipant(), msg.TargetSenderID)
	}
}

func TestWhatsAppNativeSendReaction_Disconnected(t *testing.T) {
	ch, err := NewWhatsAppNativeChannel(config.WhatsAppConfig{}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	channel := ch.(*WhatsAppNativeChannel)
	channel.SetRunning(true)

	err = channel.SendReaction(context.Background(), bus.OutboundReactionMessage{
		ChatID:         "123456789@s.whatsapp.net",
		MessageID:      "msg-1",
		TargetSenderID: "123456789@s.whatsapp.net",
		Emoji:          "👍",
	})
	if !errors.Is(err, channels.ErrTemporary) {
		t.Fatalf("SendReaction() error = %v, want ErrTemporary", err)
	}
	if !strings.Contains(err.Error(), "connection not established") {
		t.Fatalf("SendReaction() error = %v, want connection not established", err)
	}
}

func TestWhatsAppNativeSendReaction_Unpaired(t *testing.T) {
	ch, err := NewWhatsAppNativeChannel(config.WhatsAppConfig{}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	channel := ch.(*WhatsAppNativeChannel)
	channel.SetRunning(true)
	channel.client = &whatsmeow.Client{Store: &store.Device{}}
	channel.isConnected = func(*whatsmeow.Client) bool { return true }

	err = channel.SendReaction(context.Background(), bus.OutboundReactionMessage{
		ChatID:         "123456789@s.whatsapp.net",
		MessageID:      "msg-1",
		TargetSenderID: "123456789@s.whatsapp.net",
		Emoji:          "👍",
	})
	if !errors.Is(err, channels.ErrTemporary) {
		t.Fatalf("SendReaction() error = %v, want ErrTemporary", err)
	}
	if !strings.Contains(err.Error(), "not yet paired") {
		t.Fatalf("SendReaction() error = %v, want not yet paired", err)
	}
}
