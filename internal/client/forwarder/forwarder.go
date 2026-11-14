/*
	Server ne Yamux stream open karke visitor ka HTTP request wire pe fek diya. Ab client ke laptop par kya hona chahiye?
	Jaise hi server se naya stream aata hai, Client laptop par chal rahe local app (localhost:3000) se connect karta hai (net.Dial("tcp", localAddr)).
	Agar local app band hai? Client yamux stream ke andar seedha ek raw HTTP 502 Bad Gateway response likh deta hai! ReverseProxy us response ko visitor ke browser tak bhej deti hai.
	Agar local app chal raha hai:
	- Agar Traffic Inspector active hai:
	  Hum request ko capture karte hain (io.TeeReader se 1MB cap ke sath), ring buffer mein add karte hain,
	  local app ko forward karte hain, aur response status code & latency measure karke ring buffer update karte hain!
	- Agar non-HTTP stream hai:
	  Hum 2 concurrent goroutines launch karte hain:
	  Goroutine 1: Stream se bytes padho -> local app ko bhejo (Request).
	  Goroutine 2: Local app se bytes padho -> stream mein bhejo (Response).
	  Jaise hi koi ek side close hoti hai (request/response poora hua), <-errChan unblock hota hai aur dono connections safely close ho jate hain.
*/

package forwarder

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/apoorvslst/go-ngrok/internal/inspector"
)

var (
	inspectorMu            sync.RWMutex
	globalInspectorStorage *inspector.RingBuffer
)

// SetInspectorStorage global ring buffer register karta hai taaki incoming requests inspect ho sakein.
func SetInspectorStorage(storage *inspector.RingBuffer) {
	inspectorMu.Lock()
	defer inspectorMu.Unlock()
	globalInspectorStorage = storage
}

// GetInspectorStorage returns the currently registered inspector storage.
func GetInspectorStorage() *inspector.RingBuffer {
	inspectorMu.RLock()
	defer inspectorMu.RUnlock()
	return globalInspectorStorage
}

// ForwardToLocal connects an incoming yamux stream to the local application.
func ForwardToLocal(stream net.Conn, localAddr string) {
	defer stream.Close()

	// 1. Local application se connect karo (e.g. localhost:3000)
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		slog.Error("local app unreachable — is it running?",
			"local_addr", localAddr,
			"err", err,
		)
		// Agar error hai toh visitor ko clean HTTP 502 Bad Gateway error return karo
		raw502 := "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\n\r\nLocal application at " + localAddr + " is not reachable. Is it running?\n"
		_, _ = stream.Write([]byte(raw502))
		return
	}
	defer localConn.Close()

	slog.Info("forwarding stream to local app",
		"local_addr", localAddr,
		"stream_remote", stream.RemoteAddr(),
	)

	// 2. Agar Traffic Inspector active hai, HTTP request inspect aur capture karo!
	storage := GetInspectorStorage()
	if storage != nil {
		bufReader := bufio.NewReader(stream)
		req, err := http.ReadRequest(bufReader)
		if err == nil {
			startTime := time.Now()

			// Request capture karo (TeeReader + 1MB limit + MultiReader restoration)
			record, captureErr := inspector.CaptureRequest(req, 1024*1024)
			if captureErr == nil && record != nil {
				storage.Add(record)
			}

			// Captured/Restored request local application ko send karo
			if err := req.Write(localConn); err != nil {
				slog.Error("failed to write request to local app", "err", err)
				return
			}

			// Local app se response read karo
			respReader := bufio.NewReader(localConn)
			resp, err := http.ReadResponse(respReader, req)
			if err == nil {
				// Telemetry update karo: status code aur round-trip duration in ms
				if record != nil {
					record.StatusCode = resp.StatusCode
					record.DurationMs = time.Since(startTime).Milliseconds()
				}
				_ = resp.Write(stream)
				return
			}

			// Agar structured response parse na ho, bache bytes wire pe pass karo
			_, _ = io.Copy(stream, respReader)
			return
		}

		// Agar HTTP request read nahi ho saki (non-HTTP stream), buffered bytes wapas stream ke aage lagao
		var streamReader io.Reader = stream
		if bufReader.Buffered() > 0 {
			streamReader = io.MultiReader(bufReader, stream)
		}

		// Raw bidirectional fallback
		rawBidirectionalCopy(stream, localConn, streamReader)
		return
	}

	// 3. Fallback: Pure raw bidirectional TCP copy
	rawBidirectionalCopy(stream, localConn, stream)
}

// rawBidirectionalCopy performs bidirectional streaming between yamux stream and local connection.
func rawBidirectionalCopy(stream net.Conn, localConn net.Conn, streamReader io.Reader) {
	errChan := make(chan error, 2)

	// Goroutine 1: Stream -> Local App (Visitor request aata hai)
	go func() {
		_, copyErr := io.Copy(localConn, streamReader)
		errChan <- copyErr
	}()

	// Goroutine 2: Local App -> Stream (App ka response wapas jaata hai)
	go func() {
		_, copyErr := io.Copy(stream, localConn)
		errChan <- copyErr
	}()

	// Dono mein se kisi ek ke complete hone ka wait karo
	_ = <-errChan
}