package telemetry

import (
	"sync"
	"testing"
)

// TestTerminalDashboard_RecordRequest verifies atomic updates on client statistics.
func TestTerminalDashboard_RecordRequest(t *testing.T) {
	td := NewTerminalDashboard("gaurav", "http://localhost:3000", "localhost:9000")

	td.RecordRequest(25, 100, 500)
	td.RecordRequest(35, 200, 800)

	if td.Stats.TotalRequests != 2 {
		t.Errorf("expected 2 requests, got %d", td.Stats.TotalRequests)
	}
	if td.Stats.TotalBytesIn != 300 {
		t.Errorf("expected 300 bytes in, got %d", td.Stats.TotalBytesIn)
	}
	if td.Stats.TotalBytesOut != 1300 {
		t.Errorf("expected 1300 bytes out, got %d", td.Stats.TotalBytesOut)
	}
	if td.Stats.LastLatencyMs != 35 {
		t.Errorf("expected last latency 35ms, got %d", td.Stats.LastLatencyMs)
	}

	td.IncActiveStreams()
	td.IncActiveStreams()
	if td.Stats.ActiveStreams != 2 {
		t.Errorf("expected 2 active streams, got %d", td.Stats.ActiveStreams)
	}

	td.DecActiveStreams()
	if td.Stats.ActiveStreams != 1 {
		t.Errorf("expected 1 active stream, got %d", td.Stats.ActiveStreams)
	}
}

// TestTerminalDashboard_Concurrent verifies atomic thread safety under parallel requests.
func TestTerminalDashboard_Concurrent(t *testing.T) {
	td := NewTerminalDashboard("gaurav", "http://localhost:3000", "localhost:9000")
	var wg sync.WaitGroup

	iterations := 100
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			td.RecordRequest(10, 5, 15)
		}()
	}

	wg.Wait()

	if td.Stats.TotalRequests != int64(iterations) {
		t.Errorf("expected %d requests, got %d", iterations, td.Stats.TotalRequests)
	}
	if td.Stats.TotalBytesIn != int64(iterations*5) {
		t.Errorf("expected %d bytes in, got %d", iterations*5, td.Stats.TotalBytesIn)
	}
}

// TestTerminalDashboard_RenderBanner verifies RenderBanner does not panic.
func TestTerminalDashboard_RenderBanner(t *testing.T) {
	td := NewTerminalDashboard("gaurav", "http://localhost:3000", "localhost:9000")
	td.RecordRequest(14, 50, 100)
	td.RenderBanner() // Should print cleanly to stdout without errors
}
