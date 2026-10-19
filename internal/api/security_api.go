package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/observability"
	"kkrepo-go/internal/security"
	"kkrepo-go/internal/store"
)

type userRequest struct {
	Username string   `json:"username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Roles    []string `json:"roles"`
	Groups   []string `json:"groups"`
	Active   *bool    `json:"active"`
}

type roleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Privileges  []string `json:"privileges"`
}

type securityGroupRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
}

type tokenRequest struct {
	Name      string     `json:"name"`
	Username  string     `json:"username"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type anonymousRequest struct {
	Enabled bool     `json:"enabled"`
	Roles   []string `json:"roles"`
}

func (s *Server) handleSecurityMe(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	writeJSON(w, http.StatusOK, principal)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	users := s.store.ListUsers()
	items := make([]map[string]any, 0, len(users))
	for _, user := range users {
		items = append(items, userView(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "create") {
		return
	}
	var body userRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid user payload")
		return
	}
	username := strings.TrimSpace(body.Username)
	if username == "" || strings.ContainsAny(username, " /\\\t\n") {
		writeError(w, http.StatusBadRequest, "用户名不能为空或包含空白/斜杠")
		return
	}
	if len(body.Password) < 6 {
		writeError(w, http.StatusBadRequest, "密码至少需要 6 位")
		return
	}
	if err := s.validateRolesAndGroups(body.Roles, body.Groups); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := security.HashPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	user, err := s.store.PutUser(&store.User{
		Username: username, Name: firstNonEmpty(body.Name, username), Email: body.Email,
		PasswordHash: hash, Roles: body.Roles, Groups: body.Groups, Active: active,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "security.user.create", "", "", user.Username, http.StatusCreated)
	writeJSON(w, http.StatusCreated, userView(user))
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	username := r.PathValue("username")
	existing, err := s.store.GetUser(username)
	if err != nil {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}
	var body userRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid user payload")
		return
	}
	if err := s.validateRolesAndGroups(body.Roles, body.Groups); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := *existing
	if body.Name != "" {
		user.Name = body.Name
	}
	if body.Email != "" {
		user.Email = body.Email
	}
	if body.Roles != nil {
		user.Roles = body.Roles
	}
	if body.Groups != nil {
		user.Groups = body.Groups
	}
	if body.Active != nil {
		user.Active = *body.Active
	}
	if body.Password != "" {
		if len(body.Password) < 6 {
			writeError(w, http.StatusBadRequest, "密码至少需要 6 位")
			return
		}
		hash, err := security.HashPassword(body.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		user.PasswordHash = hash
	}
	updated, err := s.store.PutUser(&user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "security.user.update", "", "", username, http.StatusOK)
	writeJSON(w, http.StatusOK, userView(updated))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "delete") {
		return
	}
	if PrincipalFrom(r).Username == r.PathValue("username") {
		writeError(w, http.StatusBadRequest, "不能删除当前登录用户")
		return
	}
	if err := s.store.DeleteUser(r.PathValue("username")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "User not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.user.delete", "", "", r.PathValue("username"), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.ListRoles()})
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "create") {
		return
	}
	var body roleRequest
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "Invalid role payload")
		return
	}
	role, err := s.store.PutRole(&store.Role{
		Name: strings.TrimSpace(body.Name), Description: body.Description, Privileges: body.Privileges,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.role.create", "", "", role.Name, http.StatusCreated)
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	existing, err := s.store.GetRole(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Role not found")
		return
	}
	var body roleRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid role payload")
		return
	}
	existing.Description = body.Description
	if body.Privileges != nil {
		existing.Privileges = body.Privileges
	}
	role, err := s.store.PutRole(existing)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.role.update", "", "", role.Name, http.StatusOK)
	writeJSON(w, http.StatusOK, role)
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "delete") {
		return
	}
	if err := s.store.DeleteRole(r.PathValue("name")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.role.delete", "", "", r.PathValue("name"), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.ListGroups()})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "create") {
		return
	}
	var body securityGroupRequest
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "Invalid group payload")
		return
	}
	group, err := s.store.PutGroup(&store.Group{
		Name: strings.TrimSpace(body.Name), Description: body.Description, Roles: body.Roles,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.group.create", "", "", group.Name, http.StatusCreated)
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	existing, err := s.store.GetGroup(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Group not found")
		return
	}
	var body securityGroupRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid group payload")
		return
	}
	existing.Description = body.Description
	if body.Roles != nil {
		existing.Roles = body.Roles
	}
	group, err := s.store.PutGroup(existing)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.group.update", "", "", group.Name, http.StatusOK)
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "delete") {
		return
	}
	if err := s.store.DeleteGroup(r.PathValue("name")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "security.group.delete", "", "", r.PathValue("name"), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	tokens := s.store.ListTokens()
	items := make([]map[string]any, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, tokenView(token))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "create") {
		return
	}
	var body tokenRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid token payload")
		return
	}
	username := strings.TrimSpace(body.Username)
	if username == "" {
		username = PrincipalFrom(r).Username
	}
	if _, err := s.store.GetUser(username); err != nil {
		writeError(w, http.StatusBadRequest, "User not found: "+username)
		return
	}
	raw := "kkr_" + store.RandomID(24)
	token := &store.Token{
		ID: store.RandomID(8), Name: firstNonEmpty(body.Name, "token"), Username: username,
		Prefix: raw[:10], Hash: store.HashToken(raw), Scopes: body.Scopes,
		ExpiresAt: body.ExpiresAt, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.PutToken(token); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "security.token.create", "", "", token.ID, http.StatusCreated)
	view := tokenView(token)
	view["token"] = raw
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "delete") {
		return
	}
	if err := s.store.DeleteToken(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "Token not found")
		return
	}
	s.audit(r, "security.token.delete", "", "", r.PathValue("id"), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	offset, limit := parsePage(r, 50, 500)
	entries, total := s.store.ListAudit(store.AuditFilter{
		Query: r.URL.Query().Get("q"), User: r.URL.Query().Get("user"),
		Action: r.URL.Query().Get("action"), Repository: r.URL.Query().Get("repository"),
		Offset: offset, Limit: limit,
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": entries, "total": total, "offset": offset, "limit": limit})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.metrics.Logs(parseLimit(r, 200, 1000))})
}

func (s *Server) handleAnonymousSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "read") {
		return
	}
	settings := s.store.SecuritySettings()
	writeJSON(w, http.StatusOK, map[string]any{"enabled": settings.AnonymousEnabled, "roles": settings.AnonymousRoles})
}

func (s *Server) handleUpdateAnonymousSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecurity(w, r, "update") {
		return
	}
	var body anonymousRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid anonymous settings")
		return
	}
	if err := s.store.UpdateSecuritySettings(store.SecuritySettings{
		AnonymousEnabled: body.Enabled, AnonymousRoles: body.Roles,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "security.anonymous.update", "", "", "", http.StatusOK)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": body.Enabled, "roles": body.Roles})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	overview := s.store.Overview(14)
	repositories := map[string]float64{
		"hosted": float64(overview.Hosted), "proxy": float64(overview.Proxy), "group": float64(overview.Groups),
	}
	blocked := 0.0
	for _, repo := range s.store.ListRepositories() {
		if s.store.IsProxyBlocked(repo.Name, time.Now()) {
			blocked++
		}
	}
	output := s.metrics.Render(observability.Snapshot{
		Repositories: repositories, Assets: float64(overview.Assets),
		Components: float64(overview.Components), StorageBytes: float64(overview.Bytes),
		ProxyBlocked: blocked, Uptime: time.Since(s.started).Seconds(),
	})
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(output))
}

func (s *Server) validateRolesAndGroups(roles, groups []string) error {
	for _, role := range roles {
		if _, err := s.store.GetRole(role); err != nil {
			return errors.New("role not found: " + role)
		}
	}
	if len(groups) > 0 {
		known := map[string]struct{}{}
		for _, group := range s.store.ListGroups() {
			known[group.Name] = struct{}{}
		}
		for _, group := range groups {
			if _, ok := known[group]; !ok {
				return errors.New("group not found: " + group)
			}
		}
	}
	return nil
}

func userView(user *store.User) map[string]any {
	roles := append([]string{}, user.Roles...)
	groups := append([]string{}, user.Groups...)
	return map[string]any{
		"username": user.Username, "name": user.Name, "email": user.Email,
		"roles": roles, "groups": groups, "active": user.Active,
		"createdAt": user.CreatedAt, "updatedAt": user.UpdatedAt,
	}
}

func tokenView(token *store.Token) map[string]any {
	return map[string]any{
		"id": token.ID, "name": token.Name, "username": token.Username, "prefix": token.Prefix,
		"scopes": token.Scopes, "expiresAt": token.ExpiresAt,
		"createdAt": token.CreatedAt, "lastUsedAt": token.LastUsedAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var _ = security.Principal{}
