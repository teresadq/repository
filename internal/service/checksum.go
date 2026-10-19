package service

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type checksumSpec struct {
	algorithm string
	suffix    string
	size      int
}

var checksumSpecs = []checksumSpec{
	{"md5", ".md5", 32},
	{"sha1", ".sha1", 40},
	{"sha256", ".sha256", 64},
}

func checksumRequest(path string) (checksumSpec, string, bool) {
	for _, spec := range checksumSpecs {
		if !strings.HasSuffix(path, spec.suffix) {
			continue
		}
		base := strings.TrimSuffix(path, spec.suffix)
		if base == "" || strings.HasSuffix(base, ".md5") ||
			strings.HasSuffix(base, ".sha1") || strings.HasSuffix(base, ".sha256") {
			return checksumSpec{}, "", false
		}
		return spec, base, true
	}
	return checksumSpec{}, "", false
}

func repositoryAllowsPath(repository *store.Repository, path string) bool {
	path = strings.Trim(path, "/")
	if len(repository.IncludePatterns) > 0 {
		matched := false
		for _, pattern := range repository.IncludePatterns {
			if ok, err := store.MatchGlob(pattern, path); err == nil && ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, pattern := range repository.ExcludePatterns {
		if ok, err := store.MatchGlob(pattern, path); err == nil && ok {
			return false
		}
	}
	return true
}

func (s *Service) serveChecksum(repository *store.Repository, request *Request, spec checksumSpec, base string) (*Response, error) {
	escapedBase := request.EscapedPath
	if strings.HasSuffix(escapedBase, spec.suffix) {
		escapedBase = strings.TrimSuffix(escapedBase, spec.suffix)
	} else {
		escapedBase = escapePath(base)
	}
	response, err := s.serveRepository(repository, &Request{
		Method: http.MethodGet, Path: base, EscapedPath: escapedBase,
		Query: request.Query, Header: request.Header, HeadOnly: false,
		BaseURL: request.BaseURL, RemoteAddr: request.RemoteAddr, User: request.User,
	})
	if err != nil {
		return nil, err
	}
	defer response.Close()
	body, err := response.ReadBounded(1 << 30)
	if err != nil {
		return nil, fmt.Errorf("read checksum source: %w", err)
	}
	var digest string
	switch spec.algorithm {
	case "md5":
		sum := md5.Sum(body)
		digest = hex.EncodeToString(sum[:])
	case "sha1":
		sum := sha1.Sum(body)
		digest = hex.EncodeToString(sum[:])
	default:
		sum := sha256.Sum256(body)
		digest = hex.EncodeToString(sum[:])
	}
	return bytesResponse(http.StatusOK, "text/plain", []byte(digest), "", time.Time{}, request.HeadOnly), nil
}
