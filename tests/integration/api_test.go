// Package integration_test exercises the API over real HTTP with on-disk SQLite.
package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"atad-project/internal/httpapi"
	"atad-project/internal/storage"
)

type service struct {
	server *httptest.Server
	store  *storage.Store
	client *http.Client
}

func start(t *testing.T, path string) *service {
	t.Helper()
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewHandler(time.Now(), store))
	client := server.Client()
	client.Timeout = 10 * time.Second
	// Assert the redirect itself, never contact its external destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &service{server, store, client}
	t.Cleanup(s.close)
	return s
}

func (s *service) close() {
	s.server.Close()
	_ = s.store.Close()
}

func (s *service) request(t *testing.T, method, path, body string, status int) ([]byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, s.server.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: status %d, want %d; %s", method, path, res.StatusCode, status, data)
	}
	if status == http.StatusNoContent || method == http.MethodHead {
		if len(data) != 0 {
			t.Fatalf("unexpected response body: %s", data)
		}
	} else if status != http.StatusFound && res.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("%s %s: wrong content type %q", method, path, res.Header.Get("Content-Type"))
	}
	return data, res.Header
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (s *service) stats(t *testing.T, code string) storage.Link {
	t.Helper()
	data, _ := s.request(t, "GET", "/links/"+code+"/stats", "", 200)
	return decode[storage.Link](t, data)
}

func TestLinkLifecycleAcrossHTTPRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	s := start(t, path)
	s.request(t, "POST", "/reset", "", 204)
	data, _ := s.request(t, "POST", "/links", `{"url":"https://example.org/docs?q=1#part","code":"custom"}`, 201)
	link := decode[storage.Link](t, data)
	if link.ID != 1 || link.Code != "custom" || link.CreatedAt.IsZero() {
		t.Fatalf("creation: %+v", link)
	}
	data, _ = s.request(t, "POST", "/links", `{"url":"https://other.example","code":"custom"}`, 409)
	errBody := decode[map[string]string](t, data)
	if errBody["error"] != "conflict" || errBody["message"] == "" {
		t.Fatalf("conflict: %s", data)
	}
	if initial := s.stats(t, "custom"); initial.Visits != 0 || initial.LastVisitAt != nil {
		t.Fatalf("initial stats: %+v", initial)
	}
	_, headers := s.request(t, "GET", "/custom", "", 302)
	if headers.Get("Location") != link.URL {
		t.Fatalf("Location: %q", headers.Get("Location"))
	}
	first, second := s.stats(t, "custom"), s.stats(t, "custom")
	if first.Visits != 1 || first.LastVisitAt == nil || second.Visits != 1 || second.LastVisitAt == nil || !first.LastVisitAt.Equal(*second.LastVisitAt) {
		t.Fatalf("reads changed stats: %+v %+v", first, second)
	}
	s.request(t, "HEAD", "/custom", "", 302)
	if got := s.stats(t, "custom"); got.Visits != 1 {
		t.Fatalf("HEAD counted: %+v", got)
	}
	data, _ = s.request(t, "POST", "/links", `{"url":"https://example.org/auto"}`, 201)
	generated := decode[storage.Link](t, data)
	if len(generated.Code) != 7 {
		t.Fatalf("generated code: %q", generated.Code)
	}
	s.close()
	s = start(t, path)
	if got := s.stats(t, "custom"); got.Visits != 1 || got.LastVisitAt == nil || !got.LastVisitAt.Equal(*first.LastVisitAt) {
		t.Fatalf("restart lost stats: %+v", got)
	}
	s.request(t, "GET", "/custom", "", 302)
	s.request(t, "GET", "/"+generated.Code, "", 302)
	data, _ = s.request(t, "GET", "/links", "", 200)
	links := decode[[]storage.Link](t, data)
	if len(links) != 2 || links[0].Code != generated.Code || links[1].Code != "custom" {
		t.Fatalf("list: %+v", links)
	}
	s.request(t, "DELETE", "/links/custom", "", 204)
	s.close()
	s = start(t, path)
	for _, endpoint := range []string{"/custom", "/links/custom/stats"} {
		s.request(t, "GET", endpoint, "", 404)
	}
	s.request(t, "DELETE", "/links/custom", "", 404)
	s.request(t, "POST", "/reset", "", 204)
	s.close()
	s = start(t, path)
	data, _ = s.request(t, "GET", "/links", "", 200)
	if string(bytes.TrimSpace(data)) != "[]" {
		t.Fatalf("reset list: %s", data)
	}
	s.request(t, "GET", "/"+generated.Code, "", 404)
	data, _ = s.request(t, "POST", "/links", `{"url":"https://example.org","code":"custom"}`, 201)
	if got := decode[storage.Link](t, data); got.ID != 1 {
		t.Fatalf("reset ID: %+v", got)
	}
}

func TestConcurrentRedirectsOverHTTP(t *testing.T) {
	s := start(t, filepath.Join(t.TempDir(), "links.db"))
	s.request(t, "POST", "/links", `{"url":"https://example.org","code":"busy"}`, 201)
	const visits = 30
	var wg sync.WaitGroup
	for i := 0; i < visits; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Do not call Fatal from a worker goroutine.
			for _, path := range []string{"/busy", "/links/busy/stats"} {
				res, err := s.client.Get(s.server.URL + path)
				if err != nil {
					t.Error(err)
					return
				}
				_, readErr := io.Copy(io.Discard, res.Body)
				res.Body.Close()
				if readErr != nil {
					t.Error(readErr)
				}
				want := 200
				if path == "/busy" {
					want = 302
				}
				if res.StatusCode != want {
					t.Errorf("%s status %d, want %d", path, res.StatusCode, want)
				}
			}
		}()
	}
	wg.Wait()
	if got := s.stats(t, "busy"); got.Visits != visits || got.LastVisitAt == nil {
		t.Fatalf("lost visits: %+v", got)
	}
}
