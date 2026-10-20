package apihttp

import (
	"fmt"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

type RouteLimitOptions struct {
	RequestsPerMinute int
	Burst             int
	MaxConcurrent     int
	MaxClients        int
	IdleTTL           time.Duration
}
type clientBucket struct {
	tokens            float64
	updated, lastSeen time.Time
}
type RouteLimiter struct {
	mu          sync.Mutex
	options     RouteLimitOptions
	clients     map[string]clientBucket
	nextCleanup time.Time
	slots       chan struct{}
	now         func() time.Time
}

func NewRouteLimiter(options RouteLimitOptions) (*RouteLimiter, error) {
	if options.RequestsPerMinute < 1 || options.Burst < 1 || options.MaxConcurrent < 1 || options.MaxConcurrent > 1024 || options.MaxClients < 1 || options.MaxClients > 100000 || options.IdleTTL <= 0 || options.IdleTTL.Seconds() < float64(options.Burst)*60/float64(options.RequestsPerMinute) {
		return nil, fmt.Errorf("invalid route limits: positive rate/burst, concurrency 1..1024, clients 1..100000, TTL at least bucket refill time required")
	}
	return &RouteLimiter{options: options, clients: make(map[string]clientBucket), slots: make(chan struct{}, options.MaxConcurrent), now: time.Now}, nil
}

// Middleware must run INSIDE RouteResponses: the slot belongs to actual work,
// and remains occupied until that handler exits even after the HTTP timeout.
func (l *RouteLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.Context().Err(); err != nil {
			writeFailure(w, err, generateErrors)
			return
		}
		requestlog.SetStage(r.Context(), "admission")
		retry := l.admit(clientAddress(r.RemoteAddr))
		if retry > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			Error(w, 429, "rate_limited", "Слишком много запросов маршрутизации; повторите позже")
			return
		}
		defer func() { <-l.slots }()
		if err := r.Context().Err(); err != nil {
			writeFailure(w, err, generateErrors)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RouteLimiter) admit(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.nextCleanup) || len(l.clients) >= l.options.MaxClients {
		for id, bucket := range l.clients {
			if now.Sub(bucket.lastSeen) >= l.options.IdleTTL {
				delete(l.clients, id)
			}
		}
		l.nextCleanup = now.Add(time.Minute)
	}
	bucket, exists := l.clients[key]
	if !exists {
		if len(l.clients) >= l.options.MaxClients {
			return 60
		}
		bucket = clientBucket{tokens: float64(l.options.Burst), updated: now, lastSeen: now}
	}
	elapsed := now.Sub(bucket.updated).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	bucket.tokens = math.Min(float64(l.options.Burst), bucket.tokens+elapsed*float64(l.options.RequestsPerMinute)/60)
	bucket.updated = now
	bucket.lastSeen = now
	if bucket.tokens < 1 {
		l.clients[key] = bucket
		return int(math.Max(1, math.Ceil((1-bucket.tokens)*60/float64(l.options.RequestsPerMinute))))
	}
	select {
	case l.slots <- struct{}{}:
		bucket.tokens--
		l.clients[key] = bucket
		return 0
	default:
		l.clients[key] = bucket
		return 1 // No queue and no consumed token on global overload.
	}
}
func clientAddress(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	return addr.Unmap().String()
}
