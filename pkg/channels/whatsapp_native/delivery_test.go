//go:build whatsapp_native

package whatsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestOutboundMediaPreflightsEntireBatch(t *testing.T) {
	ch, err := NewWhatsAppNativeChannel(config.WhatsAppConfig{}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := ch.(*WhatsAppNativeChannel)
	c.SetRunning(true)
	jid := types.NewJID("123", types.DefaultUserServer)
	c.client = &whatsmeow.Client{Store: &store.Device{ID: &jid}}
	c.isConnected = func(*whatsmeow.Client) bool { return true }
	sends, uploads := 0, 0
	c.sendMessageHook = func(context.Context, types.JID, *waE2E.Message) (whatsmeow.SendResponse, error) {
		sends++
		return whatsmeow.SendResponse{ID: "accepted-id"}, nil
	}
	c.uploadHook = func(context.Context, []byte, whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
		uploads++
		return whatsmeow.UploadResponse{}, nil
	}
	mediaStore := media.NewFileMediaStore()
	c.SetMediaStore(mediaStore)
	path := filepath.Join(t.TempDir(), "document.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	ref, err := mediaStore.Store(path, media.MediaMeta{ContentType: "text/plain"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	msg := bus.OutboundMediaMessage{ChatID: jid.String(), Parts: []bus.MediaPart{{Ref: ref}, {Ref: "media://missing"}}}
	if err := c.SendMedia(context.Background(), msg); !errors.Is(err, channels.ErrSendFailed) {
		t.Fatalf("missing ref accepted: %v", err)
	}
	if sends != 0 || uploads != 0 {
		t.Fatal("partial batch sent before validation")
	}
	msg.Parts = msg.Parts[:1]
	if err := c.SendMedia(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if sends != 1 || uploads != 1 {
		t.Fatal("valid batch not sent")
	}
	c.sendMessageHook = func(context.Context, types.JID, *waE2E.Message) (whatsmeow.SendResponse, error) {
		return whatsmeow.SendResponse{}, errors.New("offline")
	}
	if err := c.Send(context.Background(), bus.OutboundMessage{ChatID: jid.String(), Content: "test"}); err == nil {
		t.Fatal("platform error hidden")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := c.SendMedia(context.Background(), msg); !errors.Is(err, channels.ErrSendFailed) {
		t.Fatal("read failure hidden")
	}
}

func TestDisconnectedEventsDoNotStartCustomReconnect(t *testing.T) {
	// A zero-value client cannot Connect: repeated events must only notify, never call it.
	c := &WhatsAppNativeChannel{client: &whatsmeow.Client{}}
	for i := 0; i < 10; i++ {
		c.eventHandler(&events.Disconnected{})
	}
	c.stopping.Store(true)
	c.eventHandler(&events.Disconnected{})
	c.eventHandler(&events.LoggedOut{})
}
