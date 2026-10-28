package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"atad-project/internal/storage"
)

func readStats(t *testing.T, handler http.Handler, code string) storage.Link {
	t.Helper()
	w := visit(handler, http.MethodGet, "/links/"+code+"/stats")
	assertStatus(t, w, http.StatusOK)
	assertContentType(t, w, "application/json")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil || len(fields) != 3 {
		t.Fatalf("stats must contain exactly three fields: %s, %v", w.Body.String(), err)
	}
	for _, field := range []string{"code", "visits", "last_visit_at"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("missing %s", field)
		}
	}
	var link storage.Link
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	return link
}

func TestVisitStatistics(t *testing.T) {
	store := testStore(t)
	handler := NewHandler(time.Now(), store)
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"stats"}`), http.StatusCreated)
	initial := readStats(t, handler, "stats")
	if initial.Code != "stats" || initial.Visits != 0 || initial.LastVisitAt != nil {
		t.Fatalf("initial: %+v", initial)
	}
	assertStatus(t, visit(handler, http.MethodHead, "/stats"), http.StatusFound)
	if got := readStats(t, handler, "stats"); got.Visits != 0 || got.LastVisitAt != nil {
		t.Fatalf("HEAD counted: %+v", got)
	}
	before := time.Now().Add(-time.Millisecond)
	assertStatus(t, visit(handler, http.MethodGet, "/stats"), http.StatusFound)
	first := readStats(t, handler, "stats")
	second := readStats(t, handler, "stats")
	if first.Visits != 1 || first.LastVisitAt == nil || first.LastVisitAt.Before(before) || first.LastVisitAt.After(time.Now()) {
		t.Fatalf("first visit: %+v", first)
	}
	if second.Visits != 1 || second.LastVisitAt == nil || !second.LastVisitAt.Equal(*first.LastVisitAt) {
		t.Fatalf("stats read changed data: %+v", second)
	}
	assertStatus(t, visit(handler, http.MethodGet, "/stats"), http.StatusFound)
	if got := readStats(t, handler, "stats"); got.Visits != 2 || got.LastVisitAt.Before(*first.LastVisitAt) {
		t.Fatalf("second visit: %+v", got)
	}
	assertAPIError(t, visit(handler, http.MethodGet, "/links/absent/stats"), http.StatusNotFound, "not_found")
	assertStatus(t, visit(handler, http.MethodPost, "/links/stats/stats"), http.StatusMethodNotAllowed)
}

func TestConcurrentVisitsAndReads(t *testing.T) {
	handler := NewHandler(time.Now(), testStore(t))
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"many"}`), http.StatusCreated)
	const count = 40
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := visit(handler, http.MethodGet, "/many")
			if w.Code != http.StatusFound {
				t.Errorf("redirect: %d %s", w.Code, w.Body.String())
			}
			w = visit(handler, http.MethodGet, "/links/many/stats")
			if w.Code != http.StatusOK {
				t.Errorf("stats: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if got := readStats(t, handler, "many"); got.Visits != count || got.LastVisitAt == nil {
		t.Fatalf("lost increments: %+v", got)
	}
}

func TestStatsPersistenceAndReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(time.Now(), s)
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"saved"}`), http.StatusCreated)
	assertStatus(t, visit(handler, http.MethodGet, "/saved"), http.StatusFound)
	before := readStats(t, handler, "saved")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	handler = NewHandler(time.Now(), s)
	after := readStats(t, handler, "saved")
	if after.Visits != 1 || after.LastVisitAt == nil || !after.LastVisitAt.Equal(*before.LastVisitAt) {
		t.Fatalf("stats lost on reopen: %+v", after)
	}
	assertStatus(t, visit(handler, http.MethodPost, "/reset"), http.StatusNoContent)
	assertAPIError(t, visit(handler, http.MethodGet, "/links/saved/stats"), http.StatusNotFound, "not_found")
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"saved"}`), http.StatusCreated)
	if got := readStats(t, handler, "saved"); got.Visits != 0 || got.LastVisitAt != nil {
		t.Fatalf("reset kept stats: %+v", got)
	}
}

type failingVisitStore struct {
	*storage.Store
	err error
}

func (s failingVisitStore) RecordVisit(context.Context, storage.Link) error { return s.err }

func TestUnservedRedirectNotCounted(t *testing.T) {
	s := testStore(t)
	if _, err := s.Create(context.Background(), "broken", "javascript:alert(1)"); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(time.Now(), s)
	assertAPIError(t, visit(handler, http.MethodGet, "/broken"), http.StatusUnprocessableEntity, "unprocessable")
	if got := readStats(t, handler, "broken"); got.Visits != 0 || got.LastVisitAt != nil {
		t.Fatalf("broken link counted: %+v", got)
	}
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"valid"}`), http.StatusCreated)
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{errors.New("write failed"), http.StatusInternalServerError, "internal"},
		{storage.ErrNotFound, http.StatusNotFound, "not_found"},
	} {
		h := NewHandler(time.Now(), failingVisitStore{s, tc.err})
		w := visit(h, http.MethodGet, "/valid")
		assertAPIError(t, w, tc.status, tc.code)
		if w.Header().Get("Location") != "" {
			t.Fatal("failed write served redirect")
		}
	}
	if got := readStats(t, handler, "valid"); got.Visits != 0 {
		t.Fatalf("failed write counted: %+v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, visit(handler, http.MethodGet, "/links/valid/stats"), http.StatusInternalServerError, "internal")
}
