package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	slacksdk "github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
)

func TestSendMediaExternalUpload(t *testing.T) {
	content := "attachment contents"
	var calls []string
	var callsMu sync.Mutex
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		calls = append(calls, r.URL.Path)
		callsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/files.getUploadURLExternal":
			if r.FormValue("length") != strconv.Itoa(len(content)) || r.FormValue("filename") != "test.txt" {
				t.Error("upload must include the file size and filename")
			}
			fmt.Fprintf(w, `{"ok":true,"upload_url":%q,"file_id":"F123"}`, server.URL+"/upload")
		case "/upload":
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil || string(data) != content {
				t.Errorf("unexpected uploaded contents: %q, %v", data, err)
			}
			fmt.Fprint(w, `{"ok":true}`)
		case "/files.completeUploadExternal":
			if r.FormValue("channel_id") != "C123" {
				t.Error("upload completed in the wrong channel")
			}
			var files []slacksdk.FileSummary
			if err := json.Unmarshal([]byte(r.FormValue("files")), &files); err != nil ||
				len(files) != 1 || files[0].ID != "F123" || files[0].Title != "caption" {
				t.Errorf("unexpected completion metadata: %v, %v", files, err)
			}
			fmt.Fprint(w, `{"ok":true,"files":[{"id":"F123","title":"caption"}]}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	channel, err := NewSlackChannel(config.SlackConfig{BotToken: "test", AppToken: "test"}, bus.NewMessageBus())
	require.NoError(t, err)
	channel.api = slacksdk.New("test",
		slacksdk.OptionAPIURL(server.URL+"/"), slacksdk.OptionHTTPClient(server.Client()))
	channel.SetRunning(true)
	store := media.NewFileMediaStore()
	channel.SetMediaStore(store)
	path := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	ref, err := store.Store(path, media.MediaMeta{Filename: "test.txt"}, "test")
	require.NoError(t, err)
	err = channel.SendMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "C123",
		Parts:  []bus.MediaPart{{Ref: ref, Filename: "test.txt", Caption: "caption"}},
	})
	require.NoError(t, err)
	want := []string{"/files.getUploadURLExternal", "/upload", "/files.completeUploadExternal"}
	callsMu.Lock()
	defer callsMu.Unlock()
	require.Equal(t, want, calls)
}

func TestParseSlackChatID(t *testing.T) {
	tests := []struct {
		name       string
		chatID     string
		wantChanID string
		wantThread string
	}{
		{
			name:       "channel only",
			chatID:     "C123456",
			wantChanID: "C123456",
			wantThread: "",
		},
		{
			name:       "channel with thread",
			chatID:     "C123456/1234567890.123456",
			wantChanID: "C123456",
			wantThread: "1234567890.123456",
		},
		{
			name:       "DM channel",
			chatID:     "D987654",
			wantChanID: "D987654",
			wantThread: "",
		},
		{
			name:       "empty string",
			chatID:     "",
			wantChanID: "",
			wantThread: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chanID, threadTS := parseSlackChatID(tt.chatID)
			if chanID != tt.wantChanID {
				t.Errorf("parseSlackChatID(%q) channelID = %q, want %q", tt.chatID, chanID, tt.wantChanID)
			}
			if threadTS != tt.wantThread {
				t.Errorf("parseSlackChatID(%q) threadTS = %q, want %q", tt.chatID, threadTS, tt.wantThread)
			}
		})
	}
}

func TestStripBotMention(t *testing.T) {
	ch := &SlackChannel{botUserID: "U12345BOT"}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "mention at start",
			input: "<@U12345BOT> hello there",
			want:  "hello there",
		},
		{
			name:  "mention in middle",
			input: "hey <@U12345BOT> can you help",
			want:  "hey  can you help",
		},
		{
			name:  "no mention",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only mention",
			input: "<@U12345BOT>",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ch.stripBotMention(tt.input)
			if got != tt.want {
				t.Errorf("stripBotMention(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewSlackChannel(t *testing.T) {
	msgBus := bus.NewMessageBus()

	t.Run("missing bot token", func(t *testing.T) {
		cfg := config.SlackConfig{
			BotToken: "",
			AppToken: "xapp-test",
		}
		_, err := NewSlackChannel(cfg, msgBus)
		if err == nil {
			t.Error("expected error for missing bot_token, got nil")
		}
	})

	t.Run("missing app token", func(t *testing.T) {
		cfg := config.SlackConfig{
			BotToken: "xoxb-test",
			AppToken: "",
		}
		_, err := NewSlackChannel(cfg, msgBus)
		if err == nil {
			t.Error("expected error for missing app_token, got nil")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		cfg := config.SlackConfig{
			BotToken:  "xoxb-test",
			AppToken:  "xapp-test",
			AllowFrom: []string{"U123"},
		}
		ch, err := NewSlackChannel(cfg, msgBus)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ch.Name() != "slack" {
			t.Errorf("Name() = %q, want %q", ch.Name(), "slack")
		}
		if ch.IsRunning() {
			t.Error("new channel should not be running")
		}
	})
}

func TestSlackChannelIsAllowed(t *testing.T) {
	msgBus := bus.NewMessageBus()

	t.Run("empty allowlist allows all", func(t *testing.T) {
		cfg := config.SlackConfig{
			BotToken:  "xoxb-test",
			AppToken:  "xapp-test",
			AllowFrom: []string{},
		}
		ch, _ := NewSlackChannel(cfg, msgBus)
		if !ch.IsAllowed("U_ANYONE") {
			t.Error("empty allowlist should allow all users")
		}
	})

	t.Run("allowlist restricts users", func(t *testing.T) {
		cfg := config.SlackConfig{
			BotToken:  "xoxb-test",
			AppToken:  "xapp-test",
			AllowFrom: []string{"U_ALLOWED"},
		}
		ch, _ := NewSlackChannel(cfg, msgBus)
		if !ch.IsAllowed("U_ALLOWED") {
			t.Error("allowed user should pass allowlist check")
		}
		if ch.IsAllowed("U_BLOCKED") {
			t.Error("non-allowed user should be blocked")
		}
	})
}
