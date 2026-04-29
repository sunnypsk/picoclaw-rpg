package channels

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/sipeed/picoclaw/pkg/bus"
)

// mockChannel is a test double that delegates Send to a configurable function.
type mockChannel struct {
	BaseChannel
	sendFn func(ctx context.Context, msg bus.OutboundMessage) error
}

func (m *mockChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	return m.sendFn(ctx, msg)
}

func (m *mockChannel) Start(ctx context.Context) error { return nil }
func (m *mockChannel) Stop(ctx context.Context) error  { return nil }

type mockReactionChannel struct {
	mockChannel
	reactionFn func(ctx context.Context, msg bus.OutboundReactionMessage) error
}

func (m *mockReactionChannel) SendReaction(ctx context.Context, msg bus.OutboundReactionMessage) error {
	return m.reactionFn(ctx, msg)
}

type mockMediaChannel struct {
	mockChannel
	mediaFn func(ctx context.Context, msg bus.OutboundMediaMessage) error
}

func (m *mockMediaChannel) SendMedia(ctx context.Context, msg bus.OutboundMediaMessage) error {
	return m.mediaFn(ctx, msg)
}

// newTestManager creates a minimal Manager suitable for unit tests.
func newTestManager() *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		workers:  make(map[string]*channelWorker),
	}
}

func TestSendWithRetry_Success(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			return nil
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	m.sendWithRetry(ctx, "test", w, msg)

	if callCount != 1 {
		t.Fatalf("expected 1 Send call, got %d", callCount)
	}
}

func TestSendWithRetry_TemporaryThenSuccess(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			if callCount <= 2 {
				return fmt.Errorf("network error: %w", ErrTemporary)
			}
			return nil
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	m.sendWithRetry(ctx, "test", w, msg)

	if callCount != 3 {
		t.Fatalf("expected 3 Send calls (2 failures + 1 success), got %d", callCount)
	}
}

func TestSendWithRetry_PermanentFailure(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			return fmt.Errorf("bad chat ID: %w", ErrSendFailed)
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	m.sendWithRetry(ctx, "test", w, msg)

	if callCount != 1 {
		t.Fatalf("expected 1 Send call (no retry for permanent failure), got %d", callCount)
	}
}

func TestSendWithRetry_NotRunning(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			return ErrNotRunning
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	m.sendWithRetry(ctx, "test", w, msg)

	if callCount != 1 {
		t.Fatalf("expected 1 Send call (no retry for ErrNotRunning), got %d", callCount)
	}
}

func TestSendWithRetry_RateLimitRetry(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			if callCount == 1 {
				return fmt.Errorf("429: %w", ErrRateLimit)
			}
			return nil
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	start := time.Now()
	m.sendWithRetry(ctx, "test", w, msg)
	elapsed := time.Since(start)

	if callCount != 2 {
		t.Fatalf("expected 2 Send calls (1 rate limit + 1 success), got %d", callCount)
	}
	// Should have waited at least rateLimitDelay (1s) but allow some slack
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected at least ~1s delay for rate limit retry, got %v", elapsed)
	}
}

func TestSendWithRetry_MaxRetriesExhausted(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			return fmt.Errorf("timeout: %w", ErrTemporary)
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	m.sendWithRetry(ctx, "test", w, msg)

	expected := maxRetries + 1 // initial attempt + maxRetries retries
	if callCount != expected {
		t.Fatalf("expected %d Send calls, got %d", expected, callCount)
	}
}

func TestSendWithRetry_UnknownError(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			if callCount == 1 {
				return errors.New("random unexpected error")
			}
			return nil
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	m.sendWithRetry(ctx, "test", w, msg)

	if callCount != 2 {
		t.Fatalf("expected 2 Send calls (unknown error treated as temporary), got %d", callCount)
	}
}

func TestSendWithRetry_ContextCancelled(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callCount++
			return fmt.Errorf("timeout: %w", ErrTemporary)
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	// Cancel context after first Send attempt returns
	ch.sendFn = func(_ context.Context, _ bus.OutboundMessage) error {
		callCount++
		cancel()
		return fmt.Errorf("timeout: %w", ErrTemporary)
	}

	m.sendWithRetry(ctx, "test", w, msg)

	// Should have called Send once, then noticed ctx canceled during backoff
	if callCount != 1 {
		t.Fatalf("expected 1 Send call before context cancellation, got %d", callCount)
	}
}

