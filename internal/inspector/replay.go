package inspector

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"
)

/*
	🧠 Concept: Webhook Replay Engine
	Replay kaise kaam karta hai?
	1. Developer localhost:4040 UI par "Replay" button dabata hai.
	2. RingBuffer se hum original request ka pura data (Method, Path, Headers, Body) nikaalte hain.
	3. Ek identical HTTP request dubara construct karte hain:
	   - Method: POST / GET / etc.
	   - Target: http://localhost:3000/webhook
	   - Body: exact same bytes
	   - Header: 'X-GoNgrok-Replayed: true' taaki backend ko pata chale yeh replay hai
	4. Seedha local application par fire kar dete hain!
	Developer ko Stripe ya GitHub par jaake naya event trigger karne ki zaroorat nahi padti!
*/

// ReplayRequest captured record ko local application ke target address par dobara fire karta hai.
func ReplayRequest(record *RequestRecord, localTargetAddr string) (*http.Response, int64, error) {
	if record == nil {
		return nil, 0, fmt.Errorf("cannot replay nil record")
	}

	// 1. Target URL construct karo (e.g., http://localhost:3000 + /webhook)
	if !strings.HasPrefix(localTargetAddr, "http://") && !strings.HasPrefix(localTargetAddr, "https://") {
		localTargetAddr = "http://" + localTargetAddr
	}
	targetURL := fmt.Sprintf("%s%s", localTargetAddr, record.Path)

	// 2. Body reader build karo
	bodyReader := bytes.NewReader([]byte(record.RequestBody))

	// 3. Exact same method aur target URL ke saath new HTTP request banao
	req, err := http.NewRequest(record.Method, targetURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to reconstruct request: %w", err)
	}

	// 4. Original headers copy karo (Host header ko override hone se bachaate hain)
	for k, v := range record.Headers {
		if strings.ToLower(k) != "host" {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("X-GoNgrok-Replayed", "true")

	// 5. Latency measure karo aur local app par request fire karo!
	startTime := time.Now()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	duration := time.Since(startTime).Milliseconds()

	if err != nil {
		return nil, duration, fmt.Errorf("replay request failed against %s: %w", targetURL, err)
	}

	return resp, duration, nil
}
