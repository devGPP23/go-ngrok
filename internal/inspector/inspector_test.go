package inspector

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRingBuffer_AddAndGetAll tests that RingBuffer maintains FIFO cap and reverse chronological order.
func TestRingBuffer_AddAndGetAll(t *testing.T) {
	rb := NewRingBuffer(3)

	// 1. Ek record add karo
	r1 := &RequestRecord{ID: "req-1", Path: "/one"}
	rb.Add(r1)

	records := rb.GetAll()
	if len(records) != 1 || records[0].ID != "req-1" {
		t.Fatalf("expected 1 record 'req-1', got %v", records)
	}

	// 2. Do aur records add karo (total 3 = capacity reached)
	r2 := &RequestRecord{ID: "req-2", Path: "/two"}
	r3 := &RequestRecord{ID: "req-3", Path: "/three"}
	rb.Add(r2)
	rb.Add(r3)

	records = rb.GetAll()
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	// Reverse chronological: Newest first -> req-3, req-2, req-1
	if records[0].ID != "req-3" || records[1].ID != "req-2" || records[2].ID != "req-1" {
		t.Fatalf("expected [req-3, req-2, req-1], got [%s, %s, %s]",
			records[0].ID, records[1].ID, records[2].ID)
	}

	// 3. 4th record add karo: req-1 overwrite ho jana chahiye!
	r4 := &RequestRecord{ID: "req-4", Path: "/four"}
	rb.Add(r4)

	records = rb.GetAll()
	if len(records) != 3 {
		t.Fatalf("expected strictly 3 records after wrap-around, got %d", len(records))
	}
	// Newest first -> req-4, req-3, req-2
	if records[0].ID != "req-4" || records[1].ID != "req-3" || records[2].ID != "req-2" {
		t.Fatalf("expected [req-4, req-3, req-2], got [%s, %s, %s]",
			records[0].ID, records[1].ID, records[2].ID)
	}

	// 4. GetByID check karo
	found, ok := rb.GetByID("req-3")
	if !ok || found.Path != "/three" {
		t.Fatalf("expected to find req-3 with path /three, got %v, %v", found, ok)
	}

	_, ok = rb.GetByID("req-1") // req-1 overwrite ho chuki hai
	if ok {
		t.Fatalf("req-1 should have been overwritten and not found")
	}
}

// TestCaptureRequest_RestoresBody verifies that CaptureRequest reads up to cap and restores stream.
func TestCaptureRequest_RestoresBody(t *testing.T) {
	originalContent := "hello world from webhook test payload"
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/webhook", strings.NewReader(originalContent))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom-Header", "test-value")

	// Capture karo with 1MB limit
	record, err := CaptureRequest(req, 1024*1024)
	if err != nil {
		t.Fatalf("CaptureRequest failed: %v", err)
	}

	if record.Method != http.MethodPost {
		t.Errorf("expected POST method, got %s", record.Method)
	}
	if record.Path != "/webhook" {
		t.Errorf("expected /webhook path, got %s", record.Path)
	}
	if record.Headers["X-Custom-Header"] != "test-value" {
		t.Errorf("missing header in record: %v", record.Headers)
	}
	if record.RequestBody != originalContent {
		t.Errorf("body mismatch: got %q, want %q", record.RequestBody, originalContent)
	}
	if record.IsTruncated {
		t.Errorf("expected not truncated, got truncated")
	}

	// 🔥 Sabse zaroori: downstream reader ke liye body intact honi chahiye!
	restoredBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read restored req.Body: %v", err)
	}
	if string(restoredBytes) != originalContent {
		t.Fatalf("restored body mismatch! Downstream would get corrupted data: got %q, want %q",
			string(restoredBytes), originalContent)
	}
}

// TestCaptureRequest_CappedTruncation tests that bodies larger than cap are marked truncated.
func TestCaptureRequest_CappedTruncation(t *testing.T) {
	largeContent := strings.Repeat("A", 100) // 100 bytes
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/upload", strings.NewReader(largeContent))

	// Cap at 10 bytes
	record, err := CaptureRequest(req, 10)
	if err != nil {
		t.Fatalf("CaptureRequest failed: %v", err)
	}

	if !record.IsTruncated {
		t.Fatalf("expected record to be marked truncated")
	}
	if len(record.RequestBody) != 10 {
		t.Fatalf("expected captured body length 10, got %d", len(record.RequestBody))
	}

	// Downstream read should still read ALL 100 bytes!
	allBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read downstream body: %v", err)
	}
	if len(allBytes) != 100 {
		t.Fatalf("downstream body should receive all 100 bytes, got %d", len(allBytes))
	}
}

// TestReplayRequest verifies that ReplayRequest sends an identical request to the target server.
func TestReplayRequest(t *testing.T) {
	receivedMethod := ""
	receivedPath := ""
	receivedBody := ""
	receivedReplayedHeader := ""

	// Local dummy backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedReplayedHeader = r.Header.Get("X-GoNgrok-Replayed")
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))
	defer backend.Close()

	record := &RequestRecord{
		ID:          "rec-123",
		Method:      http.MethodPost,
		Path:        "/api/checkout",
		Headers:     map[string]string{"Content-Type": "application/json"},
		RequestBody: `{"order_id":"999"}`,
	}

	resp, duration, err := ReplayRequest(record, backend.URL)
	if err != nil {
		t.Fatalf("ReplayRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 Created, got %d", resp.StatusCode)
	}
	if duration < 0 {
		t.Errorf("duration should be positive, got %d", duration)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if receivedPath != "/api/checkout" {
		t.Errorf("expected /api/checkout, got %s", receivedPath)
	}
	if receivedBody != `{"order_id":"999"}` {
		t.Errorf("body mismatch: got %s", receivedBody)
	}
	if receivedReplayedHeader != "true" {
		t.Errorf("expected X-GoNgrok-Replayed: true header")
	}
}

// TestInspectorServer_Endpoints verifies /api/requests and /api/replay HTTP APIs.
func TestInspectorServer_Endpoints(t *testing.T) {
	// 1. Setup local backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok from backend"))
	}))
	defer backend.Close()

	storage := NewRingBuffer(10)
	server := NewServer(":0", storage, backend.URL)
	handler := server.Handler()

	// 2. Initially empty requests API
	req := httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var emptyList []*RequestRecord
	if err := json.Unmarshal(rr.Body.Bytes(), &emptyList); err != nil {
		t.Fatalf("failed to decode empty list: %v", err)
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected 0 records, got %d", len(emptyList))
	}

	// 3. Add a record and test /api/requests
	rec := &RequestRecord{
		ID:          "test-id-1",
		Method:      "GET",
		Path:        "/ping",
		Headers:     map[string]string{"Accept": "text/plain"},
		RequestBody: "",
	}
	storage.Add(rec)

	req = httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var list []*RequestRecord
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "test-id-1" {
		t.Fatalf("expected record test-id-1, got %v", list)
	}

	// 4. Test /api/replay
	req = httptest.NewRequest(http.MethodPost, "/api/replay?id=test-id-1", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on replay, got %d: %s", rr.Code, rr.Body.String())
	}

	// 5. Test GET / (HTML dashboard)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on dashboard, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "GoNgrok Traffic Inspector") {
		t.Fatalf("dashboard missing title keyword")
	}
}
