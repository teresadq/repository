package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type browseEntry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Type        string    `json:"type"`
	Size        int64     `json:"size,omitempty"`
	Count       int       `json:"count,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	ContentType string    `json:"contentType,omitempty"`
	Source      string    `json:"source,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
	URL         string    `json:"url,omitempty"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	repository, err := s.store.GetRepository(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Repository not found")
		return
	}
	principal := PrincipalFrom(r)
	if !principal.IsAdmin() && !principal.CanBrowse(repository.Format, repository.Name) {
		writeError(w, http.StatusForbidden, "Browse privilege is required")
		return
	}

	prefix := strings.Trim(r.URL.Query().Get("path"), "/")
	assets := s.browseAssets(repository)
	entries := buildBrowseEntries(assets, prefix, repository.Name)

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    prefix,
		"entries": entries,
		"total":   len(entries),
	})
}

// browseAssets returns unique assets visible through a repository. Group
// repositories aggregate their hosted/proxy members.
func (s *Server) browseAssets(repository *store.Repository) []*store.Asset {
	repositories := []*store.Repository{repository}
	if repository.Type == "group" {
		repositories = repositories[:0]
		for _, name := range repository.MemberNames() {
			member, err := s.store.GetRepository(name)
			if err != nil || !member.Online || member.Format != repository.Format {
				continue
			}
			repositories = append(repositories, member)
		}
	}
	seen := map[string]struct{}{}
	assets := make([]*store.Asset, 0)
	for _, repo := range repositories {
		for _, asset := range s.store.ListAssets(repo.Name, "") {
			if _, ok := seen[asset.Path]; ok {
				continue
			}
			seen[asset.Path] = struct{}{}
			assets = append(assets, asset)
		}
	}
	return assets
}

func buildBrowseEntries(assets []*store.Asset, prefix, repositoryName string) []browseEntry {
	directories := map[string]*browseEntry{}
	files := make([]browseEntry, 0)
	for _, asset := range assets {
		path := strings.Trim(asset.Path, "/")
		if path == "" {
			continue
		}
		if prefix != "" {
			if !strings.HasPrefix(path, prefix+"/") {
				continue
			}
			path = strings.TrimPrefix(path, prefix+"/")
			if path == "" {
				continue
			}
		}
		if index := strings.IndexByte(path, '/'); index >= 0 {
			name := path[:index]
			directory, ok := directories[name]
			if !ok {
				directory = &browseEntry{
					Name: name, Path: joinBrowsePath(prefix, name), Type: "directory",
				}
				directories[name] = directory
			}
			directory.Count++
			directory.Size += asset.Size
			if asset.UpdatedAt.After(directory.UpdatedAt) {
				directory.UpdatedAt = asset.UpdatedAt
			}
			continue
		}
		files = append(files, browseEntry{
			Name:        path,
			Path:        joinBrowsePath(prefix, path),
			Type:        "file",
			Size:        asset.Size,
			Kind:        asset.Kind,
			ContentType: asset.ContentType,
			Source:      asset.Source,
			UpdatedAt:   asset.UpdatedAt,
			URL:         "/repository/" + repositoryName + "/" + asset.Path,
		})
	}
	entries := make([]browseEntry, 0, len(directories)+len(files))
	for _, directory := range directories {
		entries = append(entries, *directory)
	}
	entries = append(entries, files...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries
}

func joinBrowsePath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}
