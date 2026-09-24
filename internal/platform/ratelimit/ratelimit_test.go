package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	l := New(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("hit %d refused", i+1)
		}
	}
	if l.Allow("a") {
		t.Fatal("4th hit allowed")
	}
	if !l.Allow("b") {
		t.Fatal("other key limited")
	}
	if !l.Blocked("a") {
		t.Fatal("a should be blocked")
	}

	now = now.Add(time.Minute)
	if l.Blocked("a") || !l.Allow("a") {
		t.Fatal("window did not reset")
	}

	l.Hit("c")
	l.Hit("c")
	l.Hit("c")
	if !l.Blocked("c") {
		t.Fatal("c should be blocked after 3 hits")
	}
	l.Reset("c")
	if l.Blocked("c") {
		t.Fatal("reset did not clear c")
	}
}
