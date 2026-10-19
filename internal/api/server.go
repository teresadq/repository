// Package api exposes the console JSON API, artifact protocol endpoints and
// the embedded single-page console.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"kkrepo-go/internal/gomod"
	"kkrepo-go/internal/observability"
	"kkrepo-go/internal/security"
	"kkrepo-go/internal/service"
	"kkrepo-go/internal/store"
)

// Options configures the HTTP server.
type Options struct {
	Store     *store.Store
	Service   *service.Service
	Auth      *Auth
	Metrics   *observability.Recorder
	Assets    fs.FS
	Version   string
	GoVersion string
	DataDir   string
	Logger    *slog.Logger
	PublicURL string
}

// Server is the HTTP application.
type Server struct {
	store     *store.Store
	service   *service.Service
	auth      *Auth
	metrics   *observability.Recorder
	assets    fs.FS
	version   string
	goVersion string
	dataDir   string
	logger    *slog.Logger
	started   time.Time
	publicURL string
}

// New builds an HTTP server.
func New(options Options) *Server {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	metrics := options.Metrics
	if metrics == nil {
		metrics = observability.New(1000)
	}
	return &Server{
		store:     options.Store,
		service:   options.Service,
		auth:      options.Auth,
		metrics:   metrics,
		assets:    options.Assets,
		version:   options.Version,
		goVersion: options.GoVersion,
		dataDir:   options.DataDir,
		logger:    logger,
		started:   time.Now().UTC(),
		publicURL: strings.TrimRight(options.PublicURL, "/"),
	}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/auth/options", s.handleAuthOptions)
	mux.HandleFunc("GET /api/oidc/login", s.handleOIDCLogin)
	mux.HandleFunc("GET /api/oidc/callback", s.handleOIDCCallback)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/activity", s.handleActivity)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/recipes", s.handleRecipes)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.HandleFunc("GET /api/audit", s.handleAudit)
	mux.HandleFunc("GET /api/repositories", s.handleListRepositories)
	mux.HandleFunc("POST /api/repositories", s.handleCreateRepository)
	mux.HandleFunc("GET /api/repositories/{name}", s.handleGetRepository)
	mux.HandleFunc("PUT /api/repositories/{name}", s.handleUpdateRepository)
	mux.HandleFunc("DELETE /api/repositories/{name}", s.handleDeleteRepository)
	mux.HandleFunc("GET /api/repositories/{name}/components", s.handleComponents)
	mux.HandleFunc("GET /api/repositories/{name}/assets", s.handleAssets)
	mux.HandleFunc("GET /api/repositories/{name}/browse", s.handleBrowse)

	mux.HandleFunc("GET /api/security/me", s.handleSecurityMe)
	mux.HandleFunc("GET /api/users", s.handleListUsers)
	mux.HandleFunc("POST /api/users", s.handleCreateUser)
	mux.HandleFunc("PUT /api/users/{username}", s.handleUpdateUser)
	mux.HandleFunc("DELETE /api/users/{username}", s.handleDeleteUser)
	mux.HandleFunc("GET /api/roles", s.handleListRoles)
	mux.HandleFunc("POST /api/roles", s.handleCreateRole)
	mux.HandleFunc("PUT /api/roles/{name}", s.handleUpdateRole)
	mux.HandleFunc("DELETE /api/roles/{name}", s.handleDeleteRole)
	mux.HandleFunc("GET /api/groups", s.handleListGroups)
	mux.HandleFunc("POST /api/groups", s.handleCreateGroup)
	mux.HandleFunc("PUT /api/groups/{name}", s.handleUpdateGroup)
	mux.HandleFunc("DELETE /api/groups/{name}", s.handleDeleteGroup)
	mux.HandleFunc("GET /api/tokens", s.handleListTokens)
	mux.HandleFunc("POST /api/tokens", s.handleCreateToken)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.handleDeleteToken)
	mux.HandleFunc("GET /api/security/anonymous", s.handleAnonymousSettings)
	mux.HandleFunc("PUT /api/security/anonymous", s.handleUpdateAnonymousSettings)

	mux.HandleFunc("GET /api/backup/export", s.handleExportBackup)
	mux.HandleFunc("POST /api/backup/import", s.handleImportBackup)

	mux.HandleFunc("GET /api/content-selectors", s.handleListContentSelectors)
	mux.HandleFunc("POST /api/content-selectors", s.handleCreateContentSelector)
	mux.HandleFunc("PUT /api/content-selectors/{name}", s.handleUpdateContentSelector)
	mux.HandleFunc("DELETE /api/content-selectors/{name}", s.handleDeleteContentSelector)
	mux.HandleFunc("POST /api/content-selectors/test", s.handleTestContentSelector)

	mux.HandleFunc("GET /api/cleanup/policies", s.handleListCleanupPolicies)
	mux.HandleFunc("POST /api/cleanup/policies", s.handleCreateCleanupPolicy)
	mux.HandleFunc("PUT /api/cleanup/policies/{name}", s.handleUpdateCleanupPolicy)
	mux.HandleFunc("DELETE /api/cleanup/policies/{name}", s.handleDeleteCleanupPolicy)
	mux.HandleFunc("POST /api/cleanup/policies/{name}/preview", s.handlePreviewCleanup)
	mux.HandleFunc("POST /api/cleanup/policies/{name}/run", s.handleRunCleanup)
	mux.HandleFunc("GET /api/cleanup/runs", s.handleListCleanupRuns)

	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("/repository/", s.handleRepositoryProtocol)
	mux.HandleFunc("/", s.handleFrontend)

	return s.withMiddleware(mux)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")

		principal, authenticated := s.auth.AuthenticateAny(r)
		r = WithPrincipal(r, principal)

		if strings.HasPrefix(r.URL.Path, "/api/") && !isPublicAPI(r.URL.Path) {
			if !authenticated || principal.Anonymous {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Authentication required"})
				return
			}
		}

		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		repository, format := s.requestLabels(r.URL.Path)
		s.metrics.RecordRequest(observability.RequestLog{
			At: started.UTC(), Method: r.Method, Path: r.URL.Path, Repository: repository,
			Format: format, Status: status, Bytes: recorder.bytes,
			DurationMS: time.Since(started).Milliseconds(), User: principal.Username,
			IP: clientIP(r),
		})
		if !strings.HasPrefix(r.URL.Path, "/repository/") {
			s.logger.Info("request",
				"method", r.Method, "path", r.URL.Path, "status", status,
				"user", principal.Username, "duration", time.Since(started).String())
		}
	})
}

