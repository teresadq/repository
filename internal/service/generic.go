package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

const (
	maxGenericUploadBytes int64 = 5 << 30
)

// Request is a format-agnostic repository protocol request.
type Request struct {
	Method      string
	Path        string // decoded repository-relative path, no leading slash
	EscapedPath string // escaped path for upstream URLs
	Query       url.Values
	Header      http.Header
	Body        io.Reader
	HeadOnly    bool
	BaseURL     string
	RemoteAddr  string
	User        string
}

// ServeRepository dispatches a request to the repository's format handler.
func (s *Service) ServeRepository(repositoryName string, request *Request) (*Response, error) {
	repository, err := s.store.GetRepository(repositoryName)
	if err != nil {
		return nil, NotFound("Repository not found: " + repositoryName)
	}
	if !repository.Online {
		return nil, NotFound("Repository is offline: " + repositoryName)
	}
	return s.serveRepository(repository, request)
}

func (s *Service) serveRepository(repository *store.Repository, request *Request) (*Response, error) {
	if request.Path != "" {
		checkPath := strings.Trim(request.Path, "/")
		if spec, base, ok := checksumRequest(checkPath); ok {
			if !repositoryAllowsPath(repository, base) {
				return nil, NotFound(request.Path)
			}
			return s.serveChecksum(repository, request, spec, base)
		}
		if !repositoryAllowsPath(repository, checkPath) {
			return nil, NotFound(request.Path)
		}
	}
	switch repository.Format {
	case "go":
		switch request.Method {
		case http.MethodGet, http.MethodHead:
			if strings.TrimSpace(request.Path) == "" {
				return nil, NotFound("Repository info is served by the console")
			}
			return s.Serve(repository.Name, request.Path, request.HeadOnly)
		case http.MethodPut, http.MethodPost:
			published, err := s.Publish(repository.Name, request.Path, request.Body)
			if err != nil {
				return nil, err
			}
			body, _ := jsonBytes(published)
			return bytesResponse(http.StatusCreated, "application/json", body, "", time.Time{}, request.HeadOnly), nil
		case http.MethodDelete:
			if _, err := s.store.DeleteAsset(repository.Name, request.Path); err != nil {
				return nil, err
			}
			return bytesResponse(http.StatusNoContent, "text/plain", nil, "", time.Time{}, true), nil
		default:
			return nil, MethodNotAllowed("Unsupported method")
		}
	case "maven":
		return s.serveMaven(repository, request)
	case "npm":
		return s.serveNpm(repository, request)
	case "pypi":
		return s.servePypi(repository, request)
	case "helm":
		return s.serveHelm(repository, request)
	case "cargo":
		return s.serveCargo(repository, request)
	case "docker":
		return s.serveDocker(repository, request)
	case "gitlfs":
		return s.serveGitLFS(repository, request)
	case "pub":
		return s.servePub(repository, request)
	case "swift":
		return s.serveSwift(repository, request)
	case "conda":
		return s.serveConda(repository, request)
	case "yum":
		return s.serveYum(repository, request)
	case "apt":
		return s.serveApt(repository, request)
	case "alpine":
		return s.serveAlpine(repository, request)
	case "raw":
		return s.serveGeneric(repository, request)
	default:
		return s.serveGeneric(repository, request)
	}
}

func (s *Service) serveGeneric(repository *store.Repository, request *Request) (*Response, error) {
	switch repository.Type {
	case "hosted":
		return s.serveGenericHosted(repository, request)
	case "proxy":
		return s.serveGenericProxy(repository, request)
	case "group":
		return s.serveGenericGroup(repository, request)
	default:
		return nil, MethodNotAllowed("Unsupported repository type: " + repository.Type)
	}
}

func (s *Service) serveGenericHosted(repository *store.Repository, request *Request) (*Response, error) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		return s.serveStoredFile(repository, request.Path, request.HeadOnly, "")
	case http.MethodPut, http.MethodPost:
		return s.storeUploadedFile(repository, request)
	case http.MethodDelete:
		deleted, err := s.store.DeleteAsset(repository.Name, request.Path)
		if err != nil {
			return nil, err
		}
		if !deleted {
			return nil, NotFound(request.Path)
		}
		return bytesResponse(http.StatusNoContent, "text/plain", nil, "", time.Time{}, true), nil
	default:
		return nil, MethodNotAllowed("Unsupported method")
	}
}

