package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"kkrepo-go/internal/gomod"
	"kkrepo-go/internal/store"
)

const maxMetadataBytes int64 = 32 << 20

var upstreamBackoff = []time.Duration{
	30 * time.Second,
	time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
}

func (s *Service) serveProxy(repository *store.Repository, rawPath string, headOnly bool) (*Response, error) {
	path, err := parsePath(rawPath)
	if err != nil {
		return nil, err
	}
	cached, hasCached := s.store.GetAsset(repository.Name, path.Path)
	now := s.now()
	ttl := repository.ContentMaxAge()
	if path.IsMetadata() {
		ttl = repository.MetadataMaxAge()
	}
	if hasCached && isFresh(cached, ttl, now) {
		return s.serveCachedProxy(cached, path, headOnly)
	}
	if s.store.IsNotFoundCached(repository.Name, path.Path) {
		return nil, NotFound(path.Path)
	}
	if s.store.IsProxyBlocked(repository.Name, now) {
		if hasCached {
			return s.serveCachedProxy(cached, path, headOnly)
		}
		return nil, BadUpstream("Upstream temporarily blocked: " + remoteURL(repository))
	}
	return s.fetchAndCache(repository, path, cached, hasCached, headOnly, now)
}

func (s *Service) fetchAndCache(repository *store.Repository, path gomod.GoPath, cached *store.Asset, hasCached, headOnly bool, now time.Time) (*Response, error) {
	url := buildRemoteURL(remoteURL(repository), path.Path)
	headers := http.Header{}
	applyUpstreamAuth(headers, repository)
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
	var (
		result *upstreamResult
		err    error
	)
	if path.Kind == gomod.KindPackage {
		result, err = s.fetchFileWithHeaders(url, etag, lastModified, headers)
	} else {
		result, err = s.fetchBytesWithHeaders(url, etag, lastModified, headers)
	}
	if err != nil {
		return s.handleUpstreamFailure(repository, path, cached, hasCached, headOnly,
			"Upstream IO error: "+err.Error(), now)
	}
	defer result.cleanup()
	switch {
	case result.status == 304 && hasCached:
		s.store.TouchAsset(repository.Name, path.Path, now)
		s.store.RecordProxySuccess(repository.Name, now)
		s.store.InvalidateNotFound(repository.Name, path.Path)
		return s.serveCachedProxy(cached, path, headOnly)
	case result.status >= 200 && result.status < 300:
		s.store.InvalidateNotFound(repository.Name, path.Path)
		return s.persistAndServe(repository, path, result, headOnly, now)
	case result.status == 404 || result.status == 410:
		s.store.RecordProxySuccess(repository.Name, now)
		if hasCached {
			return s.serveCachedProxy(cached, path, headOnly)
		}
		if result.status == 404 {
			ttl := time.Duration(repository.MetadataMaxAge()) * time.Minute
			if ttl <= 0 {
				ttl = 24 * time.Hour
			}
			s.store.RememberNotFound(repository.Name, path.Path, ttl)
		}
		return nil, NotFound(path.Path)
	default:
		return s.handleUpstreamFailure(repository, path, cached, hasCached, headOnly,
			fmt.Sprintf("Upstream returned %d", result.status), now)
	}
}

func (s *Service) persistAndServe(repository *store.Repository, path gomod.GoPath, result *upstreamResult, headOnly bool, now time.Time) (*Response, error) {
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
	asset := &store.Asset{
		Repository:   repository.Name,
		Path:         path.Path,
		Module:       path.Module,
		Version:      path.Version,
		Kind:         path.Kind.String(),
		ContentType:  path.ProxyContentType(),
		Size:         size,
		SHA256:       sum,
		Source:       "proxy",
		ETag:         result.etag,
		LastModified: lastModified,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	var component *store.Component
	if path.HasComponent() {
		component = &store.Component{
			Repository: repository.Name,
			Module:     path.Module,
			Version:    path.Version,
			Kind:       "go-module",
			Time:       now,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}
	if err := s.store.PutAsset(asset, component, true); err != nil {
		return nil, fmt.Errorf("persist proxy asset: %w", err)
	}
	s.store.RecordProxySuccess(repository.Name, now)
	return s.serveCachedProxy(asset, path, headOnly)
}

func (s *Service) serveCachedProxy(asset *store.Asset, path gomod.GoPath, headOnly bool) (*Response, error) {
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(path.Path)
	}
	lastModified := asset.UpdatedAt
	if asset.LastModified != "" {
		if parsed, err := time.Parse(time.RFC3339, asset.LastModified); err == nil {
			lastModified = parsed
		}
	}
	return streamResponse(200, path.ProxyContentType(), body, asset.Size, asset.ETag, lastModified, headOnly), nil
}

func (s *Service) handleUpstreamFailure(repository *store.Repository, path gomod.GoPath, cached *store.Asset, hasCached, headOnly bool, message string, now time.Time) (*Response, error) {
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
		return s.serveCachedProxy(cached, path, headOnly)
	}
	return nil, BadUpstream(message)
}

func isFresh(asset *store.Asset, ttlMinutes int, now time.Time) bool {
	if asset == nil || asset.UpdatedAt.IsZero() {
		return false
	}
	if ttlMinutes < 0 {
		return true
	}
	return asset.UpdatedAt.Add(time.Duration(ttlMinutes) * time.Minute).After(now)
}

func remoteURL(repository *store.Repository) string {
	if repository.Proxy == nil {
		return ""
	}
	return repository.Proxy.RemoteURL
}

func buildRemoteURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

type upstreamResult struct {
	status       int
	etag         string
	lastModified time.Time
	contentType  string
	body         []byte
	tempFile     string
}

func (r *upstreamResult) cleanup() {
	if r != nil && r.tempFile != "" {
		_ = os.Remove(r.tempFile)
	}
}

func (s *Service) fetchBytes(url, etag string, lastModified time.Time) (*upstreamResult, error) {
	result, err := s.fetcher.FetchBytes(context.Background(), url, etag, lastModified, maxMetadataBytes)
	if err != nil {
		return nil, err
	}
	return &upstreamResult{
		status:       result.Status,
		etag:         result.ETag,
		lastModified: result.LastModified,
		body:         result.Body,
	}, nil
}

func (s *Service) fetchFile(url, etag string, lastModified time.Time) (*upstreamResult, error) {
	result, err := s.fetcher.FetchFile(context.Background(), url, etag, lastModified, gomod.MaxCompressedBytes, s.store.TempDir())
	if err != nil {
		return nil, err
	}
	return &upstreamResult{
		status:       result.Status,
		etag:         result.ETag,
		lastModified: result.LastModified,
		tempFile:     result.TempFile,
	}, nil
}