func isPublicAPI(path string) bool {
	switch path {
	case "/api/health", "/api/login", "/api/session", "/api/auth/options",
		"/api/oidc/login", "/api/oidc/callback":
		return true
	}
	return false
}

func (s *Server) requestLabels(requestPath string) (string, string) {
	if !strings.HasPrefix(requestPath, "/repository/") {
		return "", ""
	}
	rest := strings.TrimPrefix(requestPath, "/repository/")
	name := rest
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		name = rest[:slash]
	}
	if repository, err := s.store.GetRepository(name); err == nil {
		return repository.Name, repository.Format
	}
	return name, ""
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"version":       s.version,
		"goVersion":     s.goVersion,
		"uptimeSeconds": int64(time.Since(s.started).Seconds()),
		"time":          time.Now().UTC(),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid login request")
		return
	}
	principal, ok := s.auth.Login(strings.TrimSpace(body.Username), body.Password)
	if !ok {
		s.store.AppendAudit(store.AuditEntry{
			User: body.Username, Action: "security.login", Status: http.StatusUnauthorized,
			Message: "invalid credentials", IP: clientIP(r),
		})
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	s.auth.SetSession(w, principal.Username)
	s.store.AppendAudit(store.AuditEntry{
		User: principal.Username, Action: "security.login", Status: http.StatusOK, IP: clientIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "user": principal})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.ClearSession(w)
	principal := PrincipalFrom(r)
	if principal.Username != "" {
		s.store.AppendAudit(store.AuditEntry{User: principal.Username, Action: "security.logout", Status: http.StatusOK})
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if principal.Anonymous || principal.Username == "" {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "user": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "user": principal})
}