func TestWorkerRateLimiter(t *testing.T) {
	m := newTestManager()

	var mu sync.Mutex
	var sendTimes []time.Time

	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			mu.Lock()
			sendTimes = append(sendTimes, time.Now())
			mu.Unlock()
			return nil
		},
	}

	// Create a worker with a low rate: 2 msg/s, burst 1
	w := &channelWorker{
		ch:      ch,
		queue:   make(chan bus.OutboundMessage, 10),
		done:    make(chan struct{}),
		limiter: rate.NewLimiter(2, 1),
	}

	ctx := t.Context()

	go m.runWorker(ctx, "test", w)

	// Enqueue 4 messages
	for i := range 4 {
		w.queue <- bus.OutboundMessage{Channel: "test", ChatID: "1", Content: fmt.Sprintf("msg%d", i)}
	}

	// Wait enough time for all messages to be sent (4 msgs at 2/s = ~2s, give extra margin)
	time.Sleep(3 * time.Second)

	mu.Lock()
	times := make([]time.Time, len(sendTimes))
	copy(times, sendTimes)
	mu.Unlock()

	if len(times) != 4 {
		t.Fatalf("expected 4 sends, got %d", len(times))
	}

	// Verify rate limiting: total duration should be at least 1s
	// (first message immediate, then ~500ms between each subsequent one at 2/s)
	totalDuration := times[len(times)-1].Sub(times[0])
	if totalDuration < 1*time.Second {
		t.Fatalf("expected total duration >= 1s for 4 msgs at 2/s rate, got %v", totalDuration)
	}
}

func TestNewChannelWorker_DefaultRate(t *testing.T) {
	ch := &mockChannel{}
	w := newChannelWorker("unknown_channel", ch)

	if w.limiter == nil {
		t.Fatal("expected limiter to be non-nil")
	}
	if w.limiter.Limit() != rate.Limit(defaultRateLimit) {
		t.Fatalf("expected rate limit %v, got %v", rate.Limit(defaultRateLimit), w.limiter.Limit())
	}
}

func TestNewChannelWorker_ConfiguredRate(t *testing.T) {
	ch := &mockChannel{}

	for name, expectedRate := range channelRateConfig {
		w := newChannelWorker(name, ch)
		if w.limiter.Limit() != rate.Limit(expectedRate) {
			t.Fatalf("channel %s: expected rate %v, got %v", name, expectedRate, w.limiter.Limit())
		}
	}
}

