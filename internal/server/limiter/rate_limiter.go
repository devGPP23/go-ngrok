package limiter

import (
	"sync"
	"time"
)

/*
	🧠 Concept: Token Bucket Rate Limiter
	Kyun simple Fixed Window kaam nahi karta?
	Agar hum kahein "1 second mein 50 requests allow karo":
	Visitor 0.99 second par 50 requests bhejta hai aur 1.01 second par dobara 50 requests.
	0.02 second ke interval mein 100 requests ek saath aagayi! Isse "Boundary Burst Attack" kehte hain.

	🛠️ The Solution: Token Bucket Algorithm
	1. Ek bucket hoti hai jiski capacity hoti hai (e.g. 50 tokens).
	2. Har second bucket mein continuously tokens refill hote hain (refillRateSec, e.g. 10 tokens/sec).
	3. Har request aane par hum check karte hain:
	   - Time elapsed kitna hua? Us hisab se naye tokens bucket mein add karo.
	   - Agar tokens >= 1 hai, 1 token consume karo aur request allow karo.
	   - Agar bucket empty hai, return false -> HTTP 429 Too Many Requests!
*/

// Bucket represents an individual token bucket for a visitor IP or subdomain.
type Bucket struct {
	tokens         float64
	capacity       float64
	refillRateSec  float64
	lastRefillTime time.Time
	mu             sync.Mutex
}

// NewBucket initializes a bucket with full capacity.
func NewBucket(capacity float64, refillRateSec float64) *Bucket {
	return &Bucket{
		tokens:         capacity,
		capacity:       capacity,
		refillRateSec:  refillRateSec,
		lastRefillTime: time.Now(),
	}
}

// Allow checks if 1 token can be consumed.
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefillTime).Seconds()
	b.lastRefillTime = now

	// 1. Time elapsed ke mutabik bucket mein naye tokens refill karo
	b.tokens += elapsed * b.refillRateSec
	if b.tokens > b.capacity {
		b.tokens = b.capacity // Capacity se zyada tokens overflow nahi hone chahiye
	}

	// 2. Token consume karne ki koshish karo
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true // Request allowed!
	}

	// Bucket khaali hai!
	return false
}

// RateLimiter manages buckets for multiple subdomains or IP addresses.
type RateLimiter struct {
	mu            sync.RWMutex
	buckets       map[string]*Bucket
	capacity      float64
	refillRateSec float64
}

// NewRateLimiter creates a multi-key rate limiter manager.
func NewRateLimiter(capacity float64, refillRateSec float64) *RateLimiter {
	return &RateLimiter{
		buckets:       make(map[string]*Bucket),
		capacity:      capacity,
		refillRateSec: refillRateSec,
	}
}

// AllowKey checks if the key (subdomain or IP) is allowed to make a request.
func (rl *RateLimiter) AllowKey(key string) bool {
	// First check with Read Lock (Fast path for existing keys)
	rl.mu.RLock()
	bucket, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// Double-checked locking: lock lene ke baad dobara check karo taaki race condition me do buckets na banein
		bucket, exists = rl.buckets[key]
		if !exists {
			bucket = NewBucket(rl.capacity, rl.refillRateSec)
			rl.buckets[key] = bucket
		}
		rl.mu.Unlock()
	}

	return bucket.Allow()
}
