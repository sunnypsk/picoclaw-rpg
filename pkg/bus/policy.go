package bus

import (
	"context"
	"sync"
)

type traceKey struct{}

func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}
func Trace(ctx context.Context) string { v, _ := ctx.Value(traceKey{}).(string); return v }

type bufferKey struct{}
type OutputBuffer struct {
	mu       sync.Mutex
	messages []OutboundMessage
}

func WithOutputBuffer(ctx context.Context) (context.Context, *OutputBuffer) {
	b := &OutputBuffer{}
	return context.WithValue(ctx, bufferKey{}, b), b
}
func (b *OutputBuffer) Messages() []OutboundMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]OutboundMessage(nil), b.messages...)
}