func TestSendReactionWithRetry_Success(t *testing.T) {
	m := newTestManager()
	var callCount int
	ch := &mockReactionChannel{
		mockChannel: mockChannel{},
		reactionFn: func(_ context.Context, _ bus.OutboundReactionMessage) error {
			callCount++
			return nil
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	m.sendReactionWithRetry(context.Background(), "whatsapp_native", w, bus.OutboundReactionMessage{
		Channel: "whatsapp_native", ChatID: "chat", MessageID: "msg", Emoji: "👍",
	})

	if callCount != 1 {
		t.Fatalf("expected 1 SendReaction call, got %d", callCount)
	}
}

func TestDispatchOutboundReaction_RoutesToWorker(t *testing.T) {
	msgBus := bus.NewMessageBus()
	defer msgBus.Close()

	m := &Manager{
		channels: map[string]Channel{
			"whatsapp_native": &mockReactionChannel{mockChannel: mockChannel{}},
		},
		workers: make(map[string]*channelWorker),
		bus:     msgBus,
	}
	w := newChannelWorker("whatsapp_native", &mockReactionChannel{mockChannel: mockChannel{}})
	m.workers["whatsapp_native"] = w

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.dispatchOutboundReaction(ctx)

	want := bus.OutboundReactionMessage{
		Channel:        "whatsapp_native",
		ChatID:         "chat-1",
		MessageID:      "msg-1",
		TargetSenderID: "user-1",
		Emoji:          "👍",
	}
	if err := msgBus.PublishOutboundReaction(context.Background(), want); err != nil {
		t.Fatalf("PublishOutboundReaction failed: %v", err)
	}

	select {
	case got := <-w.reactionQueue:
		if got != want {
			t.Fatalf("reaction routed = %#v, want %#v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reaction to be routed")
	}
}

func TestRunWorker_MessageSplitting(t *testing.T) {
	m := newTestManager()

	var mu sync.Mutex
	var received []string

	ch := &mockChannelWithLength{
		mockChannel: mockChannel{
			sendFn: func(_ context.Context, msg bus.OutboundMessage) error {
				mu.Lock()
				received = append(received, msg.Content)
				mu.Unlock()
				return nil
			},
		},
		maxLen: 5,
	}

	w := &channelWorker{
		ch:      ch,
		queue:   make(chan bus.OutboundMessage, 10),
		done:    make(chan struct{}),
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := t.Context()

	go m.runWorker(ctx, "test", w)

	// Send a message that should be split
	w.queue <- bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello world"}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count < 2 {
		t.Fatalf("expected message to be split into at least 2 chunks, got %d", count)
	}
}

// mockChannelWithLength implements MessageLengthProvider.
type mockChannelWithLength struct {
	mockChannel
	maxLen int
}

func (m *mockChannelWithLength) MaxMessageLength() int {
	return m.maxLen
}

func TestSendWithRetry_ExponentialBackoff(t *testing.T) {
	m := newTestManager()

	var callTimes []time.Time
	var callCount atomic.Int32
	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			callTimes = append(callTimes, time.Now())
			callCount.Add(1)
			return fmt.Errorf("timeout: %w", ErrTemporary)
		},
	}
	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	ctx := context.Background()
	msg := bus.OutboundMessage{Channel: "test", ChatID: "1", Content: "hello"}

	start := time.Now()
	m.sendWithRetry(ctx, "test", w, msg)
	totalElapsed := time.Since(start)

	// With maxRetries=3: attempts at 0, ~500ms, ~1.5s, ~3.5s
	// Total backoff: 500ms + 1s + 2s = 3.5s
	// Allow some margin
	if totalElapsed < 3*time.Second {
		t.Fatalf("expected total elapsed >= 3s for exponential backoff, got %v", totalElapsed)
	}

	if int(callCount.Load()) != maxRetries+1 {
		t.Fatalf("expected %d calls, got %d", maxRetries+1, callCount.Load())
	}
}

// --- Phase 10: preSend orchestration tests ---

// mockMessageEditor is a channel that supports MessageEditor.
type mockMessageEditor struct {
	mockChannel
	editFn func(ctx context.Context, chatID, messageID, content string) error
}

func (m *mockMessageEditor) EditMessage(ctx context.Context, chatID, messageID, content string) error {
	return m.editFn(ctx, chatID, messageID, content)
}

func TestPreSend_PlaceholderEditSuccess(t *testing.T) {
	m := newTestManager()
	var sendCalled bool
	var editCalled bool

	ch := &mockMessageEditor{
		mockChannel: mockChannel{
			sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
				sendCalled = true
				return nil
			},
		},
		editFn: func(_ context.Context, chatID, messageID, content string) error {
			editCalled = true
			if chatID != "123" {
				t.Fatalf("expected chatID 123, got %s", chatID)
			}
			if messageID != "456" {
				t.Fatalf("expected messageID 456, got %s", messageID)
			}
			if content != "hello" {
				t.Fatalf("expected content 'hello', got %s", content)
			}
			return nil
		},
	}

	// Register placeholder
	m.RecordPlaceholder("test", "123", "456")

	msg := bus.OutboundMessage{Channel: "test", ChatID: "123", Content: "hello"}
	edited := m.preSend(context.Background(), "test", msg, ch)

	if !edited {
		t.Fatal("expected preSend to return true (placeholder edited)")
	}
	if !editCalled {
		t.Fatal("expected EditMessage to be called")
	}
	if sendCalled {
		t.Fatal("expected Send to NOT be called when placeholder edited")
	}
}

func TestPreSend_PlaceholderEditFails_FallsThrough(t *testing.T) {
	m := newTestManager()

	ch := &mockMessageEditor{
		mockChannel: mockChannel{
			sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
				return nil
			},
		},
		editFn: func(_ context.Context, _, _, _ string) error {
			return fmt.Errorf("edit failed")
		},
	}

	m.RecordPlaceholder("test", "123", "456")

	msg := bus.OutboundMessage{Channel: "test", ChatID: "123", Content: "hello"}
	edited := m.preSend(context.Background(), "test", msg, ch)

	if edited {
		t.Fatal("expected preSend to return false when edit fails")
	}
}

