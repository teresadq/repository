package api

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

type searchItem struct {
	Type       string    `json:"type"`
	Repository string    `json:"repository"`
	Format     string    `json:"format"`
	Name       string    `json:"name"`
	Version    string    `json:"version,omitempty"`
	Path       string    `json:"path,omitempty"`
	Size       int64     `json:"size,omitempty"`
	SHA256     string    `json:"sha256,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
	URL        string    `json:"url,omitempty"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	formatFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	repositoryFilter := strings.TrimSpace(r.URL.Query().Get("repository"))
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if kind == "" {
		kind = "assets"
	}
	checksum := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("checksum")))
	if checksum == "" && len(query) >= 6 && isHexQuery(query) {
		checksum = query
	}
	items := make([]searchItem, 0)
	for _, repository := range s.store.ListRepositories() {
		if repositoryFilter != "" && repository.Name != repositoryFilter {
			continue
		}
		if formatFilter != "" && repository.Format != formatFilter {
			continue
		}
		if !principal.IsAdmin() && !principal.CanBrowse(repository.Format, repository.Name) {
			continue
		}
		if kind == "components" {
			for _, component := range s.store.ListComponents(repository.Name, "", query) {
				var size int64
				var assetURL string
				var matchesChecksum bool
				for _, asset := range s.store.AssetsForComponent(repository.Name, component.Module, component.Version) {
					size += asset.Size
					if assetURL == "" {
						assetURL = "/repository/" + repository.Name + "/" + asset.Path
					}
					if checksum == "" || strings.HasPrefix(strings.ToLower(asset.SHA256), checksum) {
						matchesChecksum = true
					}
				}
				if checksum != "" && !matchesChecksum {
					continue
				}
				items = append(items, searchItem{
					Type: "component", Repository: repository.Name, Format: repository.Format,
					Name: component.Module, Version: component.Version, Size: size,
					UpdatedAt: component.UpdatedAt, URL: assetURL,
				})
			}
			continue
		}
		for _, asset := range s.store.ListAssets(repository.Name, query) {
			if checksum != "" && !strings.HasPrefix(strings.ToLower(asset.SHA256), checksum) {
				continue
			}
			items = append(items, searchItem{
				Type: "asset", Repository: repository.Name, Format: repository.Format,
				Name: asset.Module, Version: asset.Version, Path: asset.Path, Size: asset.Size,
				SHA256: asset.SHA256, UpdatedAt: asset.UpdatedAt,
				URL: "/repository/" + repository.Name + "/" + asset.Path,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Name < items[j].Name
	})
	offset, limit := parsePage(r, 100, 500)
	total := len(items)
	if offset >= len(items) {
		items = nil
	} else {
		end := offset + limit
		if end > len(items) {
			end = len(items)
		}
		items = items[offset:end]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "offset": offset, "limit": limit,
	})
}

func isHexQuery(value string) bool {
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}