func (s *Service) storeUploadedFile(repository *store.Repository, request *Request) (*Response, error) {
	if request.Body == nil {
		return nil, BadRequest("Request body is required")
	}
	path := strings.TrimLeft(request.Path, "/")
	if path == "" || strings.Contains(path, "..") || strings.ContainsRune(path, 0) {
		return nil, BadRequest("Invalid artifact path")
	}
	sum, size, _, err := s.store.SaveBlob(request.Body, maxGenericUploadBytes)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	contentType := request.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" ||
		strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		contentType = detectContentType(repository.Format, path)
	}
	now := s.now()
	namespace, componentName, componentVersion := genericComponentInfo(repository.Format, path)
	asset := &store.Asset{
		Repository:  repository.Name,
		Format:      repository.Format,
		Namespace:   namespace,
		Path:        path,
		Module:      componentName,
		Version:     componentVersion,
		Kind:        "file",
		ContentType: contentType,
		Size:        size,
		SHA256:      sum,
		Source:      "hosted",
		CreatedAt:   now,
		UpdatedAt:   now,
		Attributes:  map[string]string{"filename": path[strings.LastIndex(path, "/")+1:]},
	}
	if err := s.store.PutAsset(asset, nil, true); err != nil {
		return nil, fmt.Errorf("persist upload: %w", err)
	}
	return bytesResponse(http.StatusCreated, "text/plain", nil, "", time.Time{}, true), nil
}

func (s *Service) serveGenericProxy(repository *store.Repository, request *Request) (*Response, error) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return nil, MethodNotAllowed("Proxy repositories are read-only")
	}
	path := strings.TrimLeft(request.Path, "/")
	asset, hasCached := s.store.GetAsset(repository.Name, path)
	now := s.now()
	ttl := repository.ContentMaxAge()
	if isMetadataPath(repository.Format, path) {
		ttl = repository.MetadataMaxAge()
	}
	if hasCached && isFresh(asset, ttl, now) {
		return s.serveCachedGeneric(asset, request.HeadOnly)
	}
	if s.store.IsNotFoundCached(repository.Name, path) {
		return nil, NotFound(path)
	}
	if s.store.IsProxyBlocked(repository.Name, now) {
		if hasCached {
			return s.serveCachedGeneric(asset, request.HeadOnly)
		}
		return nil, BadUpstream("Upstream temporarily blocked: " + remoteURL(repository))
	}
	key := "generic|" + repository.Name + "|" + path
	value, err := s.flight.Do(key, func() (any, error) {
		current, hasCurrent := s.store.GetAsset(repository.Name, path)
		currentNow := s.now()
		currentTTL := repository.ContentMaxAge()
		if isMetadataPath(repository.Format, path) {
			currentTTL = repository.MetadataMaxAge()
		}
		if hasCurrent && isFresh(current, currentTTL, currentNow) {
			return current, nil
		}
		if s.store.IsNotFoundCached(repository.Name, path) {
			return nil, NotFound(path)
		}
		return s.fetchGenericProxy(repository, request, path, current, hasCurrent, currentNow)
	})
	if err != nil {
		return nil, err
	}
	cachedAsset, ok := value.(*store.Asset)
	if !ok || cachedAsset == nil {
		return nil, NotFound(path)
	}
	return s.serveCachedGeneric(cachedAsset, request.HeadOnly)
}

