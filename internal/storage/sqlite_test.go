package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"atad-project/internal/storage"
)

func openStore(t *testing.T, path string) *storage.Store {
	t.Helper()
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestPersistenceAndSequence(t *testing.T) {
	ctx := context.Background()
	// Spaces, # and ? must be interpreted as filename characters, not a DSN.
	path := filepath.Join(t.TempDir(), "nested", "links #1?.db")
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	link, createErr := s.Create(ctx, "gofmt", "https://go.dev/doc/effective_go")
	closeErr := s.Close()
	if createErr != nil || closeErr != nil {
		t.Fatalf("create: %v; close: %v", createErr, closeErr)
	}
	if link.ID != 1 || link.CreatedAt.IsZero() || link.Visits != 0 || link.LastVisitAt != nil {
		t.Fatalf("unexpected initial metadata: %+v", link)
	}
	reopened := openStore(t, path)
	got, err := reopened.Get(ctx, "gofmt")
	if err != nil || !reflect.DeepEqual(got, link) {
		t.Fatalf("after reopening: got %+v, want %+v; error: %v", got, link, err)
	}
	if _, err := reopened.Create(ctx, "gofmt", "https://example.org"); !errors.Is(err, storage.ErrCodeTaken) {
		t.Fatalf("persisted code must remain reserved: %v", err)
	}
	next, err := reopened.Create(ctx, "next", "https://example.org")
	if err != nil || next.ID != 2 {
		t.Fatalf("next ID after reopen = %d, error: %v", next.ID, err)
	}
	for i := 0; i < 2; i++ {
		got, err := reopened.Get(ctx, "gofmt")
		if err != nil || got.Visits != 0 || got.LastVisitAt != nil {
			t.Fatalf("reading changed statistics: %+v, %v", got, err)
		}
	}
}

func TestConcurrentCodeUniqueness(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "links.db")
	stores := []*storage.Store{openStore(t, path), openStore(t, path)}
	const requests = 20
	start := make(chan struct{})
	results := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := stores[i%len(stores)].Create(ctx, "shared", "https://example.org")
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	created, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, storage.ErrCodeTaken):
			conflicts++
		default:
			t.Errorf("unexpected insert error: %v", err)
		}
	}
	if created != 1 || conflicts != requests-1 {
		t.Fatalf("created %d, conflicts %d", created, conflicts)
	}
	link, err := stores[0].Get(ctx, "shared")
	if err != nil || link.ID != 1 || link.URL != "https://example.org" {
		t.Fatalf("unexpected winner: %+v, %v", link, err)
	}
}

func TestMissingAndCaseSensitiveCodes(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "links.db"))
	ctx := context.Background()
	if _, err := s.Get(ctx, "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing code: %v", err)
	}
	for i, code := range []string{"Code", "code"} {
		link, err := s.Create(ctx, code, "https://example.org")
		if err != nil || link.ID != int64(i+1) {
			t.Fatalf("create %q: %+v, %v", code, link, err)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	if s, err := storage.Open(""); err == nil {
		_ = s.Close()
		t.Fatal("expected empty path to fail")
	}
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := storage.Open(filepath.Join(path, "links.db")); err == nil {
		_ = s.Close()
		t.Fatal("expected invalid parent directory to fail")
	}
	if s, err := storage.Open(t.TempDir()); err == nil {
		_ = s.Close()
		t.Fatal("expected database directory path to fail")
	}
}

func TestConcurrentVisitUpdatesAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "visits.db")
	stores := []*storage.Store{openStore(t, path), openStore(t, path)}
	ctx := context.Background()
	link, err := stores[0].Create(ctx, "visits", "https://example.org")
	if err != nil {
		t.Fatal(err)
	}
	const count = 30
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := stores[i%2].RecordVisit(ctx, link); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	got, err := stores[0].Get(ctx, link.Code)
	if err != nil || got.Visits != count || got.LastVisitAt == nil {
		t.Fatalf("visits: %+v, %v", got, err)
	}
	if err := stores[0].Reset(ctx); err != nil {
		t.Fatal(err)
	}
	replacement, err := stores[0].Create(ctx, link.Code, link.URL)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID != link.ID {
		t.Fatal("test requires reused ID")
	}
	if err := stores[1].RecordVisit(ctx, link); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("stale visit affected replacement: %v", err)
	}
	got, err = stores[0].Get(ctx, link.Code)
	if err != nil || got.Visits != 0 || got.LastVisitAt != nil {
		t.Fatalf("replacement stats: %+v, %v", got, err)
	}
	if err := stores[0].RecordVisit(ctx, replacement); err != nil {
		t.Fatal(err)
	}
}
