package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"atad-project/internal/version"
)

func TestHealth(t *testing.T) {
	started := time.Now().Add(-5 * time.Second)
	w := httptest.NewRecorder()
	NewHandler(started).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	assertStatus(t, w, http.StatusOK)
	assertContentType(t, w, "application/json")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body["status"] != "ok" {
		t.Fatalf("unexpected health response: %s", w.Body.String())
	}
	uptime, ok := body["uptime_seconds"].(float64)
	if !ok || uptime < 5 || uptime > float64(int(time.Since(started).Seconds())) || uptime != float64(int(uptime)) {
		t.Errorf("uptime_seconds must be an integer elapsed time, got %v", body["uptime_seconds"])
	}
}

func TestVersion(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandler(time.Now()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/version", nil))

	assertStatus(t, w, http.StatusOK)
	assertContentType(t, w, "application/json")
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	info := version.Current()
	want := map[string]string{
		"app": info.App, "version": info.Version,
		"commit": info.Commit, "built_at": info.BuiltAt,
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("version = %v, want %v", body, want)
	}
}

func TestIdentity(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandler(time.Now()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	assertStatus(t, w, http.StatusOK)
	assertContentType(t, w, "text/html; charset=utf-8")
	for _, text := range []string{version.Current().App, "Dume Denisa", "group -", "Project 1: URL Shortener with Analytics"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Errorf("identity page is missing %q", text)
		}
	}
}

func TestReset(t *testing.T) {
	handler := NewHandler(time.Now())
	// Reset remains safe to call repeatedly when there is no state yet.
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/reset", nil))
		assertStatus(t, w, http.StatusNoContent)
		if w.Body.Len() != 0 {
			t.Errorf("reset must return an empty body, got %q", w.Body.String())
		}
	}
}

func TestRouteBoundaries(t *testing.T) {
	handler := NewHandler(time.Now())
	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/unknown", http.StatusNotFound},
		{http.MethodGet, "/health/extra", http.StatusNotFound},
		{http.MethodPost, "/health", http.StatusMethodNotAllowed},
		{http.MethodPost, "/version", http.StatusMethodNotAllowed},
		{http.MethodPost, "/", http.StatusMethodNotAllowed},
		{http.MethodGet, "/reset", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			assertStatus(t, w, tc.status)
		})
	}
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, want, w.Body.String())
	}
}

func assertContentType(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := w.Header().Get("Content-Type"); got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
}
