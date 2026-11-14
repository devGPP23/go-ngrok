package telemetry

import (
	"fmt"
	"sync/atomic"
)

/*
	🧠 Concept: Live Terminal Dashboard (Ngrok Style)
	Developer jab `go run ./cmd/client` run karta hai, toh terminal par ek clean,
	informative dashboard display hona chahiye:
	- Public Tunnel URL: https://gaurav.tunnel.dev
	- Local Web App Forwarding: http://localhost:3000
	- Inspector Web Interface: http://localhost:4040
	- Real-time stats: Total requests, Active streams, Latency, Data transferred.

	Sabhi metrics thread-safe atomic counters (`sync/atomic`) se track hote hain,
	taaki multiple concurrent streams ke dauraan bhi koi data race na ho!
*/

// ClientStats tracks real-time metrics on the developer's machine using atomic operations.
type ClientStats struct {
	TotalRequests int64
	ActiveStreams int64
	TotalBytesIn  int64
	TotalBytesOut int64
	LastLatencyMs int64
}

// TerminalDashboard manages the terminal UI and live telemetry reporting.
type TerminalDashboard struct {
	Subdomain string
	TargetURL string
	ServerURL string
	Stats     *ClientStats
}

// NewTerminalDashboard creates a new terminal telemetry manager.
func NewTerminalDashboard(subdomain, targetURL, serverURL string) *TerminalDashboard {
	return &TerminalDashboard{
		Subdomain: subdomain,
		TargetURL: targetURL,
		ServerURL: serverURL,
		Stats:     &ClientStats{},
	}
}

// RecordRequest updates request metrics atomically after a request finishes.
func (td *TerminalDashboard) RecordRequest(latencyMs int64, bytesIn, bytesOut int64) {
	atomic.AddInt64(&td.Stats.TotalRequests, 1)
	atomic.AddInt64(&td.Stats.TotalBytesIn, bytesIn)
	atomic.AddInt64(&td.Stats.TotalBytesOut, bytesOut)
	atomic.StoreInt64(&td.Stats.LastLatencyMs, latencyMs)
}

// IncActiveStreams increments active stream count.
func (td *TerminalDashboard) IncActiveStreams() {
	atomic.AddInt64(&td.Stats.ActiveStreams, 1)
}

// DecActiveStreams decrements active stream count.
func (td *TerminalDashboard) DecActiveStreams() {
	atomic.AddInt64(&td.Stats.ActiveStreams, -1)
}

// RenderBanner outputs the clean Ngrok-style status banner to terminal.
func (td *TerminalDashboard) RenderBanner() {
	fmt.Printf("\n============================================================\n")
	fmt.Printf("  🚀 Go-Ngrok Tunnel:  https://%s.tunnel.dev\n", td.Subdomain)
	fmt.Printf("  Forwarding to:       %s\n", td.TargetURL)
	fmt.Printf("  Inspector Web:       http://localhost:4040\n")
	fmt.Printf("  Server Gateway:      %s\n", td.ServerURL)
	fmt.Printf("============================================================\n")
	fmt.Printf("  Live Telemetry:\n")
	fmt.Printf("  Total Requests:      %d\n", atomic.LoadInt64(&td.Stats.TotalRequests))
	fmt.Printf("  Active Streams:      %d\n", atomic.LoadInt64(&td.Stats.ActiveStreams))
	fmt.Printf("  Last Latency:        %dms\n", atomic.LoadInt64(&td.Stats.LastLatencyMs))
	fmt.Printf("============================================================\n\n")
}
