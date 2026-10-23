// Package storage persists links in SQLite. URL and code validation belong to
// the application layer; uniqueness is enforced by the database itself.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrCodeTaken = errors.New("short code already exists")
	ErrNotFound  = errors.New("link not found")
)

// Link includes the metadata and statistics stored for a short code.
type Link struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	URL         string     `json:"url"`
	CreatedAt   time.Time  `json:"created_at"`
	Visits      int64      `json:"visits"`
	LastVisitAt *time.Time `json:"last_visit_at"`
}

// Store owns its database connections. Close it when the application stops.
type Store struct {
	db *sql.DB
}

// Open creates a file-backed database and its parent directory if necessary.
// A busy timeout lets separate connections wait for a concurrent writer.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	dsn := url.URL{Scheme: "file", Path: absolute}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(FULL)")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// Serialize this process's writes; uniqueness also holds across stores.
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT NOT NULL UNIQUE,
		url TEXT NOT NULL,
		created_at TEXT NOT NULL,
		visits INTEGER NOT NULL DEFAULT 0 CHECK (visits >= 0),
		last_visit_at TEXT
	)`)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Create atomically inserts a link. It never overwrites an existing code.
// SQLite assigns IDs starting at 1 and persists the sequence across restarts.
func (s *Store) Create(ctx context.Context, code, destination string) (Link, error) {
	created := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO links (code, url, created_at) VALUES (?, ?, ?)`,
		code, destination, created.Format(time.RFC3339Nano))
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return Link{}, ErrCodeTaken
		}
		return Link{}, fmt.Errorf("create link: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Link{}, fmt.Errorf("get link id: %w", err)
	}
	return Link{ID: id, Code: code, URL: destination, CreatedAt: created}, nil
}

// Get reads a link without changing its visit statistics.
func (s *Store) Get(ctx context.Context, code string) (Link, error) {
	var link Link
	var created string
	var last sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, code, url, created_at, visits, last_visit_at FROM links WHERE code = ?`,
		code).Scan(&link.ID, &link.Code, &link.URL, &created, &link.Visits, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, fmt.Errorf("get link: %w", err)
	}
	link.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Link{}, fmt.Errorf("decode creation time: %w", err)
	}
	if last.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, last.String)
		if err != nil {
			return Link{}, fmt.Errorf("decode last visit time: %w", err)
		}
		link.LastVisitAt = &parsed
	}
	return link, nil
}
