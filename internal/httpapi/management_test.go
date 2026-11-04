package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"atad-project/internal/storage"
)

func readLinks(t *testing.T, handler http.Handler) []linkResponse {
	t.Helper()
	w := visit(handler, http.MethodGet, "/links")
	assertStatus(t, w, http.StatusOK)
	assertContentType(t, w, "application/json")
	var links []linkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &links); err != nil || links == nil {
		t.Fatalf("expected JSON array: %s, %v", w.Body.String(), err)
	}
	var fields []map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for _, item := range fields {
		if len(item) != 4 || item["id"] == nil || item["code"] == nil || item["url"] == nil || item["created_at"] == nil {
			t.Fatalf("unexpected list item: %v", item)
		}
	}
	return links
}

func TestListDeleteAndResetHTTP(t *testing.T) {
	handler := NewHandler(time.Now(), testStore(t))
	if links := readLinks(t, handler); len(links) != 0 {
		t.Fatal("initial list is not empty")
	}
	for _, code := range []string{"first", "second", "third"} {
		assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"`+code+`"}`), http.StatusCreated)
	}
	links := readLinks(t, handler)
	if len(links) != 3 || links[0].Code != "third" || links[1].Code != "second" || links[2].Code != "first" {
		t.Fatalf("wrong order: %+v", links)
	}
	assertStatus(t, visit(handler, http.MethodGet, "/second"), http.StatusFound)
	readLinks(t, handler)
	readLinks(t, handler)
	if stats := readStats(t, handler, "second"); stats.Visits != 1 {
		t.Fatalf("list counted as visit: %+v", stats)
	}
	w := visit(handler, http.MethodDelete, "/links/second")
	assertStatus(t, w, http.StatusNoContent)
	if w.Body.Len() != 0 {
		t.Fatal("delete must have no body")
	}
	assertAPIError(t, visit(handler, http.MethodDelete, "/links/second"), http.StatusNotFound, "not_found")
	assertAPIError(t, visit(handler, http.MethodGet, "/second"), http.StatusNotFound, "not_found")
	assertAPIError(t, visit(handler, http.MethodGet, "/links/second/stats"), http.StatusNotFound, "not_found")
	links = readLinks(t, handler)
	if len(links) != 2 || links[0].Code != "third" || links[1].Code != "first" {
		t.Fatalf("delete changed other links: %+v", links)
	}
	assertStatus(t, postLink(handler, `{"url":"https://new.example","code":"second"}`), http.StatusCreated)
	links = readLinks(t, handler)
	if links[0].Code != "second" || links[0].ID != 4 {
		t.Fatalf("recreated link: %+v", links)
	}
	if stats := readStats(t, handler, "second"); stats.Visits != 0 || stats.LastVisitAt != nil {
		t.Fatalf("old stats reused: %+v", stats)
	}
	assertStatus(t, visit(handler, http.MethodPost, "/reset"), http.StatusNoContent)
	if len(readLinks(t, handler)) != 0 {
		t.Fatal("reset left links")
	}
	assertAPIError(t, visit(handler, http.MethodDelete, "/links/first"), http.StatusNotFound, "not_found")
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"after"}`), http.StatusCreated)
	if links := readLinks(t, handler); len(links) != 1 || links[0].ID != 1 {
		t.Fatalf("reset did not clear sequence: %+v", links)
	}
}

func TestManagementPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(time.Now(), s)
	for _, code := range []string{"keep", "gone"} {
		assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"`+code+`"}`), http.StatusCreated)
	}
	assertStatus(t, visit(handler, http.MethodDelete, "/links/gone"), http.StatusNoContent)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler = NewHandler(time.Now(), s)
	if links := readLinks(t, handler); len(links) != 1 || links[0].Code != "keep" {
		t.Fatalf("delete not persisted: %+v", links)
	}
	assertAPIError(t, visit(handler, http.MethodGet, "/gone"), http.StatusNotFound, "not_found")
	assertStatus(t, visit(handler, http.MethodPost, "/reset"), http.StatusNoContent)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(readLinks(t, NewHandler(time.Now(), s))) != 0 {
		t.Fatal("reset not persisted")
	}
}

func TestConcurrentDeleteHTTP(t *testing.T) {
	handler := NewHandler(time.Now(), testStore(t))
	assertStatus(t, postLink(handler, `{"url":"https://example.org","code":"once"}`), http.StatusCreated)
	results := make(chan int, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- visit(handler, http.MethodDelete, "/links/once").Code }()
	}
	close(start)
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[http.StatusNoContent] != 1 || counts[http.StatusNotFound] != 1 {
		t.Fatalf("delete results: %v", counts)
	}
}

func TestManagementStorageFailures(t *testing.T) {
	s := testStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(time.Now(), s)
	assertAPIError(t, visit(handler, http.MethodGet, "/links"), http.StatusInternalServerError, "internal")
	assertAPIError(t, visit(handler, http.MethodDelete, "/links/anything"), http.StatusInternalServerError, "internal")
}
