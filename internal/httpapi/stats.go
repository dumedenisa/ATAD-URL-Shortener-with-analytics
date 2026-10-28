package httpapi

import (
	"errors"
	"net/http"
	"time"

	"atad-project/internal/storage"
)

func linkStats(store LinkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		link, err := store.Get(r.Context(), r.PathValue("code"))
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "short code was not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not read link statistics")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Code        string     `json:"code"`
			Visits      int64      `json:"visits"`
			LastVisitAt *time.Time `json:"last_visit_at"`
		}{link.Code, link.Visits, link.LastVisitAt})
	}
}
