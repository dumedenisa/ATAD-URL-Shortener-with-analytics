package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"atad-project/internal/storage"
)

var customCode = regexp.MustCompile(`^[A-Za-z0-9_-]{4,10}$`)

func createLink(store LinkStore, generate func() (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			URL  string  `json:"url"`
			Code *string `json:"code"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "validation", "body must be a JSON object containing url and an optional code")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			writeError(w, http.StatusBadRequest, "validation", "body must contain exactly one JSON object")
			return
		}
		if !validDestination(input.URL) {
			writeError(w, http.StatusBadRequest, "validation", "url must be an absolute http or https URL with a host and no unescaped whitespace")
			return
		}
		if input.Code != nil {
			if !customCode.MatchString(*input.Code) {
				writeError(w, http.StatusBadRequest, "validation", "code must contain 4 to 10 ASCII letters, digits, hyphens or underscores")
				return
			}
			if reservedCode(*input.Code) {
				writeError(w, http.StatusBadRequest, "validation", "code is reserved for an application route")
				return
			}
		}
		var link storage.Link
		var err error
		if input.Code == nil {
			link, err = createGeneratedLink(r.Context(), store, input.URL, generate)
		} else {
			link, err = store.Create(r.Context(), *input.Code, input.URL)
		}
		if errors.Is(err, storage.ErrCodeTaken) {
			writeError(w, http.StatusConflict, "conflict", "the requested code is already taken")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not create link")
			return
		}
		// Creation exposes only the four fields specified by the project API.
		writeJSON(w, http.StatusCreated, struct {
			ID        int64     `json:"id"`
			Code      string    `json:"code"`
			URL       string    `json:"url"`
			CreatedAt time.Time `json:"created_at"`
		}{link.ID, link.Code, link.URL, link.CreatedAt})
	}
}

const (
	generatedCodeLength = 7
	maxCodeAttempts     = 32
	codeAlphabet        = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

// Each alphabet character has exactly four corresponding byte values, so this
// mapping is uniform. crypto/rand is safe for concurrent requests.
func generateCode() (string, error) {
	var data [generatedCodeLength]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	for i := range data {
		data[i] = codeAlphabet[data[i]&63]
	}
	return string(data[:]), nil
}

func reservedCode(code string) bool {
	switch code {
	case "health", "version", "reset", "links":
		return true
	}
	return false
}

// The INSERT decides uniqueness atomically. A separate existence check would
// allow concurrent requests to race. Custom codes never enter this retry loop.
func createGeneratedLink(ctx context.Context, store LinkStore, destination string, generate func() (string, error)) (storage.Link, error) {
	for attempt := 0; attempt < maxCodeAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return storage.Link{}, err
		}
		code, err := generate()
		if err != nil {
			return storage.Link{}, fmt.Errorf("generate code: %w", err)
		}
		if reservedCode(code) {
			continue
		}
		link, err := store.Create(ctx, code, destination)
		if errors.Is(err, storage.ErrCodeTaken) {
			continue
		}
		return link, err
	}
	return storage.Link{}, errors.New("could not allocate a unique code within the retry limit")
}

func validDestination(destination string) bool {
	parsed, err := url.Parse(destination)
	return err == nil && parsed.Hostname() != "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") &&
		!strings.ContainsAny(destination, " \t\r\n")
}
