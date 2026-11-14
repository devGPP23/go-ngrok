package proxy

import (
	"errors"
)

/*
	🧠 Concept: Buffered Channel Semaphore
	Server par Rahul ka laptop connected hai. Agar koi visitor robot loop chala kar 10,000 parallel
	connections shoot kar de, toh Rahul ke laptop par 10,000 streams ek saath khulne ki koshish hongi
	aur laptop freeze ho jayega (Resource Exhaustion DoS Attack).

	🛠️ The Solution:
	Hum har tunnel ke liye ek `StreamSemaphore` banate hain.
	Go mein semaphore banane ka sabse fast aur memory-efficient tareeqa:
	`chan struct{}`!
	Kyunki `struct{}` Go runtime mein **0 bytes of memory** leti hai!

	- TryAcquire(): Non-blocking `select` check karta hai. Agar capacity bachi hai, token claim hota hai.
	  Agar tokens khatam (tunnel overloaded) -> Turant false return hota hai (Visitor ko 503 error milega).
	- Release(): Stream close hote hi token wapas channel se bahar nikalta hai.
*/

// ErrTunnelOverloaded returns when a tunnel has exceeded its max in-flight streams.
var ErrTunnelOverloaded = errors.New("tunnel concurrent stream limit reached")

// StreamSemaphore bounds the number of concurrent in-flight streams for a tunnel.
type StreamSemaphore struct {
	tokens chan struct{}
	max    int
}

// NewStreamSemaphore creates a semaphore with the specified maximum capacity.
func NewStreamSemaphore(maxConcurrent int) *StreamSemaphore {
	if maxConcurrent <= 0 {
		maxConcurrent = 100 // Safe production default (max 100 concurrent requests per tunnel)
	}
	return &StreamSemaphore{
		tokens: make(chan struct{}, maxConcurrent),
		max:    maxConcurrent,
	}
}

// TryAcquire attempts to claim a stream slot without blocking.
// Returns true if acquired, false if the tunnel is at maximum capacity.
func (s *StreamSemaphore) TryAcquire() bool {
	select {
	case s.tokens <- struct{}{}:
		return true // Token successfully claimed!
	default:
		return false // Channel full hai! Tunnel is at maximum capacity.
	}
}

// Release returns the claimed slot back to the semaphore pool.
func (s *StreamSemaphore) Release() {
	select {
	case <-s.tokens:
		// Token released successfully
	default:
		// Guard against double release (agar kisi ne galti se bina acquire kiye release call kiya)
	}
}

// Available returns how many stream slots are currently free.
func (s *StreamSemaphore) Available() int {
	return s.max - len(s.tokens)
}

// InUse returns how many stream slots are currently occupied.
func (s *StreamSemaphore) InUse() int {
	return len(s.tokens)
}
