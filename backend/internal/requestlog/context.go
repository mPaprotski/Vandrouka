// Package requestlog carries safe request diagnostics without HTTP dependencies.
package requestlog

import (
	"context"
	"sync"
)

type key struct{}
type trace struct {
	mu    sync.Mutex
	stage string
}

func WithTrace(ctx context.Context) context.Context {
	return context.WithValue(ctx, key{}, &trace{stage: "request"})
}
func SetStage(ctx context.Context, stage string) {
	if t, ok := ctx.Value(key{}).(*trace); ok {
		t.mu.Lock()
		t.stage = stage
		t.mu.Unlock()
	}
}
func Stage(ctx context.Context) string {
	if t, ok := ctx.Value(key{}).(*trace); ok {
		t.mu.Lock()
		defer t.mu.Unlock()
		return t.stage
	}
	return "request"
}