func TestPreSend_TypingStopCalled(t *testing.T) {
	m := newTestManager()
	var stopCalled bool

	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			return nil
		},
	}

	m.RecordTypingStop("test", "123", "msg-1", func() {
		stopCalled = true
	})

	msg := bus.OutboundMessage{Channel: "test", ChatID: "123", Content: "hello", ReplyToMessageID: "msg-1"}
	m.preSend(context.Background(), "test", msg, ch)

	if !stopCalled {
		t.Fatal("expected typing stop func to be called")
	}
}

func TestPreSendStopsMatchingTypingMessageOnly(t *testing.T) {
	m := newTestManager()
	var firstStopped bool
	var secondStopped bool

	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			return nil
		},
	}

	m.RecordTypingStop("test", "chat1", "msg-1", func() {
		firstStopped = true
	})
	m.RecordTypingStop("test", "chat1", "msg-2", func() {
		secondStopped = true
	})

	m.preSend(context.Background(), "test", bus.OutboundMessage{
		Channel:          "test",
		ChatID:           "chat1",
		Content:          "first",
		ReplyToMessageID: "msg-1",
	}, ch)

	if !firstStopped {
		t.Fatal("expected first typing stop to be called")
	}
	if secondStopped {
		t.Fatal("did not expect second typing stop to be called")
	}
	if _, loaded := m.typingStops.Load(typingStopKey{channel: "test", chatID: "chat1", messageID: "msg-2"}); !loaded {
		t.Fatal("expected second typing entry to remain")
	}

	m.preSend(context.Background(), "test", bus.OutboundMessage{
		Channel:          "test",
		ChatID:           "chat1",
		Content:          "second",
		ReplyToMessageID: "msg-2",
	}, ch)

	if !secondStopped {
		t.Fatal("expected second typing stop to be called")
	}
	if _, loaded := m.typingStops.Load(typingStopKey{channel: "test", chatID: "chat1", messageID: "msg-2"}); loaded {
		t.Fatal("expected second typing entry to be deleted")
	}
}

func TestPreSendWithoutReplyStopsAllTypingForChat(t *testing.T) {
	m := newTestManager()
	var stopped atomic.Int32
	var otherStopped bool

	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			return nil
		},
	}

	m.RecordTypingStop("test", "chat1", "msg-1", func() { stopped.Add(1) })
	m.RecordTypingStop("test", "chat1", "msg-2", func() { stopped.Add(1) })
	m.RecordTypingStop("test", "other", "msg-3", func() { otherStopped = true })

	m.preSend(context.Background(), "test", bus.OutboundMessage{
		Channel: "test",
		ChatID:  "chat1",
		Content: "fallback",
	}, ch)

	if got := stopped.Load(); got != 2 {
		t.Fatalf("stopped typing entries = %d, want 2", got)
	}
	if otherStopped {
		t.Fatal("did not expect other chat typing stop to be called")
	}
	if _, loaded := m.typingStops.Load(typingStopKey{channel: "test", chatID: "other", messageID: "msg-3"}); !loaded {
		t.Fatal("expected other chat typing entry to remain")
	}
}

func TestRecordTypingStop_ReplacesDuplicateKey(t *testing.T) {
	m := newTestManager()
	var firstStopped bool
	var secondStopped bool

	m.RecordTypingStop("test", "chat1", "msg-1", func() {
		firstStopped = true
	})
	m.RecordTypingStop("test", "chat1", "msg-1", func() {
		secondStopped = true
	})

	if !firstStopped {
		t.Fatal("expected replaced typing stop to be called")
	}
	if secondStopped {
		t.Fatal("did not expect replacement typing stop to be called yet")
	}

	m.stopTypingForOutbound("test", "chat1", "msg-1")

	if !secondStopped {
		t.Fatal("expected replacement typing stop to be called")
	}
}

func TestPreSend_NoRegisteredState(t *testing.T) {
	m := newTestManager()

	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			return nil
		},
	}

	msg := bus.OutboundMessage{Channel: "test", ChatID: "123", Content: "hello"}
	edited := m.preSend(context.Background(), "test", msg, ch)

	if edited {
		t.Fatal("expected preSend to return false with no registered state")
	}
}

