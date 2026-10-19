package api

import (
	"net/http"
	"strings"

	"kkrepo-go/internal/security"
	"kkrepo-go/internal/store"
)

type selectorRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
}

type selectorTestRequest struct {
	Expression string `json:"expression"`
	Format     string `json:"format"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
}

func (s *Server) handleListContentSelectors(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.ListContentSelectors()})
}

func (s *Server) handleCreateContentSelector(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	var body selectorRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid content selector")
		return
	}
	selector, err := s.contentSelector(&body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.PutContentSelector(selector)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.selector.create", "", "", created.Name, http.StatusCreated)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateContentSelector(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	existing, err := s.store.GetContentSelector(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Content selector not found")
		return
	}
	var body selectorRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid content selector")
		return
	}
	body.Name = existing.Name
	selector, err := s.contentSelector(&body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	selector.CreatedAt = existing.CreatedAt
	updated, err := s.store.PutContentSelector(selector)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.selector.update", "", "", updated.Name, http.StatusOK)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteContentSelector(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	if err := s.store.DeleteContentSelector(r.PathValue("name")); err != nil {
		writeError(w, http.StatusNotFound, "Content selector not found")
		return
	}
	s.audit(r, "security.selector.delete", "", "", r.PathValue("name"), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestContentSelector(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	var body selectorTestRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid selector test")
		return
	}
	matched, err := security.MatchSelector(body.Expression, security.SelectorContext{
		Path: body.Path, Format: body.Format, Repository: body.Repository,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matched": matched})
}

func (s *Server) contentSelector(body *selectorRequest) (*store.ContentSelector, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return nil, errInvalid("selector name is required")
	}
	expression := strings.TrimSpace(body.Expression)
	if expression == "" {
		return nil, errInvalid("selector expression is required")
	}
	if err := security.ValidateSelector(expression); err != nil {
		return nil, errInvalid("invalid selector expression: " + err.Error())
	}
	return &store.ContentSelector{
		Name: name, Description: strings.TrimSpace(body.Description), Expression: expression,
	}, nil
}

func (s *Server) selectorResolver() security.SelectorResolver {
	return func(name string) (string, bool) {
		selector, err := s.store.GetContentSelector(name)
		if err != nil {
			return "", false
		}
		return selector.Expression, true
	}
}
