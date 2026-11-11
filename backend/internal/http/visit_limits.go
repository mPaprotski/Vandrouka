package apihttp

import (
	"net/http"
	"sync"
	"time"
)

// Per-process bounds for the single-instance MVP. Identity comes from authentication.
type visitLimits struct {
	mu      sync.Mutex
	windows map[int64]authWindow
	slots   chan struct{}
	now     func() time.Time
}

func newVisitLimits() *visitLimits {
	return &visitLimits{windows: make(map[int64]authWindow), slots: make(chan struct{}, 4), now: time.Now}
}
func (l *visitLimits) admit(w http.ResponseWriter, owner int64) bool {
	l.mu.Lock()
	now := l.now()
	for id, v := range l.windows {
		if !now.Before(v.expires) {
			delete(l.windows, id)
		}
	}
	v, exists := l.windows[owner]
	if (!exists && len(l.windows) >= 4096) || v.count >= 30 {
		l.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		Error(w, 429, "rate_limited", "Слишком много подтверждений. Повторите через минуту")
		return false
	}
	if !exists {
		v.expires = now.Add(time.Minute)
	}
	v.count++
	l.windows[owner] = v
	l.mu.Unlock()
	select {
	case l.slots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		Error(w, 429, "rate_limited", "Слишком много одновременных подтверждений. Повторите позже")
		return false
	}
}
