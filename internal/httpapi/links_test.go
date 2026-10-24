package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"atad-project/internal/storage"
)

func postLink(handler http.Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(w, r)
	return w
}

func TestCreateCustomLink(t *testing.T) {
	store := testStore(t)
	handler := NewHandler(time.Now(), store)
	body := `{"url":"https://go.dev/doc/effective_go","code":"gofmt"}`
	w := postLink(handler, body)
	assertStatus(t, w, http.StatusCreated)
	assertContentType(t, w, "application/json")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil || len(fields) != 4 {
		t.Fatalf("creation must return exactly four fields: %s, %v", w.Body.String(), err)
	}
	var link storage.Link
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	if link.ID != 1 || link.Code != "gofmt" || link.URL != "https://go.dev/doc/effective_go" || link.CreatedAt.IsZero() {
		t.Fatalf("unexpected link: %+v", link)
	}
	stored, err := store.Get(context.Background(), link.Code)
	if err != nil || stored.ID != link.ID || stored.URL != link.URL || !stored.CreatedAt.Equal(link.CreatedAt) {
		t.Fatalf("stored link mismatch: %+v, %v", stored, err)
	}
	assertAPIError(t, postLink(handler, `{"url":"https://example.org","code":"gofmt"}`), http.StatusConflict, "conflict")
	stored, err = store.Get(context.Background(), "gofmt")
	if err != nil || stored.URL != link.URL {
		t.Fatalf("conflict overwrote original: %+v, %v", stored, err)
	}
	for _, code := range []string{"Ab-_", "0123456789"} {
		assertStatus(t, postLink(handler, `{"url":"http://localhost:8080/path?q=1#part","code":"`+code+`"}`), http.StatusCreated)
	}
}

func TestCreateValidation(t *testing.T) {
	handler := NewHandler(time.Now(), testStore(t))
	for _, body := range []string{
		``, `{`, `null`, `[]`, `{}`, `{"url":123,"code":"okay"}`,
		`{"url":"https://example.org","code":123}`,
		`{"url":"https://example.org","code":"okay","extra":1}`,
		`{"url":"https://example.org","code":"okay"} {}`,
		`{"url":"https://example.org","code":"okay"} garbage`,
	} {
		t.Run(body, func(t *testing.T) {
			assertAPIError(t, postLink(handler, body), http.StatusBadRequest, "validation")
		})
	}
	for _, destination := range []string{"", "example.org", "/relative", "ftp://example.org", "javascript:alert(1)", "https://", "http:///path", "https://example.org/a b", "https://example.org/\n", "https://example.org:bad"} {
		t.Run(destination, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"url": destination, "code": "okay"})
			assertAPIError(t, postLink(handler, string(body)), http.StatusBadRequest, "validation")
		})
	}
	for _, code := range []string{"", "abc", "abcdefghijk", "abc/", "abc.", "abc ", "ábcd", "health", "version", "reset", "links"} {
		t.Run(code, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"url": "https://example.org", "code": code})
			assertAPIError(t, postLink(handler, string(body)), http.StatusBadRequest, "validation")
		})
	}
	assertAPIError(t, postLink(handler, `{"url":"https://example.org/`+strings.Repeat("a", 1<<20)+`","code":"okay"}`), http.StatusBadRequest, "validation")
	// Invalid requests must not consume IDs or insert rows.
	w := postLink(handler, `{"url":"https://example.org","code":"okay"}`)
	assertStatus(t, w, http.StatusCreated)
	var link storage.Link
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil || link.ID != 1 {
		t.Fatalf("validation changed storage: %s, %v", w.Body.String(), err)
	}
}

func TestConcurrentCreateHTTP(t *testing.T) {
	handler := NewHandler(time.Now(), testStore(t))
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- postLink(handler, `{"url":"https://example.org","code":"same"}`)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	created, conflicts := 0, 0
	for w := range results {
		switch w.Code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
			assertAPIError(t, w, http.StatusConflict, "conflict")
		default:
			t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
		}
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("created %d, conflicts %d", created, conflicts)
	}
}

func TestHTTPPersistenceAndReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(time.Now(), store)
	body := `{"url":"https://example.org","code":"keep"}`
	assertStatus(t, postLink(handler, body), http.StatusCreated)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler = NewHandler(time.Now(), store)
	assertAPIError(t, postLink(handler, body), http.StatusConflict, "conflict")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/reset", nil))
	assertStatus(t, w, http.StatusNoContent)
	if w.Body.Len() != 0 {
		t.Fatal("reset returned a body")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Get(context.Background(), "keep"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("reset did not persist: %v", err)
	}
	w = postLink(NewHandler(time.Now(), store), body)
	assertStatus(t, w, http.StatusCreated)
	var link storage.Link
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil || link.ID != 1 {
		t.Fatalf("reset did not reset sequence: %s, %v", w.Body.String(), err)
	}
}

func TestStorageFailureHTTP(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(time.Now(), store)
	assertAPIError(t, postLink(handler, `{"url":"https://example.org","code":"okay"}`), http.StatusInternalServerError, "internal")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/reset", nil))
	assertAPIError(t, w, http.StatusInternalServerError, "internal")
}

func assertAPIError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	assertStatus(t, w, status)
	assertContentType(t, w, "application/json")
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body) != 2 || body["error"] != code || body["message"] == "" {
		t.Fatalf("unexpected error body: %s, %v", w.Body.String(), err)
	}
}
