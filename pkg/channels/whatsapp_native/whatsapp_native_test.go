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

func TestBuildWhatsAppMediaMessage_Image(t *testing.T) {
	uploadResp := whatsmeow.UploadResponse{
		URL:        "https://example.com/image",
		DirectPath: "/image",
		MediaKey:   []byte("media-key"),
		FileLength: 123,
	}
	part := resolvedWhatsAppMediaPart{
		mediaType:   "image",
		contentType: "image/png",
		caption:     "hello",
	}

	uploadType, waMsg := buildWhatsAppMediaMessage(part, uploadResp, nil)
	if uploadType != whatsmeow.MediaImage {
		t.Fatalf("uploadType = %v, want MediaImage", uploadType)
	}
	if waMsg.GetImageMessage() == nil {
		t.Fatal("expected image message")
	}
	if waMsg.GetImageMessage().GetCaption() != "hello" {
		t.Fatalf("caption = %q, want hello", waMsg.GetImageMessage().GetCaption())
	}
	if waMsg.GetImageMessage().GetMimetype() != "image/png" {
		t.Fatalf("mimetype = %q, want image/png", waMsg.GetImageMessage().GetMimetype())
	}
}

func TestBuildWhatsAppMediaMessage_Audio(t *testing.T) {
	uploadResp := whatsmeow.UploadResponse{
		URL:        "https://example.com/audio",
		DirectPath: "/audio",
		MediaKey:   []byte("media-key"),
		FileLength: 456,
	}
	part := resolvedWhatsAppMediaPart{
		mediaType:   "audio",
		contentType: "audio/ogg",
		caption:     "ignored",
	}

	uploadType, waMsg := buildWhatsAppMediaMessage(part, uploadResp, nil)
	if uploadType != whatsmeow.MediaAudio {
		t.Fatalf("uploadType = %v, want MediaAudio", uploadType)
	}
	if waMsg.GetAudioMessage() == nil {
		t.Fatal("expected audio message")
	}
	if waMsg.GetAudioMessage().GetMimetype() != "audio/ogg" {
		t.Fatalf("mimetype = %q, want audio/ogg", waMsg.GetAudioMessage().GetMimetype())
	}
	if waMsg.GetAudioMessage().GetPTT() {
		t.Fatal("expected outbound audio to have PTT=false")
	}
}

func TestBuildWhatsAppMediaMessage_Video(t *testing.T) {
	uploadResp := whatsmeow.UploadResponse{
		URL:        "https://example.com/video",
		DirectPath: "/video",
		MediaKey:   []byte("media-key"),
		FileLength: 789,
	}
	part := resolvedWhatsAppMediaPart{
		mediaType:   "video",
		contentType: "video/mp4",
		caption:     "clip",
	}

	uploadType, waMsg := buildWhatsAppMediaMessage(part, uploadResp, nil)
	if uploadType != whatsmeow.MediaVideo {
		t.Fatalf("uploadType = %v, want MediaVideo", uploadType)
	}
	if waMsg.GetVideoMessage() == nil {
		t.Fatal("expected video message")
	}
	if waMsg.GetVideoMessage().GetCaption() != "clip" {
		t.Fatalf("caption = %q, want clip", waMsg.GetVideoMessage().GetCaption())
	}
}

func TestBuildWhatsAppMediaMessage_Document(t *testing.T) {
	uploadResp := whatsmeow.UploadResponse{
		URL:        "https://example.com/document",
		DirectPath: "/document",
		MediaKey:   []byte("media-key"),
		FileLength: 321,
	}
	part := resolvedWhatsAppMediaPart{
		mediaType:   "file",
		contentType: "application/pdf",
		filename:    "report.pdf",
		caption:     "report",
	}

	uploadType, waMsg := buildWhatsAppMediaMessage(part, uploadResp, nil)
	if uploadType != whatsmeow.MediaDocument {
		t.Fatalf("uploadType = %v, want MediaDocument", uploadType)
	}
	if waMsg.GetDocumentMessage() == nil {
		t.Fatal("expected document message")
	}
	if waMsg.GetDocumentMessage().GetFileName() != "report.pdf" {
		t.Fatalf("filename = %q, want report.pdf", waMsg.GetDocumentMessage().GetFileName())
	}
	if waMsg.GetDocumentMessage().GetCaption() != "report" {
		t.Fatalf("caption = %q, want report", waMsg.GetDocumentMessage().GetCaption())
	}
}

func TestResolveWhatsAppMediaPart_StickerUsesWebP(t *testing.T) {
	part := bus.MediaPart{Type: "sticker"}
	meta := media.MediaMeta{
		Filename: "sticker.webp",
	}

	resolved := resolveWhatsAppMediaPart(part, filepath.Join(t.TempDir(), "ignored.bin"), meta)
	if resolved.mediaType != "sticker" {
		t.Fatalf("mediaType = %q, want sticker", resolved.mediaType)
	}
	if resolved.contentType != "image/webp" {
		t.Fatalf("contentType = %q, want image/webp", resolved.contentType)
	}
}