func (s *Server) handleOverview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"overview":      s.store.Overview(14),
		"repositories":  s.repositorySummaries(),
		"recent":        s.activityViews(12),
		"uptimeSeconds": int64(time.Since(s.started).Seconds()),
	})
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.activityViews(parseLimit(r, 30, 200))})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	overview := s.store.Overview(14)
	repos := s.store.ListRepositories()
	states := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		entry := map[string]any{"name": repo.Name, "type": repo.Type, "online": repo.Online, "format": repo.Format}
		if repo.Type == "proxy" {
			entry["proxyState"] = proxyStateViewFrom(s.store.ProxyState(repo.Name))
		}
		states = append(states, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "version": s.version, "goVersion": s.goVersion,
		"uptimeSeconds": int64(time.Since(s.started).Seconds()), "dataDir": s.dataDir,
		"repositories": states, "overview": overview,
		"security": map[string]int{
			"users": len(s.store.ListUsers()), "roles": len(s.store.ListRoles()),
			"groups": len(s.store.ListGroups()), "tokens": len(s.store.ListTokens()),
		},
	})
}

func (s *Server) handleListRepositories(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.repositorySummaries()})
}

func (s *Server) handleGetRepository(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.GetRepository(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	writeJSON(w, http.StatusOK, s.repositoryView(repo))
}

func (s *Server) handleCreateRepository(w http.ResponseWriter, r *http.Request) {
	if !s.canAdminRepositories(r) {
		writeError(w, http.StatusForbidden, "Repository administration privilege is required")
		return
	}
	var body repositoryRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid repository payload")
		return
	}
	repo, err := s.buildRepository(body, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.CreateRepository(repo)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "Repository already exists: "+repo.Name)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "repository.create", created.Name, created.Format, "", http.StatusCreated)
	writeJSON(w, http.StatusCreated, s.repositoryView(created))
}