func (s *Service) fetchGenericProxy(repository *store.Repository, request *Request, path string, cached *store.Asset, hasCached bool, now time.Time) (*store.Asset, error) {
	upstreamPath := request.EscapedPath
	if upstreamPath == "" {
		upstreamPath = escapePath(path)
	}
	target := buildRemoteURL(remoteURL(repository), upstreamPath)
	if encoded := request.Query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	etag := ""
	var lastModified time.Time
	if hasCached {
		etag = cached.ETag
		if cached.LastModified != "" {
			if parsed, err := time.Parse(time.RFC3339, cached.LastModified); err == nil {
				lastModified = parsed
			}
		}
	}
	headers := request.Header.Clone()
	applyUpstreamAuth(headers, repository)
	var result *upstreamResult
	var err error
	if isMetadataPath(repository.Format, path) {
		result, err = s.fetchBytesWithHeaders(target, etag, lastModified, headers)
	} else {
		result, err = s.fetchFileWithHeaders(target, etag, lastModified, headers)
	}
	if err != nil {
		return s.handleGenericUpstreamFailure(repository, path, cached, hasCached,
			"Upstream IO error: "+err.Error(), now)
	}
	defer result.cleanup()
	switch {
	case result.status == 304 && hasCached:
		s.store.TouchAsset(repository.Name, path, now)
		s.store.RecordProxySuccess(repository.Name, now)
		s.store.InvalidateNotFound(repository.Name, path)
		return cached, nil
	case result.status >= 200 && result.status < 300:
		s.store.InvalidateNotFound(repository.Name, path)
		return s.persistGenericProxy(repository, path, result, now)
	case result.status == 404 || result.status == 410:
		s.store.RecordProxySuccess(repository.Name, now)
		if hasCached {
			return cached, nil
		}
		if result.status == 404 {
			ttl := time.Duration(repository.MetadataMaxAge()) * time.Minute
			if ttl <= 0 {
				ttl = 24 * time.Hour
			}
			s.store.RememberNotFound(repository.Name, path, ttl)
		}
		return nil, NotFound(path)
	case result.status >= 400 && result.status < 500:
		return nil, &Error{Status: result.status, Message: fmt.Sprintf("Upstream returned %d", result.status)}
	default:
		return s.handleGenericUpstreamFailure(repository, path, cached, hasCached,
			fmt.Sprintf("Upstream returned %d", result.status), now)
	}
}

func (s *Service) persistGenericProxy(repository *store.Repository, path string, result *upstreamResult, now time.Time) (*store.Asset, error) {
	var (
		sum  string
		size int64
		err  error
	)
	if result.tempFile != "" {
		sum, size, _, err = s.store.SaveBlobFile(result.tempFile)
	} else {
		sum, size, _, err = s.store.SaveBlobBytes(result.body)
	}
	if err != nil {
		return nil, fmt.Errorf("store proxy content: %w", err)
	}
	lastModified := ""
	if !result.lastModified.IsZero() {
		lastModified = result.lastModified.UTC().Format(time.RFC3339)
	}
	contentType := result.contentType
	if contentType == "" {
		contentType = detectContentType(repository.Format, path)
	}
	namespace, componentName, componentVersion := genericComponentInfo(repository.Format, path)
	asset := &store.Asset{
		Repository:   repository.Name,
		Format:       repository.Format,
		Namespace:    namespace,
		Path:         path,
		Module:       componentName,
		Version:      componentVersion,
		Kind:         "file",
		ContentType:  contentType,
		Size:         size,
		SHA256:       sum,
		Source:       "proxy",
		ETag:         result.etag,
		LastModified: lastModified,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.PutAsset(asset, nil, true); err != nil {
		return nil, fmt.Errorf("persist proxy asset: %w", err)
	}
	s.store.RecordProxySuccess(repository.Name, now)
	return asset, nil
}

func (s *Service) serveCachedGeneric(asset *store.Asset, headOnly bool) (*Response, error) {
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(asset.Path)
	}
	lastModified := asset.UpdatedAt
	if asset.LastModified != "" {
		if parsed, err := time.Parse(time.RFC3339, asset.LastModified); err == nil {
			lastModified = parsed
		}
	}
	return streamResponse(http.StatusOK, asset.ContentType, body, asset.Size, asset.ETag, lastModified, headOnly), nil
}

