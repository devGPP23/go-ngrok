package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

/*
	🧠 Concept: Panic Recovery Middleware
	Agar kisi third-party visitor ne malformed header bheja ya code mein nil pointer dereference ho gaya,
	aur agar humne recover nahi kiya, toh **poora server process crash** ho jayega!
	Result: Server par chal rahe 1,000 developers ke tunnels sab ek second mein drop ho jayenge!

	🛠️ The Solution:
	defer func() { if r := recover(); r != nil { ... } }()
	Crash ko intercept karo, stack trace log karo, us visitor ko clean HTTP 500 error return karo,
	aur server process bina ruke chalta rahega!
*/

// Recoverer catches any panics in HTTP handlers, logs the stack trace,
// and returns a clean 500 Internal Server Error without killing the server process.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				slog.Error("panic recovered in HTTP handler",
					"error", rvr,
					"method", r.Method,
					"path", r.URL.Path,
					"remote", r.RemoteAddr,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
