package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"atad-project/internal/storage"
)

func visit(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestRedirectCreatedLinks(t *testing.T) {
	store := testStore(t)
	handler := NewHandler(time.Now(), store)
	for _, body := range []string{
		`{"url":"https://example.org/path?q=one%20two&x=3#section","code":"Go-_01"}`,
		`{"url":"http://example.org:8080/other"}`,
	} {
		created := postLink(handler, body)
		assertStatus(t, created, http.StatusCreated)
		var link storage.Link
		if err := json.Unmarshal(created.Body.Bytes(), &link); err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := visit(handler, method, "/"+link.Code+"?ignored=1")
			assertStatus(t, w, http.StatusFound)
			if got := w.Header().Get("Location"); got != link.URL {
				t.Fatalf("Location = %q, want %q", got, link.URL)
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		}
	}
	// Codes are case-sensitive.
	assertAPIError(t, visit(handler, http.MethodGet, "/go-_01"), http.StatusNotFound, "not_found")
	assertAPIError(t, visit(handler, http.MethodGet, "/missing"), http.StatusNotFound, "not_found")
}

func TestRedirectAfterReopenAndReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(time.Now(), store)
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"saved"}`), http.StatusCreated)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler = NewHandler(time.Now(), store)
	assertStatus(t, visit(handler, http.MethodGet, "/saved"), http.StatusFound)
	assertStatus(t, visit(handler, http.MethodPost, "/reset"), http.StatusNoContent)
	assertAPIError(t, visit(handler, http.MethodGet, "/saved"), http.StatusNotFound, "not_found")
}

func TestRedirectFailures(t *testing.T) {
	store := testStore(t)
	handler := NewHandler(time.Now(), store)
	// Simulate legacy or corrupted destinations bypassing HTTP validation.
	for _, destination := range []string{"javascript:alert(1)", "/relative", "https://", "https://example.org/\r\nInjected: value"} {
		if err := store.Reset(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Create(context.Background(), "broken", destination); err != nil {
			t.Fatal(err)
		}
		w := visit(handler, http.MethodGet, "/broken")
		assertAPIError(t, w, http.StatusUnprocessableEntity, "unprocessable")
		if w.Header().Get("Location") != "" {
			t.Fatal("invalid destination leaked into Location")
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	w := visit(handler, http.MethodGet, "/broken")
	assertAPIError(t, w, http.StatusInternalServerError, "internal")
	if w.Header().Get("Location") != "" {
		t.Fatal("storage error returned Location")
	}
}

func TestRedirectRouteBoundaries(t *testing.T) {
	handler := NewHandler(time.Now(), testStore(t))
	for _, path := range []string{"/links", "/reset"} {
		w := visit(handler, http.MethodGet, path)
		assertAPIError(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
		if w.Header().Get("Allow") != "POST" {
			t.Fatalf("missing Allow for %s", path)
		}
	}
	for _, path := range []string{"/links/anything/stats", "/anything/extra", "/health/extra"} {
		assertStatus(t, visit(handler, http.MethodGet, path), http.StatusNotFound)
	}
	assertStatus(t, visit(handler, http.MethodPost, "/anything"), http.StatusMethodNotAllowed)
}