func (s *Service) handleGenericUpstreamFailure(repository *store.Repository, path string, cached *store.Asset, hasCached bool, message string, now time.Time) (*store.Asset, error) {
	failCount := s.store.ProxyState(repository.Name).FailCount
	var backoff time.Duration
	if repository.AutoBlock() {
		index := failCount
		if index >= len(upstreamBackoff) {
			index = len(upstreamBackoff) - 1
		}
		backoff = upstreamBackoff[index]
	}
	s.store.RecordProxyFailure(repository.Name, backoff, message, now)
	if hasCached {
		return cached, nil
	}
	return nil, BadUpstream(message)
}

func (s *Service) serveGenericGroup(repository *store.Repository, request *Request) (*Response, error) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return nil, MethodNotAllowed("Group repositories are read-only")
	}
	members := s.groupMembers(repository)
	var lastUpstream error
	for _, member := range members {
		response, err := s.serveRepository(member, request)
		if err == nil {
			return response, nil
		}
		if isNotFoundError(err) {
			continue
		}
		lastUpstream = err
	}
	if lastUpstream != nil {
		return nil, lastUpstream
	}
	return nil, NotFound(request.Path)
}

func (s *Service) serveStoredFile(repository *store.Repository, path string, headOnly bool, contentTypeOverride string) (*Response, error) {
	asset, ok := s.store.GetAsset(repository.Name, strings.TrimLeft(path, "/"))
	if !ok {
		return nil, NotFound(path)
	}
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(path)
	}
	contentType := asset.ContentType
	if contentTypeOverride != "" {
		contentType = contentTypeOverride
	}
	if contentType == "" {
		contentType = detectContentType(repository.Format, asset.Path)
	}
	return streamResponse(http.StatusOK, contentType, body, asset.Size, asset.SHA256, asset.UpdatedAt, headOnly), nil
}

func (s *Service) fetchBytesWithHeaders(target, etag string, lastModified time.Time, headers http.Header) (*upstreamResult, error) {
	result, err := s.fetcher.FetchBytesHeaders(context.Background(), target, etag, lastModified, maxMetadataBytes, headers)
	if err != nil {
		return nil, err
	}
	return &upstreamResult{
		status: result.Status, etag: result.ETag, lastModified: result.LastModified,
		body: result.Body, contentType: result.ContentType,
	}, nil
}

func (s *Service) fetchFileWithHeaders(target, etag string, lastModified time.Time, headers http.Header) (*upstreamResult, error) {
	result, err := s.fetcher.FetchFileHeaders(context.Background(), target, etag, lastModified,
		maxGenericUploadBytes, s.store.TempDir(), headers)
	if err != nil {
		return nil, err
	}
	return &upstreamResult{
		status: result.Status, etag: result.ETag, lastModified: result.LastModified,
		tempFile: result.TempFile, contentType: result.ContentType,
	}, nil
}

func isMetadataPath(format, path string) bool {
	if strings.HasSuffix(path, "/") {
		return true
	}
	name := path
	if index := strings.LastIndex(name, "/"); index >= 0 {
		name = name[index+1:]
	}
	lower := strings.ToLower(name)
	switch format {
	case "maven":
		return strings.HasSuffix(lower, "maven-metadata.xml") || strings.HasSuffix(lower, ".sha1") ||
			strings.HasSuffix(lower, ".md5") || strings.HasSuffix(lower, ".sha256") ||
			strings.HasSuffix(lower, ".sha512")
	case "helm":
		return lower == "index.yaml"
	case "npm":
		return !strings.Contains(path, "/-/")
	case "pypi":
		return strings.HasPrefix(path, "simple")
	case "cargo":
		return strings.HasSuffix(lower, "config.json") || strings.HasPrefix(path, "index/") ||
			(!strings.Contains(lower, "/download") && !strings.HasSuffix(lower, ".crate") && strings.HasPrefix(path, "api/v1/crates"))
	case "docker":
		return strings.Contains(path, "/manifests/") || strings.HasSuffix(path, "/tags/list")
	case "conda":
		return strings.Contains(lower, "repodata") || strings.HasSuffix(lower, "channeldata.json") ||
			strings.HasSuffix(lower, "notices.json")
	case "apt":
		return strings.Contains(path, "/dists/") || strings.HasSuffix(lower, "release") ||
			strings.Contains(lower, "packages")
	case "alpine":
		return lower == "apkindex.tar.gz" || strings.HasSuffix(lower, "apkindex.tar.gz")
	case "yum":
		return strings.Contains(path, "repodata/")
	}
	return strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".xml") ||
		strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") ||
		strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".txt")
}