func TestBuildWhatsAppMediaMessage_Sticker(t *testing.T) {
	uploadResp := whatsmeow.UploadResponse{
		URL:        "https://example.com/sticker",
		DirectPath: "/sticker",
		MediaKey:   []byte("media-key"),
		FileLength: 111,
	}
	part := resolvedWhatsAppMediaPart{
		mediaType:   "sticker",
		contentType: "image/webp",
	}

	uploadType, waMsg := buildWhatsAppMediaMessage(part, uploadResp, nil)
	if uploadType != whatsmeow.MediaImage {
		t.Fatalf("uploadType = %v, want MediaImage", uploadType)
	}
	sticker := waMsg.GetStickerMessage()
	if sticker == nil {
		t.Fatal("expected sticker message")
	}
	if sticker.GetMimetype() != "image/webp" {
		t.Fatalf("mimetype = %q, want image/webp", sticker.GetMimetype())
	}
	if sticker.GetIsAnimated() {
		t.Fatal("expected static sticker to have IsAnimated=false")
	}
}

func TestBuildWhatsAppReplyContext_DirectChatOmitsParticipant(t *testing.T) {
	chatJID, err := parseJID("123456789@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}

	contextInfo := buildWhatsAppReplyContext(chatJID, "wamid-1", "123456789@s.whatsapp.net")
	if contextInfo == nil {
		t.Fatal("expected reply context")
	}
	if contextInfo.GetStanzaID() != "wamid-1" {
		t.Fatalf("stanza ID = %q, want %q", contextInfo.GetStanzaID(), "wamid-1")
	}
	if contextInfo.GetRemoteJID() != "123456789@s.whatsapp.net" {
		t.Fatalf("remote JID = %q, want %q", contextInfo.GetRemoteJID(), "123456789@s.whatsapp.net")
	}
	if contextInfo.GetParticipant() != "" {
		t.Fatalf("participant = %q, want empty", contextInfo.GetParticipant())
	}
}

func TestBuildWhatsAppReplyContext_GroupChatSetsParticipant(t *testing.T) {
	chatJID, err := parseJID("12345-678@g.us")
	if err != nil {
		t.Fatal(err)
	}

	contextInfo := buildWhatsAppReplyContext(chatJID, "wamid-2", "whatsapp:987654321@s.whatsapp.net")
	if contextInfo == nil {
		t.Fatal("expected reply context")
	}
	if contextInfo.GetParticipant() != "987654321@s.whatsapp.net" {
		t.Fatalf("participant = %q, want %q", contextInfo.GetParticipant(), "987654321@s.whatsapp.net")
	}
}

func TestBuildWhatsAppTextMessage_ReplyUsesExtendedTextMessage(t *testing.T) {
	to, err := parseJID("12345-678@g.us")
	if err != nil {
		t.Fatal(err)
	}

	waMsg := buildWhatsAppTextMessage(bus.OutboundMessage{
		ChatID:           "12345-678@g.us",
		Content:          "hello there",
		ReplyToMessageID: "wamid-3",
		ReplyToSenderID:  "555123456@s.whatsapp.net",
	}, to)

	if waMsg.GetExtendedTextMessage() == nil {
		t.Fatal("expected extended text message")
	}
	if waMsg.GetExtendedTextMessage().GetText() != "hello there" {
		t.Fatalf("text = %q, want %q", waMsg.GetExtendedTextMessage().GetText(), "hello there")
	}
	if waMsg.GetExtendedTextMessage().GetContextInfo().GetStanzaID() != "wamid-3" {
		t.Fatalf("stanza ID = %q, want %q", waMsg.GetExtendedTextMessage().GetContextInfo().GetStanzaID(), "wamid-3")
	}
	if waMsg.GetConversation() != "" {
		t.Fatalf("conversation = %q, want empty", waMsg.GetConversation())
	}
}

func TestBuildWhatsAppMediaMessage_AttachesReplyContext(t *testing.T) {
	uploadResp := whatsmeow.UploadResponse{
		URL:        "https://example.com/image",
		DirectPath: "/image",
		MediaKey:   []byte("media-key"),
		FileLength: 123,
	}
	part := resolvedWhatsAppMediaPart{
		mediaType:   "image",
		contentType: "image/png",
	}
	contextInfo := &waE2E.ContextInfo{
		StanzaID:    proto.String("wamid-4"),
		RemoteJID:   proto.String("12345-678@g.us"),
		Participant: proto.String("555123456@s.whatsapp.net"),
	}

	_, waMsg := buildWhatsAppMediaMessage(part, uploadResp, contextInfo)
	if waMsg.GetImageMessage() == nil {
		t.Fatal("expected image message")
	}
	if waMsg.GetImageMessage().GetContextInfo().GetStanzaID() != "wamid-4" {
		t.Fatalf("stanza ID = %q, want %q", waMsg.GetImageMessage().GetContextInfo().GetStanzaID(), "wamid-4")
	}
}

func TestExtractWhatsAppReplyMetadata_IncludesQuotedText(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("current reply"),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String("wamid-5"),
				Participant: proto.String("555123456@s.whatsapp.net"),
				QuotedMessage: &waE2E.Message{
					ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text: proto.String("quoted hello"),
					},
				},
			},
		},
	}

	metadata := extractWhatsAppReplyMetadata(msg)
	if metadata["reply_to_message_id"] != "wamid-5" {
		t.Fatalf("reply_to_message_id = %q, want %q", metadata["reply_to_message_id"], "wamid-5")
	}
	if metadata["reply_to_sender_id"] != "555123456@s.whatsapp.net" {
		t.Fatalf("reply_to_sender_id = %q, want %q", metadata["reply_to_sender_id"], "555123456@s.whatsapp.net")
	}
	if metadata["reply_to_text"] != "quoted hello" {
		t.Fatalf("reply_to_text = %q, want %q", metadata["reply_to_text"], "quoted hello")
	}
}

