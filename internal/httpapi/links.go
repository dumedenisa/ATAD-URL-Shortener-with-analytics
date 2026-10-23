package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"atad-project/internal/storage"
)

var customCode = regexp.MustCompile(`^[A-Za-z0-9_-]{4,10}$`)

func createLink(store LinkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			URL  string  `json:"url"`
			Code *string `json:"code"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "validation", "body must be a JSON object containing url and code")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			writeError(w, http.StatusBadRequest, "validation", "body must contain exactly one JSON object")
			return
		}
		parsed, err := url.Parse(input.URL)
		if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.ContainsAny(input.URL, " \t\r\n") {
			writeError(w, http.StatusBadRequest, "validation", "url must be an absolute http or https URL with a host and no unescaped whitespace")
			return
		}
		// Automatic generation is introduced in the next implementation step.
		if input.Code == nil || !customCode.MatchString(*input.Code) {
			writeError(w, http.StatusBadRequest, "validation", "code is required and must contain 4 to 10 ASCII letters, digits, hyphens or underscores")
			return
		}
		switch *input.Code {
		case "health", "version", "reset", "links":
			writeError(w, http.StatusBadRequest, "validation", "code is reserved for an application route")
			return
		}
		link, err := store.Create(r.Context(), *input.Code, input.URL)
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