func genericComponentInfo(format, path string) (namespace, name, version string) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "", format, ""
	}
	parts := strings.Split(trimmed, "/")
	switch format {
	case "maven":
		if len(parts) >= 4 {
			return strings.Join(parts[:len(parts)-3], "/"), parts[len(parts)-3], parts[len(parts)-2]
		}
		if len(parts) >= 2 {
			return "", parts[len(parts)-2], ""
		}
	case "pypi":
		if len(parts) >= 3 && parts[0] == "packages" {
			return "", parts[1], parts[2]
		}
	case "npm":
		if index := strings.Index(path, "/-/"); index > 0 {
			return npmNamespace(path[:index]), path[:index], ""
		}
	case "cargo":
		if len(parts) >= 3 && parts[0] == "crates" {
			filename := parts[len(parts)-1]
			version := strings.TrimSuffix(strings.TrimPrefix(filename, parts[1]+"-"), ".crate")
			return "", parts[1], version
		}
	case "gitlfs":
		short := trimmed
		if len(short) > 16 {
			short = short[:16]
		}
		return "", "lfs:" + short, ""
	case "docker":
		rest := strings.TrimPrefix(trimmed, "v2/")
		for _, marker := range []string{"/manifests/", "/blobs/"} {
			if index := strings.Index(rest, marker); index > 0 {
				image := rest[:index]
				reference := rest[index+len(marker):]
				ns, n := dockerImageParts(image)
				return ns, n, reference
			}
		}
		if len(parts) >= 2 {
			return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1], ""
		}
	case "helm":
		if name, version, ok := splitChartFilename(path); ok {
			return "", name, version
		}
	}
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-2], "/"), parts[len(parts)-2], parts[len(parts)-1]
	}
	return "", parts[0], ""
}

func detectContentType(format, path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".jar"):
		return "application/java-archive"
	case strings.HasSuffix(lower, ".pom"), strings.HasSuffix(lower, ".xml"):
		return "application/xml"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "application/yaml"
	case strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar.gz"):
		return "application/gzip"
	case strings.HasSuffix(lower, ".whl"):
		return "application/octet-stream"
	case strings.HasSuffix(lower, ".crate"):
		return "application/gzip"
	case strings.HasSuffix(lower, ".deb"):
		return "application/vnd.debian.binary-package"
	case strings.HasSuffix(lower, ".rpm"):
		return "application/x-rpm"
	case strings.HasSuffix(lower, ".apk"):
		return "application/vnd.alpine.apk"
	case strings.HasSuffix(lower, ".conda"):
		return "application/zip"
	case strings.HasSuffix(lower, ".tar.bz2"):
		return "application/x-bzip2"
	case strings.HasSuffix(lower, ".zip"):
		return "application/zip"
	}
	if contentType := mime.TypeByExtension("." + lower[strings.LastIndex(lower, ".")+1:]); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

func escapePath(path string) string {
	parts := strings.Split(strings.TrimLeft(path, "/"), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func isNotFoundError(err error) bool {
	var serviceErr *Error
	return errors.As(err, &serviceErr) && serviceErr.Status == http.StatusNotFound
}

func applyUpstreamAuth(headers http.Header, repository *store.Repository) {
	if headers.Get("Authorization") != "" || repository.Proxy == nil {
		return
	}
	if repository.Proxy.RemoteBearerToken != "" {
		headers.Set("Authorization", "Bearer "+repository.Proxy.RemoteBearerToken)
		return
	}
	if repository.Proxy.RemoteUsername != "" {
		request := &http.Request{Header: headers}
		request.SetBasicAuth(repository.Proxy.RemoteUsername, repository.Proxy.RemotePassword)
	}
}

func principalName(request *Request) string {
	if request == nil {
		return ""
	}
	if request.User != "" {
		return request.User
	}
	return request.Header.Get("X-Kkrepo-User")
}
