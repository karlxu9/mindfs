package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"mindfs/server/internal/backup"
)

// handleBackupExport streams a backup archive (R-5.1). Parameters travel in
// the query instead of a JSON body: the response is a binary zip, so the
// endpoint authenticates like /api/file (request proof) rather than through
// the JSON-encrypting protectedEndpoint wrapper.
func (h *HTTPHandler) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireRequestProof(r); err != nil {
		respondError(w, http.StatusUnauthorized, err)
		return
	}
	query := r.URL.Query()
	scope := strings.TrimSpace(query.Get("scope"))
	if scope == "" {
		scope = "all"
	}
	input := backup.ExportInput{
		Scope:              scope,
		RootID:             strings.TrimSpace(query.Get("root")),
		IncludeCredentials: query.Get("include_credentials") == "1" || strings.EqualFold(query.Get("include_credentials"), "true"),
		Version:            h.Version,
	}

	// Build into a temp file first: a failure mid-archive can still produce a
	// clean error response, and the download gets a Content-Length.
	tmp, err := os.CreateTemp("", "mindfs-export-*.zip")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := h.service().ExportBackup(r.Context(), tmp, input); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	filename := "mindfs-backup-" + time.Now().UTC().Format("20060102-150405") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if _, err := io.Copy(w, tmp); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		// The response is already streaming; nothing better to do than log-free
		// abort (client sees the truncated download).
		return
	}
}


// handleStorageReport serves the per-root storage checkup (R-5.3). Walking
// the metadata directory may take a second on large roots; the handler runs
// per-request so other requests are not blocked (N-4).
func (h *HTTPHandler) handleStorageReport(w http.ResponseWriter, r *http.Request) {
	rootID := strings.TrimSpace(r.URL.Query().Get("root"))
	if rootID == "" {
		respondError(w, http.StatusBadRequest, errors.New("root required"))
		return
	}
	report, err := h.service().StorageReport(r.Context(), rootID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	respondJSON(w, http.StatusOK, report)
}

func (h *HTTPHandler) handleStorageCleanup(w http.ResponseWriter, r *http.Request) {
	rootID := strings.TrimSpace(r.URL.Query().Get("root"))
	if rootID == "" {
		respondError(w, http.StatusBadRequest, errors.New("root required"))
		return
	}
	result, err := h.service().StorageCleanup(r.Context(), rootID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}
