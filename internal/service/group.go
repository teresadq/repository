package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/gomod"
	"kkrepo-go/internal/store"
)

func (s *Service) serveGroup(group *store.Repository, rawPath string, headOnly bool) (*Response, error) {
	path, err := parsePath(rawPath)
	if err != nil {
		return nil, err
	}
	members := s.groupMembers(group)
	if len(members) == 0 {
		return nil, NotFound(path.Path)
	}
	switch path.Kind {
	case gomod.KindList:
		return s.groupList(group, members, path, headOnly)
	case gomod.KindLatest:
		return s.groupLatest(members, path, headOnly)
	default:
		return s.groupFirst(members, path, headOnly)
	}
}

func (s *Service) groupMembers(group *store.Repository) []*store.Repository {
	names := group.MemberNames()
	members := make([]*store.Repository, 0, len(names))
	for _, name := range names {
		member, err := s.store.GetRepository(name)
		if err != nil || !member.Online || member.Format != "go" {
			continue
		}
		members = append(members, member)
	}
	return members
}

func (s *Service) groupList(group *store.Repository, members []*store.Repository, path gomod.GoPath, headOnly bool) (*Response, error) {
	collected := make([]string, 0)
	var lastUpstream error
	successful := false
	remaining := maxMetadataBytes
	for _, member := range members {
		var (
			versions []string
			used     int64
			err      error
		)
		switch member.Type {
		case "hosted":
			versions, err = s.hostedVersions(member, path.Module)
			used = serializedListBytes(versions)
		case "proxy":
			versions, used, err = s.proxyVersionList(member, path, remaining)
		case "group":
			return nil, MethodNotAllowed("Nested Go group repositories are not supported: " + member.Name)
		default:
			continue
		}
		if err != nil {
			var serviceErr *Error
			if errors.As(err, &serviceErr) && serviceErr.Status == http.StatusNotFound {
				continue
			}
			lastUpstream = err
			continue
		}
		remaining -= used
		if remaining < 0 {
			lastUpstream = BadUpstream("Go group metadata exceeds the aggregation limit")
			continue
		}
		successful = true
		collected = append(collected, versions...)
	}
	if !successful {
		if lastUpstream != nil {
			return nil, lastUpstream
		}
		return nil, NotFound(path.Path)
	}
	versions := gomod.ListVersions(collected)
	body := []byte(strings.Join(versions, "\n"))
	return bytesResponse(200, "text/plain", body, "", time.Time{}, headOnly), nil
}

func (s *Service) proxyVersionList(member *store.Repository, path gomod.GoPath, remaining int64) ([]string, int64, error) {
	response, err := s.serveProxy(member, path.Path, false)
	if err != nil {
		return nil, 0, err
	}
	defer response.Close()
	body, err := response.ReadBounded(remaining)
	if err != nil {
		return nil, 0, BadUpstream("Go group metadata exceeds the aggregation limit")
	}
	versions := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !gomod.IsCanonicalVersion(line) {
			return nil, 0, BadUpstream("Go group member returned an invalid version: " + line)
		}
		if !gomod.IsPseudoVersion(line) {
			versions = append(versions, line)
		}
	}
	return versions, int64(len(body)), nil
}

func serializedListBytes(versions []string) int64 {
	var bytes int64
	for index, version := range versions {
		if index > 0 {
			bytes++
		}
		bytes += int64(len(version))
	}
	return bytes
}

type latestCandidate struct {
	candidate    gomod.Candidate
	lastModified time.Time
}

func (s *Service) groupLatest(members []*store.Repository, path gomod.GoPath, headOnly bool) (*Response, error) {
	candidates := make([]latestCandidate, 0)
	var lastUpstream error
	remaining := maxMetadataBytes
	for _, member := range members {
		if remaining < 0 {
			lastUpstream = BadUpstream("Go group metadata exceeds the aggregation limit")
			break
		}
		response, err := s.memberGet(member, path.Path, false)
		if err != nil {
			var serviceErr *Error
			if errors.As(err, &serviceErr) && serviceErr.Status == http.StatusNotFound {
				continue
			}
			lastUpstream = err
			continue
		}
		body, readErr := response.ReadBounded(remaining)
		response.Close()
		if readErr != nil {
			lastUpstream = BadUpstream("Go group metadata exceeds the aggregation limit")
			continue
		}
		remaining -= int64(len(body))
		version, at, parseErr := parseInfo(body)
		if parseErr != nil || !gomod.IsCanonicalVersion(version) {
			lastUpstream = BadUpstream("Go group member returned invalid @latest metadata")
			continue
		}
		lastModified, _ := http.ParseTime(response.Header.Get("Last-Modified"))
		candidates = append(candidates, latestCandidate{
			candidate:    gomod.Candidate{Version: version, Time: at},
			lastModified: lastModified,
		})
	}
	versions := make([]gomod.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		versions = append(versions, candidate.candidate)
	}
	selected, ok := gomod.Latest(versions)
	if !ok {
		if lastUpstream != nil {
			return nil, lastUpstream
		}
		return nil, NotFound(path.Path)
	}
	var selectedLastModified time.Time
	for _, candidate := range candidates {
		if candidate.candidate.Version == selected.Version {
			selectedLastModified = candidate.lastModified
			break
		}
	}
	body, err := infoBytes(selected.Version, selected.Time)
	if err != nil {
		return nil, fmt.Errorf("encode @latest info: %w", err)
	}
	return bytesResponse(200, "text/plain", body, sha256Hex(body), selectedLastModified, headOnly), nil
}

func (s *Service) groupFirst(members []*store.Repository, path gomod.GoPath, headOnly bool) (*Response, error) {
	var lastUpstream error
	for _, member := range members {
		response, err := s.memberGet(member, path.Path, headOnly)
		if err != nil {
			var serviceErr *Error
			if errors.As(err, &serviceErr) && serviceErr.Status == http.StatusNotFound {
				continue
			}
			lastUpstream = err
			continue
		}
		return response, nil
	}
	if lastUpstream != nil {
		return nil, lastUpstream
	}
	return nil, NotFound(path.Path)
}

func (s *Service) memberGet(member *store.Repository, rawPath string, headOnly bool) (*Response, error) {
	switch member.Type {
	case "hosted":
		return s.serveHosted(member, rawPath, headOnly)
	case "proxy":
		return s.serveProxy(member, rawPath, headOnly)
	case "group":
		return nil, MethodNotAllowed("Nested Go group repositories are not supported: " + member.Name)
	default:
		return nil, MethodNotAllowed("Unsupported Go member repository type: " + member.Type)
	}
}
