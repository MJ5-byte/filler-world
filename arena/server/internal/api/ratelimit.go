package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// limiter is a simple fixed-window per-IP rate limiter; good enough to stop
// upload/match spam from exhausting the worker pool.
type limiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string][]time.Time
	calls  uint64
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{window: window, max: max, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)

	// Periodically evict idle IPs so the map can't grow without bound.
	l.calls++
	if l.calls%512 == 0 {
		for k, ts := range l.hits {
			if len(ts) == 0 || !ts[len(ts)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}

	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, now)
	return true
}

func (l *limiter) middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !l.allow(ip) {
			http.Error(w, `{"error":"rate limit exceeded, try again later"}`, http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
