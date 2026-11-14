package limiter

import (
	"testing"
	"time"
)

// TestBucket_BurstAndExhaustion verifies that capacity allows initial burst and then blocks.
func TestBucket_BurstAndExhaustion(t *testing.T) {
	capacity := 3.0
	refillRate := 10.0 // 10 tokens/sec
	b := NewBucket(capacity, refillRate)

	// Consume 3 tokens (burst allowed)
	if !b.Allow() || !b.Allow() || !b.Allow() {
		t.Fatalf("expected all 3 burst tokens to be allowed")
	}

	// 4th token should be rejected immediately
	if b.Allow() {
		t.Fatalf("expected 4th token to be rejected when bucket is empty")
	}
}

// TestBucket_RefillOverTime verifies that tokens refill accurately based on elapsed time.
func TestBucket_RefillOverTime(t *testing.T) {
	capacity := 2.0
	refillRate := 20.0 // 20 tokens per second -> 1 token every 50ms
	b := NewBucket(capacity, refillRate)

	// Exhaust bucket
	b.Allow()
	b.Allow()
	if b.Allow() {
		t.Fatalf("bucket should be empty")
	}

	// Sleep 100ms -> should refill ~2 tokens
	time.Sleep(100 * time.Millisecond)

	if !b.Allow() {
		t.Fatalf("expected token to be refilled after waiting 100ms")
	}
}

// TestRateLimiter_MultipleKeys verifies separate rate limit tracking per subdomain/IP.
func TestRateLimiter_MultipleKeys(t *testing.T) {
	rl := NewRateLimiter(2.0, 1.0)

	// User A consumes 2 tokens
	if !rl.AllowKey("user-a") || !rl.AllowKey("user-a") {
		t.Fatalf("user-a should be allowed 2 tokens")
	}
	if rl.AllowKey("user-a") {
		t.Fatalf("user-a should be rate limited")
	}

	// User B should NOT be affected by User A's rate limit!
	if !rl.AllowKey("user-b") {
		t.Fatalf("user-b should be allowed even if user-a is exhausted")
	}
}
