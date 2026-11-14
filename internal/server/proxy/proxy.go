/*
	Normal Reverse Proxy (jaise Nginx ya Caddy) kaise kaam karta hai?
	Jab Nginx ko request milti hai, toh woh backend tak ek naya TCP connection dial karta hai:
	Visitor -> Nginx -> net.Dial("tcp", "backend:3000") -> Backend

	Humare sath problem kya hai?
	Humara backend developer ke ghar ke laptop par hai, jo Wi-Fi Router / NAT ke peeche band hai.
	Server laptop ko net.Dial kar hi nahi sakta!

	The Genius Solution:
	Go standard library ka httputil.ReverseProxy internally ek http.Transport use karta hai.
	Aur http.Transport mein ek setting hoti hai: DialContext (jo decide karti hai ki backend se kaise connect hona hai).
	Hum is DialContext function ko hijack (override) kar dete hain!
	Normal ReverseProxy:
	  Transport.DialContext ──► net.Dial("tcp", ...) ──► New TCP Connection
	HAMARA ReverseProxy:
	  Transport.DialContext ──► tunnel.Session.OpenStream() ──► Yamux Virtual Stream!

	🔥 Day 8 Armor Additions:
	1. Token Bucket Rate Limiter: Har subdomain ke requests ko cap karta hai (e.g. 50 req/sec with burst).
	   Agar limit cross ho -> HTTP 429 Too Many Requests!
	2. Per-Tunnel Stream Semaphore: Har client laptop par active parallel streams ko cap karta hai (max 100).
	   Agar 101st stream aayi -> HTTP 503 Service Unavailable (Laptop crash hone se bachao).
	3. Context Cancellation: Agar visitor browser tab band kar de, request context turant cancel ho jata hai
	   aur Yamux stream cleanly terminate ho jati hai (zero wasted database queries on local app).
*/

package proxy

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"

	"github.com/apoorvslst/go-ngrok/internal/server/limiter"
	"github.com/apoorvslst/go-ngrok/internal/server/registry"
	"github.com/apoorvslst/go-ngrok/internal/server/router"
)

var (
	// DefaultRateLimiter: 50 requests/sec with burst capacity of 100
	defaultRateLimiter = limiter.NewRateLimiter(100.0, 50.0)

	// Har subdomain ke liye per-tunnel semaphore cache
	semaphoresMu sync.RWMutex
	semaphores   = make(map[string]*StreamSemaphore)
)

// GetTunnelSemaphore returns or initializes a StreamSemaphore for the specified subdomain.
func GetTunnelSemaphore(subdomain string, maxConcurrent int) *StreamSemaphore {
	semaphoresMu.RLock()
	sem, exists := semaphores[subdomain]
	semaphoresMu.RUnlock()
	if exists {
		return sem
	}

	semaphoresMu.Lock()
	defer semaphoresMu.Unlock()
	sem, exists = semaphores[subdomain]
	if !exists {
		sem = NewStreamSemaphore(maxConcurrent)
		semaphores[subdomain] = sem
	}
	return sem
}

// NewTunnelProxy creates an HTTP handler that proxies visitor requests through yamux streams
// with rate limiting, stream concurrency bounding, and graceful error handling.
func NewTunnelProxy(reg *registry.Registry, rootDomain string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Subdomain extract karo
		subdomain, err := router.ExtractSubdomain(r.Host, rootDomain)
		if err != nil {
			http.Error(w, "Invalid tunnel URL", http.StatusBadRequest)
			return
		}

		// 2. 🛡️ Step A: Rate Limiter Check (Token Bucket)
		// Agar visitor spam kar raha hai toh turant 429 Too Many Requests return karo
		if !defaultRateLimiter.AllowKey(subdomain) {
			slog.Warn("rate limit exceeded for subdomain", "subdomain", subdomain, "remote", r.RemoteAddr)
			w.Header().Set("Retry-After", "1")
			http.Error(w, "429 Too Many Requests: Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// 3. Check karo ki kya yeh tunnel online hai
		tunnel, ok := reg.Get(subdomain)
		if !ok {
			http.Error(w, "Tunnel not found or offline", http.StatusNotFound)
			return
		}

		// 4. Check karo ki yamux session abhi bhi zinda hai ya nahi
		if tunnel.Session.IsClosed() {
			reg.Delete(subdomain)
			http.Error(w, "Tunnel client disconnected", http.StatusBadGateway)
			return
		}

		// 5. 🛡️ Step B: Per-Tunnel Stream Semaphore (Concurrency bounding)
		// Ek laptop par ek time pe max 100 parallel streams allow karo taaki laptop freeze na ho!
		sem := GetTunnelSemaphore(subdomain, 100)
		if !sem.TryAcquire() {
			slog.Warn("tunnel stream limit reached",
				"subdomain", subdomain,
				"in_use", sem.InUse(),
				"max", 100,
			)
			http.Error(w, "503 Service Unavailable: Tunnel stream limit reached", http.StatusServiceUnavailable)
			return
		}
		// Request complete hone par semaphore slot release karo
		defer sem.Release()

		// 6. ReverseProxy configure karo custom DialContext ke saath
		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = "http"
				req.URL.Host = r.Host
				req.Host = r.Host // Original Host header preserve karo
			},
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					// Naya TCP dial karne ke bajaye Yamux virtual stream open karo!
					stream, err := tunnel.Session.OpenStream()
					if err != nil {
						slog.Error("failed to open yamux stream", "subdomain", subdomain, "err", err)
						return nil, err
					}

					// Context propagation: Agar visitor cancel/disconnect kare, stream close ho sake
					go func() {
						<-ctx.Done()
						_ = stream.Close()
					}()

					slog.Info("yamux stream opened for visitor request",
						"subdomain", subdomain,
						"stream_id", stream.StreamID(),
						"method", r.Method,
						"path", r.URL.Path,
					)
					return stream, nil
				},
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				// Agar client disconnect ya timeout hua ho
				slog.Error("proxy error", "subdomain", subdomain, "err", err)
				http.Error(w, "Bad Gateway: tunnel client failed to respond", http.StatusBadGateway)
			},
		}

		// 7. Request ko actually forward karo
		proxy.ServeHTTP(w, r)
	})
}