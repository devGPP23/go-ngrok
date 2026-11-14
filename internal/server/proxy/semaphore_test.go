package proxy

import (
	"sync"
	"testing"
)

// TestStreamSemaphore_MaxCapacity verifies that TryAcquire bounds concurrent streams.
func TestStreamSemaphore_MaxCapacity(t *testing.T) {
	capacity := 5
	sem := NewStreamSemaphore(capacity)

	if sem.Available() != 5 || sem.InUse() != 0 {
		t.Fatalf("initial state mismatch: avail=%d, inuse=%d", sem.Available(), sem.InUse())
	}

	// 1. Acquire exactly 5 slots
	for i := 0; i < capacity; i++ {
		if !sem.TryAcquire() {
			t.Fatalf("expected acquire %d to succeed", i)
		}
	}

	if sem.Available() != 0 || sem.InUse() != 5 {
		t.Fatalf("full state mismatch: avail=%d, inuse=%d", sem.Available(), sem.InUse())
	}

	// 2. 6th attempt must fail non-blocking
	if sem.TryAcquire() {
		t.Fatalf("expected 6th acquire to fail when semaphore is full")
	}

	// 3. Release 1 token and verify 1 new acquire succeeds
	sem.Release()
	if sem.Available() != 1 || sem.InUse() != 4 {
		t.Fatalf("after release mismatch: avail=%d, inuse=%d", sem.Available(), sem.InUse())
	}

	if !sem.TryAcquire() {
		t.Fatalf("expected acquire to succeed after release")
	}

	// 4. Test safe double release guard
	sem.Release()
	sem.Release() // Extra release should not panic
}

// TestStreamSemaphore_Concurrent verifies thread-safety under concurrent load.
func TestStreamSemaphore_Concurrent(t *testing.T) {
	sem := NewStreamSemaphore(10)
	var wg sync.WaitGroup

	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sem.TryAcquire() {
				mu.Lock()
				successCount++
				mu.Unlock()
				sem.Release()
			}
		}()
	}

	wg.Wait()
	if successCount == 0 {
		t.Fatalf("expected at least some acquires to succeed concurrently")
	}
}
