# 📅 Day 8 — Abuse Controls, Stream Semaphores, Rate Limiting & Context Cancellation (Odin Project Style)

> **Rule**: Hum code direct inject nahi karenge. Hum batayenge KYA likhna hai, KIS file mein, KAUNSA function, KAUNSA concept pehle samjho. Har ek function tum khud likhoge.
>
> **Apoorv** = 🔵 | **Gaurav** = 🟢 | **Both** = ⚪

---

## 🗺️ Day 8 Overview: Where We Are & Where We Are Going

Day 7 tak humara tunnel aur traffic inspector ready hai.
Lekin socho: **Agar hum ise public internet pe deploy karein, toh yeh 5 minute mein crash ho sakta hai!**

### The Attack Scenarios:
1. **The Spammer / DoS Attack:**
   Koi script kiddie `curl` ka loop chala kar Rahul ke tunnel pe 10,000 requests per second bhejta hai.
   Rahul ka laptop freeze ho jayega aur server ke sare network sockets exhaust ho jayenge!
2. **The Slowloris / Leaked Connection Attack:**
   Ek visitor request bhejta hai lekin browser tab turant band kar deta hai.
   Agar humne `context.Context` cancellation propagate nahi kiya, toh Rahul ka local backend (`localhost:3000`) us mare hue visitor ke liye heavy database queries run karta rahega!
3. **The Nil Pointer Panic:**
   Agar kisi third-party visitor ne malformed header bheja aur hamara code panic kar gaya, toh **poora server crash** ho jayega aur baki 1,000 developers ke tunnels disconnect ho jayenge!

### Day 8 ka Mission:
Hum apne tunnel server ko **bulletproof production armor** pehnayenge:
1. **Per-Tunnel Stream Semaphore (Apoorv 🔵):** Har client ke active concurrent streams ko cap karenge (e.g. max 100 streams). Agar 101st request aaye, toh server use `503 Service Unavailable` return karega bina client ko crash kiye.
2. **Token Bucket Rate Limiter (Gaurav 🟢):** Token bucket algorithm use karke per-subdomain aur per-IP rate limiting lagayenge (e.g. 50 req/sec with burst of 100).
3. **Context Propagation & Panic Recovery (Both ⚪):** Jab visitor disconnect ho, toh cancel signal Yamux stream aur client ke through seedha `localhost:3000` tak pahuchega, aur `recover()` middleware har crash ko shield karega.

```
Visitor Request
       │
       ▼
 [ Panic Recovery Middleware ] ──── (Protects entire server from crashing)
       │
       ▼
 [ Token Bucket Rate Limiter ] ──── (Capped at 50 req/sec) ──► 429 Too Many Requests
       │
       ▼
 [ Stream Semaphore Buffer ]   ──── (Capped at 100 streams) ─► 503 Overloaded
       │
       ▼
 [ Context Propagation ]       ──── (Visitor cancels -> Stream cancels instantly)
       │
       ▼
  Yamux Stream ──► Rahul's Laptop
```

---

## 🔵 Step 1: The Core Concurrency Pattern — Buffered Channel Semaphore (Apoorv — 20 min)

### 🧠 The Mental Model:
Go mein concurrency ko limit karne ka sabse fast aur memory-efficient tareeqa kya hai?
**A Buffered Channel of Empty Structs `chan struct{}`!**

Kyunki `struct{}` Go mein **0 bytes of RAM** leti hai!

```
Semaphore = make(chan struct{}, 100) // 100 Tokens available
```

1. Jab visitor request aati hai:
   `Semaphore <- struct{}{}` (Token claim karo).
   - Agar 100 se kam active streams hain, yeh instantly pass ho jata hai.
   - Agar already 100 streams chal rahi hain, toh hum block hone ke bajaye `select` with `default` use karte hain aur turant visitor ko `503 Service Unavailable` bhejte hain!
2. Jab request finish hoti hai:
   `<-Semaphore` (Token wapas return karo).

---

