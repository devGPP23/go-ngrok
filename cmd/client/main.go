package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/apoorvslst/go-ngrok/internal/client"
	"github.com/apoorvslst/go-ngrok/internal/client/forwarder"
	"github.com/apoorvslst/go-ngrok/internal/config"
	"github.com/apoorvslst/go-ngrok/internal/inspector"
)

func main() {
	config.SetupLogger(true)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	token := "dev-secret-token"
	desiredSubdomain := "gaurav"
	localTarget := "localhost:3000" // Humara local web app yahan chalega!
	inspectorAddr := ":4040"        // Web interface port for live traffic inspector

	// 1. Day 7 Inspector Engine initialize karo (100 requests fixed ring buffer)
	inspectorStorage := inspector.NewRingBuffer(100)
	forwarder.SetInspectorStorage(inspectorStorage)

	inspectorServer := inspector.NewServer(inspectorAddr, inspectorStorage, localTarget)
	go func() {
		if err := inspectorServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("inspector web server stopped with error", "err", err)
		}
	}()
	defer inspectorServer.Close()

	// Exponential Backoff Config for reconnection
	bCfg := client.BackoffConfig{
		BaseDelay:  1 * time.Second,
		MaxDelay:   15 * time.Second,
		MaxRetries: 0, // 0 = retry forever until user presses Ctrl+C
	}

	slog.Info("GoNgrok Client starting with auto-reconnect and inspector...", "subdomain", desiredSubdomain)

	// 🔥 Auto-Reconnect Engine (Saara connect/handshake iske andar hoga)
	err := client.RetryWithBackoff(ctx, bCfg, "tunnel-client", func() error {
		// 1. Server se connect karo
		conn, err := client.Connect("localhost:9000")
		if err != nil {
			return fmt.Errorf("connection error: %w", err)
		}
		defer conn.Close()

		// 2. Handshake karo (agar fail ho toh return error taaki retry ho sake)
		authResp, err := client.PerformClientHandshake(conn, token, desiredSubdomain)
		if err != nil {
			return fmt.Errorf("handshake failed: %w", err)
		}

		// Ngrok style friendly console banner
		fmt.Printf("\n============================================================\n")
		fmt.Printf("🚀 TUNNEL LIVE AT:             %s\n", authResp.AssignedURL)
		fmt.Printf("Forwarding:                    %s -> http://%s\n", authResp.AssignedURL, localTarget)
		fmt.Printf("Web Interface (Inspector):     http://127.0.0.1%s\n", inspectorAddr)
		fmt.Printf("============================================================\n")
		fmt.Println("Press Ctrl+C to quit.")

		// 3. Yamux Session elevate karo
		session, err := client.NewYamuxClientSession(conn)
		if err != nil {
			return fmt.Errorf("yamux client session failed: %w", err)
		}
		defer session.Close()

		slog.Info("Yamux client session ready. Forwarding traffic to local app...")

		// 4. Stream listener loop (blocks until connection breaks)
		client.ListenForServerStreams(session, localTarget)

		// Connection break hua, loop wapas retry karega!
		return fmt.Errorf("connection lost, initiating reconnect...")
	})

	if err != nil && err != context.Canceled {
		slog.Error("client terminated with error", "err", err)
	} else {
		slog.Info("client gracefully stopped.")
	}
}
