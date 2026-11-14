package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/apoorvslst/go-ngrok/internal/server/middleware"
	"github.com/apoorvslst/go-ngrok/internal/server/proxy"
	"github.com/apoorvslst/go-ngrok/internal/server/registry"
)

/*
 REAL-WORLD EXAMPLE: The Public Front Gate (Public HTTP Listener)

WHO IS WHO?
1. CLIENT:
   - Your physical laptop sitting at home or in an office.
   - Running your web app locally (e.g. Next.js on localhost:3000).
   - Running ./cmd/client which connected to Server's Port :9000 and stays connected.

2. SERVER (This Program):
   - The public cloud machine (like AWS EC2) with a real public IP address.
   - Has TWO doors:
       Door 1 (Port :9000): Private control line where laptops (clients) connect.
       Door 2 (Port :8080): Public front gate where anyone on the internet arrives.

3. VISITOR:
   - A friend, a customer, or a Stripe payment webhook sitting anywhere on earth.
   - Opens their browser and types: "http://apoorv.tunnel.dev:8080/pay"
   - They CANNOT see your laptop directly; they only talk to the Server on Port :8080.

WHAT HAPPENS AT THIS PUBLIC LISTENER?
1. Visitor knocks on Port :8080 asking for "apoorv.tunnel.dev:8080/pay".
2. Panic Recovery Middleware intercepts to protect server from unexpected crashes.
3. Token Bucket Rate Limiter ensures visitor doesn't flood the server.
4. Stream Semaphore prevents client laptop overload.
5. Pipe the HTTP request directly into laptop via Yamux stream!
*/

// StartPublicListener starts the HTTP server that receives visitor requests.
func StartPublicListener(addr string, reg *registry.Registry, rootDomain string) error {
	// 1. Create the reverse proxy handler with Yamux DialContext trick
	proxyHandler := proxy.NewTunnelProxy(reg, rootDomain)

	// 2. 🛡️ Wrap with Panic Recovery Middleware taaki koi bhi handler crash na hone de
	protectedHandler := middleware.Recoverer(proxyHandler)

	slog.Info("public HTTP listener starting with abuse controls & recovery", "addr", addr)
	return http.ListenAndServe(addr, protectedHandler)
}