func TestPreSend_TypingAndPlaceholder(t *testing.T) {
	m := newTestManager()
	var stopCalled bool
	var editCalled bool

	ch := &mockMessageEditor{
		mockChannel: mockChannel{
			sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
				return nil
			},
		},
		editFn: func(_ context.Context, _, _, _ string) error {
			editCalled = true
			return nil
		},
	}

	m.RecordTypingStop("test", "123", "msg-1", func() {
		stopCalled = true
	})
	m.RecordPlaceholder("test", "123", "456")

	msg := bus.OutboundMessage{Channel: "test", ChatID: "123", Content: "hello", ReplyToMessageID: "msg-1"}
	edited := m.preSend(context.Background(), "test", msg, ch)

	if !stopCalled {
		t.Fatal("expected typing stop to be called")
	}
	if !editCalled {
		t.Fatal("expected EditMessage to be called")
	}
	if !edited {
		t.Fatal("expected preSend to return true")
	}
}

func TestRecordPlaceholder_ConcurrentSafe(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			chatID := fmt.Sprintf("chat_%d", i%10)
			m.RecordPlaceholder("test", chatID, fmt.Sprintf("msg_%d", i))
		}(i)
	}
	wg.Wait()
}

func TestRecordTypingStop_ConcurrentSafe(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			chatID := fmt.Sprintf("chat_%d", i%10)
			m.RecordTypingStop("test", chatID, fmt.Sprintf("msg_%d", i), func() {})
		}(i)
	}
	wg.Wait()
}

func TestSendWithRetry_PreSendEditsPlaceholder(t *testing.T) {
	m := newTestManager()
	var sendCalled bool

	ch := &mockMessageEditor{
		mockChannel: mockChannel{
			sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
				sendCalled = true
				return nil
			},
		},
		editFn: func(_ context.Context, _, _, _ string) error {
			return nil // edit succeeds
		},
	}

	m.RecordPlaceholder("test", "123", "456")

	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	msg := bus.OutboundMessage{Channel: "test", ChatID: "123", Content: "hello"}
	m.sendWithRetry(context.Background(), "test", w, msg)

	if sendCalled {
		t.Fatal("expected Send to NOT be called when placeholder was edited")
	}
}

func TestSendMediaWithRetry_StopsMatchingTypingMessage(t *testing.T) {
	m := newTestManager()
	var stopCalled bool
	var mediaCalled bool

	ch := &mockMediaChannel{
		mockChannel: mockChannel{
			sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
				return nil
			},
		},
		mediaFn: func(_ context.Context, _ bus.OutboundMediaMessage) error {
			mediaCalled = true
			return nil
		},
	}

	m.RecordTypingStop("test", "chat1", "msg-1", func() {
		stopCalled = true
	})

	w := &channelWorker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}

	m.sendMediaWithRetry(context.Background(), "test", w, bus.OutboundMediaMessage{
		Channel:          "test",
		ChatID:           "chat1",
		Parts:            []bus.MediaPart{{Type: "image", Ref: "media://1"}},
		ReplyToMessageID: "msg-1",
	})

	if !stopCalled {
		t.Fatal("expected typing stop to be called before media send")
	}
	if !mediaCalled {
		t.Fatal("expected SendMedia to be called")
	}
}

// --- Dispatcher exit tests (Step 1) ---

func TestDispatcherExitsOnCancel(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	m := &Manager{
		channels: make(map[string]Channel),
		workers:  make(map[string]*channelWorker),
		bus:      mb,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		m.dispatchOutbound(ctx)
		close(done)
	}()

	// Cancel context and verify the dispatcher exits quickly
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("dispatchOutbound did not exit within 2s after context cancel")
	}
}

