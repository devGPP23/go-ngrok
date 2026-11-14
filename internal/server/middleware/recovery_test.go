package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRecoverer_RecoversPanic verifies that panics in handlers don't crash the server.
func TestRecoverer_RecoversPanic(t *testing.T) {
	// A handler that panics
	panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("nil pointer dereference or crash simulated")
	})

	// Wrap with Recoverer
	protected := Recoverer(panickingHandler)

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rr := httptest.NewRecorder()

	// This should NOT crash the test process!
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d", rr.Code)
	}
}

// TestRecoverer_NormalExecution verifies normal handlers execute uninterrupted.
func TestRecoverer_NormalExecution(t *testing.T) {
	normalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("all good"))
	})

	protected := Recoverer(normalHandler)

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rr := httptest.NewRecorder()

	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
	if rr.Body.String() != "all good" {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}
