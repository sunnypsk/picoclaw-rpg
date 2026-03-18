package telegram

import (
	"testing"

	"github.com/mymmrac/telego"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestTelegramMessageSubtype(t *testing.T) {
	if got := telegramMessageSubtype(&telego.Message{Voice: &telego.Voice{}}); got != bus.MessageSubtypeVoiceNote {
		t.Fatalf("voice message subtype = %q, want %q", got, bus.MessageSubtypeVoiceNote)
	}

	if got := telegramMessageSubtype(&telego.Message{Audio: &telego.Audio{}}); got != "" {
		t.Fatalf("audio upload subtype = %q, want empty", got)
	}

	if got := telegramMessageSubtype(nil); got != "" {
		t.Fatalf("nil message subtype = %q, want empty", got)
	}
}
