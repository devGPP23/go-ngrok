# 📅 Day 9 — Production Observability, Prometheus Metrics & Live CLI Telemetry (Odin Project Style)

> **Rule**: Hum code direct inject nahi karenge. Hum batayenge KYA likhna hai, KIS file mein, KAUNSA function, KAUNSA concept pehle samjho. Har ek function tum khud likhoge.
>
> **Apoorv** = 🔵 | **Gaurav** = 🟢 | **Both** = ⚪

---

## 🗺️ Day 9 Overview: Where We Are & Where We Are Going

Production engineering ka golden rule hai:
> *"If you cannot measure it, you cannot operate it."*

Abhi tak hamara tunnel server aur client chalte hain, lekin:
- Server admin ko kaise pata chalega ki kitne tunnels active hain?
- Agar traffic spike aaye, toh 95th percentile latency ($p95$) kitni hai?
- Kis user ke subdomain pe sabse zyada `500` errors aa rahe hain?
- Developer ke terminal pe ngrok jaisa clean live dashboard kaise dikhega?

### Day 9 ka Mission:
1. **Server-Side Prometheus Metrics (Apoorv 🔵):**
   Standard Prometheus metrics exporter build karenge:
   - `ngrok_active_tunnels` (Gauge)
   - `ngrok_active_streams` (Gauge)
   - `ngrok_requests_total` (Counter by method, subdomain, status code)
   - `ngrok_request_duration_seconds` (Histogram with latency buckets)
   - Dedicated `/metrics` endpoint for Prometheus scraping.
2. **Client-Side Live Terminal Dashboard (Gaurav 🟢):**
   Terminal pe ngrok-style clean telemetry screen:
   - Tunnel status (Online / Reconnecting)
   - Target mapping: `https://rahul.tunnel.dev -> http://localhost:3000`
   - Real-time stats: Total Requests, Active Streams, Average Latency, Bytes In/Out.
3. **Structured Request Tracing with `request_id` (Both ⚪):**
   Har request ko ek unique UUID milega jo server aur client dono ke `slog` logs mein attach hoga taaki complete end-to-end trace mil sake!

```
                                  PROMETHEUS SCRAPER
                                          │
                                          ▼ (Scrapes GET /metrics)
Visitor ──► [ SERVER :8080 ] ──────► [ Prometheus Registry ]
                   │                  • Active Tunnels Gauge
                   │                  • Latency Histogram
                   ▼                  • Error Counters
            [ CLIENT CLI ]
                   │
                   ├──► Live Console Dashboard (Active Streams, QPS, Latency)
                   ▼
            localhost:3000
```

---

## 🔵 Step 1: The Core Systems Concept — Gauges, Counters & Histograms (Apoorv — 20 min)

### 🧠 The 3 Core Metric Types in Prometheus:

1. **Counter (Only goes UP):**
   - Example: Total requests served (`ngrok_requests_total`).
   - Counter kabhi decrease nahi hota (jab tak process restart na ho).
   - Prometheus ispe `rate()` function chalata hai to calculate Requests Per Second (RPS / QPS).
2. **Gauge (Can go UP and DOWN):**
   - Example: Active tunnels (`ngrok_active_tunnels`), Active in-flight streams.
   - Client connect hua $\rightarrow +1$. Client disconnect hua $\rightarrow -1$.
3. **Histogram (Measures Distribution & Latency):**
   - Example: Request duration in seconds (`ngrok_request_duration_seconds`).
   - Bins requests into buckets: $\le 10\text{ms}, \le 50\text{ms}, \le 250\text{ms}, \le 1\text{s}, \le 5\text{s}$.
   - Allows calculating **$p50$, $p90$, $p99$ tail latency**!

---

## 🔵 Step 2: Prometheus Metrics Collector & Middleware (Apoorv — 45 min)

### File: `internal/metrics/metrics.go`
### Package: `package metrics`

### Dependencies:
```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
```

