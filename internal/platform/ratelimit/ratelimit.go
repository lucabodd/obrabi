// Package ratelimit provides a tiny in-memory fixed-window limiter, enough for
// a single-instance deployment (login attempts, telemetry floods).
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows at most Limit hits per key in each Window.
type Limiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	buckets   map[string]*bucket
	lastSweep time.Time
	now       func() time.Time
}

type bucket struct {
	start time.Time
	count int
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, buckets: map[string]*bucket{}, now: time.Now}
}

// current returns the live bucket for key, resetting it when its window has
// passed. Callers hold l.mu.
func (l *Limiter) current(key string) *bucket {
	now := l.now()
	if now.Sub(l.lastSweep) > l.window {
		for k, b := range l.buckets {
			if now.Sub(b.start) >= l.window {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[key]
	if !ok || now.Sub(b.start) >= l.window {
		b = &bucket{start: now}
		l.buckets[key] = b
	}
	return b
}

// Allow records a hit and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.current(key)
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// Blocked reports whether key has used up its hits, without recording one.
func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current(key).count >= l.limit
}

// Hit records a hit without checking the limit.
func (l *Limiter) Hit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.current(key).count++
}

// Reset forgets the hits of key.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}