func TestDispatcherMediaExitsOnCancel(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	m := &Manager{
		channels: make(map[string]Channel),
		workers:  make(map[string]*channelWorker),
		bus:      mb,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		m.dispatchOutboundMedia(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("dispatchOutboundMedia did not exit within 2s after context cancel")
	}
}

// --- TTL Janitor tests (Step 2) ---

func TestTypingStopJanitorEviction(t *testing.T) {
	m := newTestManager()

	var stopCalled atomic.Bool
	// Store a typing entry with a creation time far in the past
	m.typingStops.Store(typingStopKey{channel: "test", chatID: "123", messageID: "msg-1"}, typingEntry{
		stop:      func() { stopCalled.Store(true) },
		createdAt: time.Now().Add(-10 * time.Minute), // well past typingStopTTL
	})

	// Run janitor with a short-lived context
	ctx, cancel := context.WithCancel(context.Background())

	// Manually trigger the janitor logic once by simulating a tick
	go func() {
		// Override janitor to run immediately
		now := time.Now()
		m.typingStops.Range(func(key, value any) bool {
			if entry, ok := value.(typingEntry); ok {
				if now.Sub(entry.createdAt) > typingStopTTL {
					if _, loaded := m.typingStops.LoadAndDelete(key); loaded {
						entry.stop()
					}
				}
			}
			return true
		})
		cancel()
	}()

	<-ctx.Done()

	if !stopCalled.Load() {
		t.Fatal("expected typing stop function to be called by janitor eviction")
	}

	// Verify entry was deleted
	if _, loaded := m.typingStops.Load(typingStopKey{channel: "test", chatID: "123", messageID: "msg-1"}); loaded {
		t.Fatal("expected typing entry to be deleted after eviction")
	}
}

func TestPlaceholderJanitorEviction(t *testing.T) {
	m := newTestManager()

	// Store a placeholder entry with a creation time far in the past
	m.placeholders.Store("test:456", placeholderEntry{
		id:        "msg_old",
		createdAt: time.Now().Add(-20 * time.Minute), // well past placeholderTTL
	})

	// Simulate janitor logic
	now := time.Now()
	m.placeholders.Range(func(key, value any) bool {
		if entry, ok := value.(placeholderEntry); ok {
			if now.Sub(entry.createdAt) > placeholderTTL {
				m.placeholders.Delete(key)
			}
		}
		return true
	})

	// Verify entry was deleted
	if _, loaded := m.placeholders.Load("test:456"); loaded {
		t.Fatal("expected placeholder entry to be deleted after eviction")
	}
}

func TestPreSendStillWorksWithWrappedTypes(t *testing.T) {
	m := newTestManager()
	var stopCalled bool
	var editCalled bool

	ch := &mockMessageEditor{
		mockChannel: mockChannel{
			sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
				return nil
			},
		},
		editFn: func(_ context.Context, chatID, messageID, content string) error {
			editCalled = true
			if messageID != "ph_id" {
				t.Fatalf("expected messageID ph_id, got %s", messageID)
			}
			return nil
		},
	}

	// Use the new wrapped types via the public API
	m.RecordTypingStop("test", "chat1", "msg-1", func() {
		stopCalled = true
	})
	m.RecordPlaceholder("test", "chat1", "ph_id")

	msg := bus.OutboundMessage{Channel: "test", ChatID: "chat1", Content: "response", ReplyToMessageID: "msg-1"}
	edited := m.preSend(context.Background(), "test", msg, ch)

	if !stopCalled {
		t.Fatal("expected typing stop to be called via wrapped type")
	}
	if !editCalled {
		t.Fatal("expected EditMessage to be called via wrapped type")
	}
	if !edited {
		t.Fatal("expected preSend to return true")
	}
}

// --- Lazy worker creation tests (Step 6) ---

func TestLazyWorkerCreation(t *testing.T) {
	m := newTestManager()

	ch := &mockChannel{
		sendFn: func(_ context.Context, _ bus.OutboundMessage) error {
			return nil
		},
	}

	// RegisterChannel should NOT create a worker
	m.RegisterChannel("lazy", ch)

	m.mu.RLock()
	_, chExists := m.channels["lazy"]
	_, wExists := m.workers["lazy"]
	m.mu.RUnlock()

	if !chExists {
		t.Fatal("expected channel to be registered")
	}
	if wExists {
		t.Fatal("expected worker to NOT be created by RegisterChannel (lazy creation)")
	}
}

// --- FastID uniqueness test (Step 5) ---

func TestBuildMediaScope_FastIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)

	for range 1000 {
		scope := BuildMediaScope("test", "chat1", "")
		if seen[scope] {
			t.Fatalf("duplicate scope generated: %s", scope)
		}
		seen[scope] = true
	}

	// Verify format: "channel:chatID:id"
	scope := BuildMediaScope("telegram", "42", "")
	parts := 0
	for _, c := range scope {
		if c == ':' {
			parts++
		}
	}
	if parts != 2 {
		t.Fatalf("expected scope to have 2 colons (channel:chatID:id), got: %s", scope)
	}
}

func TestBuildMediaScope_WithMessageID(t *testing.T) {
	scope := BuildMediaScope("discord", "chat99", "msg123")
	expected := "discord:chat99:msg123"
	if scope != expected {
		t.Fatalf("expected %s, got %s", expected, scope)
	}
}