### Metrics Definition:
```go
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ActiveTunnels tracks current connected developer tunnels
	ActiveTunnels = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ngrok_active_tunnels",
		Help: "Current number of active registered reverse tunnels",
	})

	// ActiveStreams tracks current in-flight HTTP streams across all tunnels
	ActiveStreams = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ngrok_active_streams",
		Help: "Current in-flight streams per subdomain",
	}, []string{"subdomain"})

	// RequestsTotal counts all visitor HTTP requests handled
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ngrok_requests_total",
		Help: "Total HTTP requests routed through tunnels",
	}, []string{"subdomain", "method", "status"})

	// RequestDuration measures request round-trip latency distribution
	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ngrok_request_duration_seconds",
		Help:    "Latency distribution of requests in seconds",
		Buckets: []float64{0.005, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
	}, []string{"subdomain", "status"})

	// BytesTransferred tracks total bandwidth transferred
	BytesTransferred = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ngrok_bytes_transferred_total",
		Help: "Total bytes transferred through the tunnel",
	}, []string{"subdomain", "direction"})
)

// ResponseWriterWrapper intercepts status code and bytes written
type ResponseWriterWrapper struct {
	http.ResponseWriter
	StatusCode   int
	BytesWritten int64
}

func NewResponseWriterWrapper(w http.ResponseWriter) *ResponseWriterWrapper {
	return &ResponseWriterWrapper{ResponseWriter: w, StatusCode: http.StatusOK}
}

func (rw *ResponseWriterWrapper) WriteHeader(code int) {
	rw.StatusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *ResponseWriterWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.BytesWritten += int64(n)
	return n, err
}

// InstrumentHandler records telemetry for an HTTP request
func InstrumentHandler(subdomain string, method string, duration time.Duration, status int, bytesIn int64, bytesOut int64) {
	statusStr := strconv.Itoa(status)
	durationSec := duration.Seconds()

	RequestsTotal.WithLabelValues(subdomain, method, statusStr).Inc()
	RequestDuration.WithLabelValues(subdomain, statusStr).Observe(durationSec)
	BytesTransferred.WithLabelValues(subdomain, "in").Add(float64(bytesIn))
	BytesTransferred.WithLabelValues(subdomain, "out").Add(float64(bytesOut))
}
```

---

## 🟢 Step 3: Client Live Terminal Dashboard (Gaurav — 40 min)

### File: `internal/client/telemetry/dashboard.go`
### Package: `package telemetry`

### Concept:
Rahul apne terminal par jab client chalata hai, toh terminal clean hona chahiye aur statistics live update hone chahiye:
```text
============================================================
  Go-Ngrok Client (v1.0.0) — Tunnel is ONLINE
============================================================
  Public URL:       https://rahul.tunnel.dev
  Forwarding To:    http://localhost:3000
  Status:           Active (Connected to port :9000)
------------------------------------------------------------
  Telemetry:
  Total Requests:   142
  Active Streams:   3
  Avg Latency:      18ms
  Bytes Recv/Sent:  1.2 MB / 8.4 MB
============================================================
```

### Dashboard Struct & Live Display:
```go
package telemetry

import (
	"fmt"
	"sync/atomic"
	"time"
)

// ClientStats tracks real-time metrics on Rahul's laptop
type ClientStats struct {
	TotalRequests int64
	ActiveStreams int64
	TotalBytesIn  int64
	TotalBytesOut int64
	LastLatencyMs int64
}

type TerminalDashboard struct {
	Subdomain string
	TargetURL string
	ServerURL string
	Stats     *ClientStats
}

func NewTerminalDashboard(subdomain, targetURL, serverURL string) *TerminalDashboard {
	return &TerminalDashboard{
		Subdomain: subdomain,
		TargetURL: targetURL,
		ServerURL: serverURL,
		Stats:     &ClientStats{},
	}
}

func (td *TerminalDashboard) RecordRequest(latencyMs int64, bytesIn, bytesOut int64) {
	atomic.AddInt64(&td.Stats.TotalRequests, 1)
	atomic.AddInt64(&td.Stats.TotalBytesIn, bytesIn)
	atomic.AddInt64(&td.Stats.TotalBytesOut, bytesOut)
	atomic.StoreInt64(&td.Stats.LastLatencyMs, latencyMs)
}

func (td *TerminalDashboard) RenderBanner() {
	fmt.Printf("\n============================================================\n")
	fmt.Printf("  Go-Ngrok Tunnel: https://%s.tunnel.dev\n", td.Subdomain)
	fmt.Printf("  Forwarding to:   %s\n", td.TargetURL)
	fmt.Printf("  Inspector Web:   http://localhost:4040\n")
	fmt.Printf("============================================================\n\n")
}
```

---

## ⚪ Step 4: Exposing `/metrics` Endpoint & End-to-End Test (Both — 35 min)

1. **Server Main:**
   Server ke `:9000` ya internal port `:9090` pe `promhttp.Handler()` mount karo:
   ```go
   http.Handle("/metrics", promhttp.Handler())
   ```
2. **Verification with `curl`:**
   ```bash
   curl http://localhost:9090/metrics
   ```
   Verify outputs:
   ```text
   # HELP ngrok_active_tunnels Current number of active registered reverse tunnels
   # TYPE ngrok_active_tunnels gauge
   ngrok_active_tunnels 1

   # HELP ngrok_requests_total Total HTTP requests routed through tunnels
   # TYPE ngrok_requests_total counter
   ngrok_requests_total{method="GET",status="200",subdomain="apoorv"} 12
   ```

