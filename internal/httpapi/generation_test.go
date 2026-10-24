package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"atad-project/internal/storage"
)

func TestGeneratedLinkHTTP(t *testing.T) {
	store := testStore(t)
	handler := NewHandler(time.Now(), store)
	for _, body := range []string{`{"url":"https://example.org"}`, `{"url":"https://example.org","code":null}`} {
		w := postLink(handler, body)
		assertStatus(t, w, http.StatusCreated)
		var link storage.Link
		if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
			t.Fatal(err)
		}
		if len(link.Code) != 7 || !customCode.MatchString(link.Code) {
			t.Fatalf("invalid generated code: %q", link.Code)
		}
		stored, err := store.Get(context.Background(), link.Code)
		if err != nil || stored.ID != link.ID || stored.URL != link.URL {
			t.Fatalf("generated link not stored: %+v, %v", stored, err)
		}
	}
}

func TestGeneratedCollisionRetries(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	original, err := store.Create(ctx, "taken01", "https://original.example")
	if err != nil {
		t.Fatal(err)
	}
	codes := []string{"version", "taken01", "taken01", "fresh01"}
	calls := 0
	generator := func() (string, error) { code := codes[calls]; calls++; return code, nil }
	w := postLink(createLink(store, generator), `{"url":"https://new.example"}`)
	assertStatus(t, w, http.StatusCreated)
	var link storage.Link
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || link.Code != "fresh01" {
		t.Fatalf("calls=%d, link=%+v", calls, link)
	}
	got, err := store.Get(ctx, original.Code)
	if err != nil || got.URL != original.URL || got.ID != original.ID {
		t.Fatalf("collision overwrote original: %+v, %v", got, err)
	}
	// A custom-code conflict must not call the generator or be silently renamed.
	w = postLink(createLink(store, func() (string, error) { t.Fatal("generator called for custom code"); return "", nil }), `{"url":"https://new.example","code":"taken01"}`)
	assertAPIError(t, w, http.StatusConflict, "conflict")
}

func TestGeneratedFailures(t *testing.T) {
	store := testStore(t)
	if _, err := store.Create(context.Background(), "taken01", "https://example.org"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	w := postLink(createLink(store, func() (string, error) { calls++; return "taken01", nil }), `{"url":"https://example.org"}`)
	assertAPIError(t, w, http.StatusInternalServerError, "internal")
	if calls != maxCodeAttempts {
		t.Fatalf("retry count = %d", calls)
	}
	w = postLink(createLink(store, func() (string, error) { return "", errors.New("entropy unavailable") }), `{"url":"https://example.org"}`)
	assertAPIError(t, w, http.StatusInternalServerError, "internal")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := createGeneratedLink(ctx, store, "https://example.org", func() (string, error) { t.Fatal("generator called after cancellation"); return "", nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// A non-conflict database error must stop immediately, not consume retries.
	calls = 0
	w = postLink(createLink(store, func() (string, error) { calls++; return "fresh01", nil }), `{"url":"https://example.org"}`)
	assertAPIError(t, w, http.StatusInternalServerError, "internal")
	if calls != 1 {
		t.Fatalf("retried storage failure %d times", calls)
	}
}

func TestConcurrentGeneratedCollision(t *testing.T) {
	store := testStore(t)
	var calls atomic.Int64
	handler := createLink(store, func() (string, error) {
		n := calls.Add(1)
		if n <= 2 {
			return "shared1", nil
		}
		return fmt.Sprintf("code%03d", n), nil
	})
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; responses <- postLink(handler, `{"url":"https://example.org"}`) }()
	}
	close(start)
	wg.Wait()
	close(responses)
	seen := map[string]bool{}
	for w := range responses {
		assertStatus(t, w, http.StatusCreated)
		var link storage.Link
		if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
			t.Fatal(err)
		}
		if seen[link.Code] {
			t.Fatalf("duplicate generated code %q", link.Code)
		}
		seen[link.Code] = true
		if _, err := store.Get(context.Background(), link.Code); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 || len(seen) != 2 {
		t.Fatalf("calls=%d, codes=%v", calls.Load(), seen)
	}
}
