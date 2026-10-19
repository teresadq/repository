package api

import (
	"net/http"
	"strings"

	"kkrepo-go/internal/store"
)

type cleanupPolicyRequest struct {
	Name                        string `json:"name"`
	Repository                  string `json:"repository"`
	Enabled                     *bool  `json:"enabled"`
	Pattern                     string `json:"pattern"`
	PublishedOlderThanDays      int    `json:"publishedOlderThanDays"`
	LastDownloadedOlderThanDays int    `json:"lastDownloadedOlderThanDays"`
	RetainCount                 int    `json:"retainCount"`
}

func (s *Server) handleListCleanupPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.ListCleanupPolicies()})
}

func (s *Server) handleCreateCleanupPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	var body cleanupPolicyRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid cleanup policy")
		return
	}
	policy, err := s.cleanupPolicy(&body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.PutCleanupPolicy(policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "cleanup.policy.create", "", "", created.Name, http.StatusCreated)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateCleanupPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	existing, err := s.store.GetCleanupPolicy(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Cleanup policy not found")
		return
	}
	var body cleanupPolicyRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid cleanup policy")
		return
	}
	body.Name = existing.Name
	if body.Repository == "" {
		body.Repository = existing.Repository
	}
	policy, err := s.cleanupPolicy(&body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	policy.CreatedAt = existing.CreatedAt
	updated, err := s.store.PutCleanupPolicy(policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "cleanup.policy.update", "", "", updated.Name, http.StatusOK)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteCleanupPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	if err := s.store.DeleteCleanupPolicy(r.PathValue("name")); err != nil {
		writeError(w, http.StatusNotFound, "Cleanup policy not found")
		return
	}
	s.audit(r, "cleanup.policy.delete", "", "", r.PathValue("name"), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePreviewCleanup(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	run, err := s.store.RunCleanup(r.PathValue("name"), true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleRunCleanup(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	run, err := s.store.RunCleanup(r.PathValue("name"), false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "cleanup.run", run.Policy, "", "", http.StatusOK)
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListCleanupRuns(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.ListCleanupRuns(parseLimit(r, 50, 200))})
}

func (s *Server) cleanupPolicy(body *cleanupPolicyRequest) (*store.CleanupPolicy, error) {
	name := strings.TrimSpace(body.Name)
	repository, err := s.store.GetRepository(strings.TrimSpace(body.Repository))
	if err != nil {
		return nil, store.ErrNotFound
	}
	if name == "" {
		return nil, errInvalid("policy name is required")
	}
	if body.PublishedOlderThanDays < 0 || body.LastDownloadedOlderThanDays < 0 || body.RetainCount < 0 {
		return nil, errInvalid("cleanup thresholds must be non-negative")
	}
	if body.Pattern == "" && body.PublishedOlderThanDays == 0 &&
		body.LastDownloadedOlderThanDays == 0 && body.RetainCount == 0 {
		return nil, errInvalid("at least one cleanup criterion is required")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	return &store.CleanupPolicy{
		Name: name, Repository: repository.Name, Format: repository.Format, Enabled: enabled,
		Pattern:                     strings.TrimSpace(body.Pattern),
		PublishedOlderThanDays:      body.PublishedOlderThanDays,
		LastDownloadedOlderThanDays: body.LastDownloadedOlderThanDays,
		RetainCount:                 body.RetainCount,
	}, nil
}

type invalidError string

func (e invalidError) Error() string { return string(e) }

func errInvalid(message string) error { return invalidError(message) }
