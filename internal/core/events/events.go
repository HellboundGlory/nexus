package events

import (
	"context"
	"log/slog"
	"sync"
)

type Event interface{ Name() string }

type Handler func(context.Context, Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	log      *slog.Logger
}

func New() *Bus {
	return &Bus{handlers: make(map[string][]Handler), log: slog.Default()}
}

// WithLogger sets the logger used for async panic recovery. Returns the bus for chaining.
func (b *Bus) WithLogger(l *slog.Logger) *Bus { b.log = l; return b }

func (b *Bus) Subscribe(name string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[name] = append(b.handlers[name], h)
}

func (b *Bus) snapshot(name string) []Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	hs := b.handlers[name]
	out := make([]Handler, len(hs))
	copy(out, hs)
	return out
}

// Publish runs handlers synchronously in registration order.
func (b *Bus) Publish(ctx context.Context, e Event) {
	for _, h := range b.snapshot(e.Name()) {
		h(ctx, e)
	}
}

// PublishAsync runs each handler in its own goroutine with panic recovery.
func (b *Bus) PublishAsync(ctx context.Context, e Event) {
	for _, h := range b.snapshot(e.Name()) {
		h := h
		go func() {
			defer func() {
				if r := recover(); r != nil {
					b.log.Error("event handler panicked", "event", e.Name(), "recover", r)
				}
			}()
			h(ctx, e)
		}()
	}
}