func TestExtractWhatsAppReplyMetadata_UsesMediaPlaceholderFallback(t *testing.T) {
	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			ContextInfo: &waE2E.ContextInfo{
				StanzaID: proto.String("wamid-6"),
				QuotedMessage: &waE2E.Message{
					VideoMessage: &waE2E.VideoMessage{},
				},
			},
		},
	}

	metadata := extractWhatsAppReplyMetadata(msg)
	if metadata["reply_to_text"] != "[Video]" {
		t.Fatalf("reply_to_text = %q, want %q", metadata["reply_to_text"], "[Video]")
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

func TestBuildWhatsAppReactionMessage_AcceptsCanonicalLIDSenderID(t *testing.T) {
	client := &whatsmeow.Client{Store: &store.Device{}}
	msg := bus.OutboundReactionMessage{
		ChatID:         "130184887930990@lid",
		MessageID:      "3EB04F67AAC3BC8A2D38D7",
		TargetSenderID: "whatsapp:130184887930990:59@lid",
		Emoji:          "👍",
	}

	to, waMsg, err := buildWhatsAppReactionMessage(client, msg)
	if err != nil {
		t.Fatalf("buildWhatsAppReactionMessage() error = %v", err)
	}
	if to.String() != msg.ChatID {
		t.Fatalf("reaction chat = %q, want %q", to.String(), msg.ChatID)
	}
	if waMsg.GetReactionMessage() == nil {
		t.Fatal("expected reaction message")
	}
	if waMsg.GetReactionMessage().GetKey() == nil {
		t.Fatal("expected reaction key")
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

func TestWhatsAppNativeSendMedia_Disconnected(t *testing.T) {
	ch, err := NewWhatsAppNativeChannel(config.WhatsAppConfig{}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	channel := ch.(*WhatsAppNativeChannel)
	channel.SetRunning(true)
	channel.SetMediaStore(media.NewFileMediaStore())

	err = channel.SendMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "123456789@s.whatsapp.net",
		Parts: []bus.MediaPart{{
			Type: "file",
			Ref:  "media://missing",
		}},
	})
	if !errors.Is(err, channels.ErrTemporary) {
		t.Fatalf("SendMedia() error = %v, want ErrTemporary", err)
	}
	if !strings.Contains(err.Error(), "connection not established") {
		t.Fatalf("SendMedia() error = %v, want connection not established", err)
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

func TestWhatsAppNativeSendMedia_Unpaired(t *testing.T) {
	ch, err := NewWhatsAppNativeChannel(config.WhatsAppConfig{}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	channel := ch.(*WhatsAppNativeChannel)
	channel.SetRunning(true)
	channel.client = &whatsmeow.Client{Store: &store.Device{}}
	channel.isConnected = func(*whatsmeow.Client) bool { return true }
	store := media.NewFileMediaStore()
	channel.SetMediaStore(store)

	path := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(path, media.MediaMeta{
		Filename:    "report.pdf",
		ContentType: "application/pdf",
	}, "scope-1")
	if err != nil {
		t.Fatal(err)
	}

	err = channel.SendMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "123456789@s.whatsapp.net",
		Parts: []bus.MediaPart{{
			Type:        "file",
			Ref:         ref,
			Filename:    "report.pdf",
			ContentType: "application/pdf",
		}},
	})
	if !errors.Is(err, channels.ErrTemporary) {
		t.Fatalf("SendMedia() error = %v, want ErrTemporary", err)
	}
	if !strings.Contains(err.Error(), "not yet paired") {
		t.Fatalf("SendMedia() error = %v, want not yet paired", err)
	}
}
