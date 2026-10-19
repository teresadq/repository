package api

import (
	"net/http"
	"time"

	"kkrepo-go/internal/store"
)

func (s *Server) handleExportBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	filename := "kkrepo-go-backup-" + time.Now().UTC().Format("20060102-150405") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.WriteHeader(http.StatusOK)
	if err := s.store.Export(w); err != nil {
		s.logger.Error("backup export failed", "error", err)
	}
}

func (s *Server) handleImportBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "replace"
	}
	force := r.URL.Query().Get("force") == "true"
	if mode == "replace" && !force {
		writeError(w, http.StatusBadRequest, "replace import requires force=true")
		return
	}
	body := http.MaxBytesReader(w, r.Body, 32<<30)
	result, err := s.store.ImportArchive(body, store.ImportOptions{Mode: mode, Force: force})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "backup.import", "", "", mode, http.StatusOK)
	writeJSON(w, http.StatusOK, result)
}