## 🔵 Step 2: Per-Tunnel Stream Semaphore Implementation (Apoorv — 35 min)

### File: `internal/server/proxy/semaphore.go`
### Package: `package proxy`

### Struct & Methods:
```go
package proxy

import (
	"errors"
	"sync"
)

var ErrTunnelOverloaded = errors.New("tunnel concurrent stream limit reached")

// StreamSemaphore bounds the number of concurrent in-flight streams for a tunnel.
type StreamSemaphore struct {
	tokens chan struct{}
	max    int
}

// NewStreamSemaphore creates a semaphore with the specified maximum capacity.
func NewStreamSemaphore(maxConcurrent int) *StreamSemaphore {
	if maxConcurrent <= 0 {
		maxConcurrent = 100 // Safe production default
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
		return true
	default:
		return false // Tunnel is full!
	}
}

// Release returns the claimed slot back to the semaphore pool.
func (s *StreamSemaphore) Release() {
	select {
	case <-s.tokens:
	default:
		// Guard against double release
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
```

---

## 🟢 Step 3: The Systems Concept — Token Bucket Rate Limiting (Gaurav — 20 min)

### 🧠 Why Fixed Window Rate Limiting Fails:
Agar tum socho: *"Har 1 second mein 50 requests allow karo"*:
- Visitor 0.99th second pe 50 requests bhejta hai.
- Wahi visitor 1.01th second pe dobara 50 requests bhejta hai.
- **Problem:** 0.02 seconds ke window mein **100 requests ek saath aagayi!** Isse bolte hain **Boundary Burst Attack**.

### 🛠️ The Solution: Token Bucket Algorithm
1. Bucket ki ek max capacity hoti hai (e.g. `Burst = 50 tokens`).
2. Bucket mein lagataar ek steady speed se tokens drop hote hain (e.g. `Rate = 10 tokens / second`).
3. Har incoming request bucket se 1 token leti hai.
   - Agar token hai $\rightarrow$ Request allowed!
   - Agar bucket khaali hai $\rightarrow$ `429 Too Many Requests`!

---

## 🟢 Step 4: Token Bucket Rate Limiter Implementation (Gaurav — 40 min)

### File: `internal/server/limiter/rate_limiter.go`
### Package: `package limiter`

### Struct & Methods:
```go
package limiter

import (
	"sync"
	"time"
)

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

	// Refill tokens based on time elapsed
	b.tokens += elapsed * b.refillRateSec
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}

	// Try to consume 1 token
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}

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
	rl.mu.RLock()
	bucket, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// Double-checked locking
		bucket, exists = rl.buckets[key]
		if !exists {
			bucket = NewBucket(rl.capacity, rl.refillRateSec)
			rl.buckets[key] = bucket
		}
		rl.mu.Unlock()
	}

	return bucket.Allow()
}
```

---

## 🔵 Step 5: Panic Recovery & Context Cancellation Middleware (Apoorv — 30 min)

### File: `internal/server/middleware/recovery.go`
### Package: `package middleware`

```go
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recoverer catches any panics in HTTP handlers, logs the stack trace,
// and returns a clean 500 Internal Server Error without killing the server process.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				slog.Error("panic recovered in HTTP handler",
					"error", rvr,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
```

---

## ⚪ Step 6: Integrating Abuse Controls into Proxy & Live Stress Test (Both — 35 min)

1. Server ke `NewTunnelProxy` mein:
   - Check Rate Limiter: If `!limiter.AllowKey(subdomain)` $\rightarrow$ return `429 Too Many Requests`.
   - Check Semaphore: If `!tunnel.Semaphore.TryAcquire()` $\rightarrow$ return `503 Service Unavailable`.
   - `defer tunnel.Semaphore.Release()`
2. Unit tests likho:
   - `TestStreamSemaphore_MaxCapacity` (Burst 100 pass, 101st fails).
   - `TestRateLimiter_TokenRefill` (Verify token refill over time).
   - `TestRecovery_NoCrash` (Simulate panic handler, ensure server survives).

