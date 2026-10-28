package httpapi

import (
	"errors"
	"net/http"

	"atad-project/internal/storage"
)

func redirectLink(store LinkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		// GET wildcards also match names used by POST-only application routes.
		// Keep those names out of short-code resolution until their GET exists.
		if reservedCode(code) {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "this route does not support GET")
			return
		}
		link, err := store.Get(r.Context(), code)
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "short code was not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not resolve short code")
			return
		}
		// A broken stored destination is distinct from an unknown code. Do not
		// fetch the remote URL: destination availability is outside this service.
		if !validDestination(link.URL) {
			writeError(w, http.StatusUnprocessableEntity, "unprocessable", "stored link has an invalid destination URL")
			return
		}
		// Persist before serving the redirect: a failed write must not produce
		// an uncounted 302. HEAD is a metadata request and does not count.
		if r.Method == http.MethodGet {
			if err := store.RecordVisit(r.Context(), link); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					writeError(w, http.StatusNotFound, "not_found", "short code was removed before the redirect")
				} else {
					writeError(w, http.StatusInternalServerError, "internal", "could not record visit")
				}
				return
			}
		}
		http.Redirect(w, r, link.URL, http.StatusFound)
	}
}