func (s *Server) handleUpdateRepository(w http.ResponseWriter, r *http.Request) {
	existing, err := s.store.GetRepository(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	if !s.canAdminRepository(r, existing) {
		writeError(w, http.StatusForbidden, "Repository administration privilege is required")
		return
	}
	var body repositoryRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid repository payload")
		return
	}
	body.Name = existing.Name
	repo, err := s.buildRepository(body, existing)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.store.UpdateRepository(repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "repository.update", updated.Name, updated.Format, "", http.StatusOK)
	writeJSON(w, http.StatusOK, s.repositoryView(updated))
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	existing, err := s.store.GetRepository(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	if !s.canAdminRepository(r, existing) {
		writeError(w, http.StatusForbidden, "Repository administration privilege is required")
		return
	}
	for _, other := range s.store.ListRepositories() {
		if other.Type != "group" {
			continue
		}
		for _, member := range other.MemberNames() {
			if member == existing.Name {
				writeError(w, http.StatusConflict,
					fmt.Sprintf("Repository %s is a member of group %s", existing.Name, other.Name))
				return
			}
		}
	}
	if err := s.store.DeleteRepository(existing.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "repository.delete", existing.Name, existing.Format, "", http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleComponents(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.GetRepository(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	principal := PrincipalFrom(r)
	if !principal.IsAdmin() && !principal.CanBrowse(repo.Format, repo.Name) {
		writeError(w, http.StatusForbidden, "Browse privilege is required")
		return
	}
	components := s.store.ListComponents(repo.Name,
		strings.TrimSpace(r.URL.Query().Get("module")), strings.TrimSpace(r.URL.Query().Get("q")))
	sortComponents(components)
	offset, limit := parsePage(r, 100, 500)
	items := make([]componentView, 0, limit)
	for index := offset; index < len(components) && index < offset+limit; index++ {
		items = append(items, s.componentView(repo, components[index]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": len(components), "offset": offset, "limit": limit,
	})
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.GetRepository(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	principal := PrincipalFrom(r)
	if !principal.IsAdmin() && !principal.CanBrowse(repo.Format, repo.Name) {
		writeError(w, http.StatusForbidden, "Browse privilege is required")
		return
	}
	assets := s.store.ListAssets(repo.Name, strings.TrimSpace(r.URL.Query().Get("q")))
	offset, limit := parsePage(r, 100, 500)
	items := make([]assetView, 0, limit)
	for index := offset; index < len(assets) && index < offset+limit; index++ {
		items = append(items, s.assetView(assets[index]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": len(assets), "offset": offset, "limit": limit,
	})
}

func (s *Server) handleRepositoryProtocol(w http.ResponseWriter, r *http.Request) {
	escaped := r.URL.EscapedPath()
	rest := strings.TrimPrefix(escaped, "/repository/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	name := rest
	escapedPath := ""
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		name = rest[:slash]
		escapedPath = rest[slash+1:]
	}
	decodedName, err := url.PathUnescape(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid repository name")
		return
	}
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid artifact path")
		return
	}
	repository, err := s.store.GetRepository(decodedName)
	if err != nil {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	principal := PrincipalFrom(r)
	writeRequest := r.Method == http.MethodPut || r.Method == http.MethodPost ||
		r.Method == http.MethodPatch || r.Method == http.MethodDelete
	isLFSBatch := strings.HasSuffix(decodedPath, "info/lfs/objects/batch")
	resolver := s.selectorResolver()
	if writeRequest && !isLFSBatch {
		if !principal.IsAdmin() && !principal.CanPath(repository.Format, repository.Name, "write", decodedPath, resolver) {
			s.writePermissionError(w, principal)
			return
		}
	} else if !principal.IsAdmin() && !principal.CanPath(repository.Format, repository.Name, "read", decodedPath, resolver) {
		s.writePermissionError(w, principal)
		return
	}

	if decodedPath == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.writeRepositoryInfo(w, r, repository)
		return
	}
	request := &service.Request{
		Method: r.Method, Path: decodedPath, EscapedPath: escapedPath,
		Query: r.URL.Query(), Header: r.Header.Clone(), Body: r.Body,
		HeadOnly: r.Method == http.MethodHead, BaseURL: s.baseURL(r),
		RemoteAddr: clientIP(r), User: principal.Username,
	}
	response, err := s.service.ServeRepository(decodedName, request)
	if err != nil {
		s.writeServiceError(w, err)
		if writeRequest {
			s.audit(r, "artifact.write", decodedName, repository.Format, decodedPath, statusForError(err))
		}
		return
	}
	defer response.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.Status)
	if r.Method != http.MethodHead && response.Body != nil {
		if _, err := io.Copy(w, response.Body); err != nil {
			s.logger.Warn("stream response failed", "path", r.URL.Path, "error", err)
		}
	}
	if r.Method == http.MethodGet && decodedPath != "" && response.Status < 400 {
		s.store.TouchDownload(decodedName, decodedPath, time.Now())
	}
	if writeRequest {
		action := "artifact.write"
		if r.Method == http.MethodDelete {
			action = "artifact.delete"
		}
		s.audit(r, action, decodedName, repository.Format, decodedPath, response.Status)
	}
}

func (s *Server) writePermissionError(w http.ResponseWriter, principal security.Principal) {
	w.Header().Set("WWW-Authenticate", `Basic realm="kkrepo-go"`)
	if principal.Anonymous {
		writeError(w, http.StatusUnauthorized, "Authentication required for this repository")
		return
	}
	writeError(w, http.StatusForbidden, "Repository privilege is required")
}

func (s *Server) writeRepositoryInfo(w http.ResponseWriter, r *http.Request, repo *store.Repository) {
	base := s.baseURL(r)
	var builder strings.Builder
	fmt.Fprintf(&builder, "kkrepo-go repository: %s\n", repo.Name)
	fmt.Fprintf(&builder, "format: %s\ntype: %s\nonline: %t\n", repo.Format, repo.Type, repo.Online)
	if repo.Type == "proxy" && repo.Proxy != nil {
		fmt.Fprintf(&builder, "remote: %s\n", repo.Proxy.RemoteURL)
	}
	if repo.Type == "group" {
		fmt.Fprintf(&builder, "members: %s\n", strings.Join(repo.MemberNames(), ", "))
	}
	builder.WriteString("\nURL=" + base + "/repository/" + repo.Name + "\n")
	if repo.Format == "go" {
		builder.WriteString("GOPROXY=" + base + "/repository/" + repo.Name + "\n")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, builder.String())
}

func (s *Server) writeServiceError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := err.Error()
	var serviceErr *service.Error
	if errors.As(err, &serviceErr) {
		status = serviceErr.Status
		message = serviceErr.Message
	} else if errors.Is(err, store.ErrQuota) {
		status = http.StatusRequestEntityTooLarge
		message = err.Error()
	} else {
		s.logger.Error("protocol error", "error", err)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message)
}

func statusForError(err error) int {
	var serviceErr *service.Error
	if errors.As(err, &serviceErr) {
		return serviceErr.Status
	}
	return http.StatusInternalServerError
}

func (s *Server) baseURL(r *http.Request) string {
	if s.publicURL != "" {
		return s.publicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "API endpoint not found")
		return
	}
	requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requestPath == "" || requestPath == "." {
		requestPath = "index.html"
	}
	content, err := fs.ReadFile(s.assets, requestPath)
	if err != nil {
		content, err = fs.ReadFile(s.assets, "index.html")
		if err != nil {
			writeError(w, http.StatusNotFound, "Console assets not found")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
		return
	}
	w.Header().Set("Content-Type", contentTypeFor(requestPath))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func contentTypeFor(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
		r.ResponseWriter.WriteHeader(status)
	}
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(body)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}

func parseLimit(r *http.Request, fallback, max int) int {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > max {
		return max
	}
	return parsed
}

func parsePage(r *http.Request, defaultLimit, maxLimit int) (int, int) {
	limit := parseLimit(r, defaultLimit, maxLimit)
	offset := 0
	if value := strings.TrimSpace(r.URL.Query().Get("offset")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			offset = parsed
		}
	}
	if offset < 0 {
		offset = 0
	}
	return offset, limit
}

func sortComponents(components []*store.Component) {
	sort.Slice(components, func(i, j int) bool {
		if components[i].Module != components[j].Module {
			return components[i].Module < components[j].Module
		}
		if comparison := gomod.CompareVersions(components[i].Version, components[j].Version); comparison != 0 {
			return comparison > 0
		}
		return components[i].Version > components[j].Version
	})
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host := r.RemoteAddr
	if index := strings.LastIndex(host, ":"); index > 0 {
		host = host[:index]
	}
	return strings.Trim(host, "[]")
}

func (s *Server) audit(r *http.Request, action, repository, format, path string, status int) {
	principal := PrincipalFrom(r)
	s.store.AppendAudit(store.AuditEntry{
		User: principal.Username, IP: clientIP(r), Action: action,
		Repository: repository, Format: format, Path: path, Status: status,
	})
}

func (s *Server) canAdminRepositories(r *http.Request) bool {
	principal := PrincipalFrom(r)
	return principal.IsAdmin() || principal.Can("nexus:repository-admin:*:*:add")
}

func (s *Server) canAdminRepository(r *http.Request, repo *store.Repository) bool {
	principal := PrincipalFrom(r)
	return principal.IsAdmin() || principal.CanAdminRepository(repo.Format, repo.Name)
}

func (s *Server) requireSecurity(w http.ResponseWriter, r *http.Request, action string) bool {
	principal := PrincipalFrom(r)
	if principal.IsAdmin() || principal.CanSecurity(action) {
		return true
	}
	writeError(w, http.StatusForbidden, "Security administration privilege is required")
	return false
}
