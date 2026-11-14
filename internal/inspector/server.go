package inspector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

/*
	🧠 Concept: Inspector HTTP Server & Single-Binary UI
	Developer ke laptop par ek local port (e.g. :4040) par web server chalega.
	Yeh provide karta hai:
	1. GET /api/requests: Saare captured requests ka JSON list (newest first).
	2. POST /api/replay?id=xxx: Particular request ID ko localhost:3000 par replay karta hai.
	3. GET /: Ek sleek embedded single-file dashboard UI jo browser mein open hoti hai!
*/

// Server inspector web dashboard aur REST API serve karta hai.
type Server struct {
	storage         *RingBuffer
	localTargetAddr string
	listenAddr      string
	httpServer      *http.Server
}

// NewServer inspector web service initialize karta hai.
func NewServer(listenAddr string, storage *RingBuffer, localTargetAddr string) *Server {
	return &Server{
		storage:         storage,
		localTargetAddr: localTargetAddr,
		listenAddr:      listenAddr,
	}
}

// Handler returns the http.Handler for the inspector routes (useful for testing).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 1. REST API: Return captured requests JSON
	mux.HandleFunc("/api/requests", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		records := s.storage.GetAll()
		if records == nil {
			records = []*RequestRecord{} // Ensure valid empty array [] instead of null in JSON
		}
		_ = json.NewEncoder(w).Encode(records)
	})

	// 2. REST API: Replay a request by ID
	mux.HandleFunc("/api/replay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing 'id' query parameter", http.StatusBadRequest)
			return
		}

		record, ok := s.storage.GetByID(id)
		if !ok {
			http.Error(w, "Request ID not found in history", http.StatusNotFound)
			return
		}

		resp, duration, err := ReplayRequest(record, s.localTargetAddr)
		if err != nil {
			slog.Error("replay failed", "id", id, "err", err)
			http.Error(w, fmt.Sprintf("Replay error: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		slog.Info("request replayed successfully", "id", id, "status", resp.StatusCode, "duration_ms", duration)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      "ok",
			"status_code": resp.StatusCode,
			"duration_ms": duration,
		})
	})

	// 3. Embedded Vanilla Dashboard HTML
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(DashboardHTML))
	})

	return mux
}

// Start launches the inspector UI server.
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:    s.listenAddr,
		Handler: s.Handler(),
	}

	slog.Info("inspector web interface live", "url", "http://localhost"+s.listenAddr)
	return s.httpServer.ListenAndServe()
}

// Close gracefully stops the inspector server.
func (s *Server) Close() error {
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

// DashboardHTML provides the single-file web dashboard.
const DashboardHTML = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>GoNgrok Traffic Inspector</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0d1117; color: #c9d1d9; margin: 0; padding: 20px; }
        h1 { color: #58a6ff; font-size: 20px; border-bottom: 1px solid #30363d; padding-bottom: 10px; }
        .toolbar { margin-bottom: 15px; }
        table { width: 100%; border-collapse: collapse; margin-top: 15px; background: #161b22; border-radius: 6px; overflow: hidden; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #30363d; font-size: 14px; }
        th { background: #21262d; color: #8b949e; }
        .method { padding: 3px 8px; border-radius: 4px; font-weight: bold; font-size: 12px; }
        .POST { background: #238636; color: white; }
        .GET { background: #1f6feb; color: white; }
        .PUT { background: #d29922; color: white; }
        .DELETE { background: #da3633; color: white; }
        .replay-btn { background: #8957e5; color: white; border: none; padding: 6px 12px; border-radius: 4px; cursor: pointer; font-size: 12px; transition: 0.2s; }
        .replay-btn:hover { background: #a371f7; }
        pre { background: #0d1117; padding: 8px; border-radius: 4px; font-size: 12px; max-height: 80px; overflow: auto; margin: 0; }
        .status-badge { padding: 2px 6px; border-radius: 4px; font-weight: bold; font-size: 11px; }
        .s2xx { background: #238636; color: white; }
        .s4xx { background: #d29922; color: white; }
        .s5xx { background: #da3633; color: white; }
    </style>
</head>
<body>
    <h1>GoNgrok Webhook & Traffic Inspector (:4040)</h1>
    <div class="toolbar">
        <button onclick="loadRequests()" style="padding: 6px 12px; cursor: pointer; background: #21262d; color: #c9d1d9; border: 1px solid #30363d; border-radius: 4px;">Refresh</button>
    </div>
    <table>
        <thead>
            <tr>
                <th>Time</th>
                <th>Method</th>
                <th>Path</th>
                <th>Status</th>
                <th>Headers</th>
                <th>Body</th>
                <th>Action</th>
            </tr>
        </thead>
        <tbody id="table-body">
            <tr><td colspan="7">Loading traffic...</td></tr>
        </tbody>
    </table>

    <script>
        async function loadRequests() {
            try {
                const res = await fetch('/api/requests');
                const data = await res.json();
                const tbody = document.getElementById('table-body');
                tbody.innerHTML = '';
                if (!data || data.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="7">No requests captured yet. Hit your tunnel URL!</td></tr>';
                    return;
                }
                data.forEach(req => {
                    const tr = document.createElement('tr');
                    const statusClass = req.status_code >= 500 ? 's5xx' : (req.status_code >= 400 ? 's4xx' : 's2xx');
                    const statusText = req.status_code > 0 ? req.status_code + ' (' + req.duration_ms + 'ms)' : 'pending';
                    tr.innerHTML = ` + "`" + `
                        <td>${new Date(req.timestamp).toLocaleTimeString()}</td>
                        <td><span class="method ${req.method}">${req.method}</span></td>
                        <td>${req.path}</td>
                        <td><span class="status-badge ${statusClass}">${statusText}</span></td>
                        <td><pre>${JSON.stringify(req.headers, null, 2)}</pre></td>
                        <td><pre>${req.request_body || '(empty)'}</pre></td>
                        <td><button class="replay-btn" onclick="replay('${req.id}')">Replay</button></td>
                    ` + "`" + `;
                    tbody.appendChild(tr);
                });
            } catch (e) {
                console.error("Failed to load requests:", e);
            }
        }
        async function replay(id) {
            try {
                const res = await fetch('/api/replay?id=' + id, { method: 'POST' });
                const result = await res.json();
                if (result.status === 'ok') {
                    alert('Replay sent! Status: ' + result.status_code + ' (' + result.duration_ms + 'ms)');
                } else {
                    alert('Replay failed: ' + JSON.stringify(result));
                }
                loadRequests();
            } catch (e) {
                alert('Replay request failed: ' + e);
            }
        }
        setInterval(loadRequests, 2000);
        loadRequests();
    </script>
</body>
</html>`
