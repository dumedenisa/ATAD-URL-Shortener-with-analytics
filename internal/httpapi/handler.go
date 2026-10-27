// Package httpapi implements the service HTTP contract.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"atad-project/internal/storage"
	"atad-project/internal/version"
)

// LinkStore supplies the persistent operations used by HTTP handlers.
type LinkStore interface {
	Create(context.Context, string, string) (storage.Link, error)
	Get(context.Context, string) (storage.Link, error)
	Reset(context.Context) error
}

// NewHandler constructs the application routes without opening a listener.
// started is the process startup time used to report uptime.
func NewHandler(started time.Time, store LinkStore) http.Handler {
	mux := http.NewServeMux()

	// ---- the common contract: four routes, identical in every project -------

	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = version.WriteJSON(w)
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"uptime_seconds": int(time.Since(started).Seconds()),
		})
	})

	// The marking script reads the application and student identity here.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8">
<title>%s</title><body>
<h1>%s</h1>
<p>Dume Denisa, group -</p>
<p>Project 1: URL Shortener with Analytics</p>
</body></html>`, version.Current().App, version.Current().App)
	})

	// Discards everything the service holds, so tests can start from a known
	// state without restarting the container. If your project persists data,
	// this clears what was persisted too.
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Reset(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not reset stored links")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /links", createLink(store, generateCode))
	mux.HandleFunc("GET /{code}", redirectLink(store))

	return mux
}

// writeJSON is the one place that sets the content type, so every response in
// the service is consistent about it.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError produces the error shape the common contract requires. Use it for
// every failure rather than writing the JSON by hand at each call site.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
