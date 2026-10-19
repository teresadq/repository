package service

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type cargoIndexEntry struct {
	Name     string              `json:"name"`
	Vers     string              `json:"vers"`
	Deps     []json.RawMessage   `json:"deps"`
	Cksum    string              `json:"cksum"`
	Features map[string][]string `json:"features"`
	Yanked   bool                `json:"yanked"`
}

type cargoPublish struct {
	Name          string              `json:"name"`
	Vers          string              `json:"vers"`
	Deps          []json.RawMessage   `json:"deps"`
	Features      map[string][]string `json:"features"`
	Description   string              `json:"description"`
	License       string              `json:"license"`
	Repository    string              `json:"repository"`
	Homepage      string              `json:"homepage"`
	Documentation string              `json:"documentation"`
}

func (s *Service) serveCargo(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" {
		switch {
		case (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
			(path == "config.json" || path == "index/config.json"):
			config, _ := jsonBytes(map[string]any{
				"dl":            request.BaseURL + "/repository/" + repository.Name + "/api/v1/crates",
				"api":           request.BaseURL + "/repository/" + repository.Name,
				"auth-required": false,
			})
			return bytesResponse(http.StatusOK, "application/json", config, sha256Hex(config), time.Time{}, request.HeadOnly), nil
		case request.Method == http.MethodPut && (path == "api/v1/crates/new" || path == "api/v1/crates/new/"):
			return s.cargoPublish(repository, request)
		case request.Method == http.MethodPut || request.Method == http.MethodDelete:
			if name, version, ok := parseCargoYankPath(path); ok {
				yank := request.Method == http.MethodDelete
				if err := s.cargoYank(repository, name, version, yank); err != nil {
					return nil, err
				}
				body, _ := jsonBytes(map[string]any{"ok": true})
				return bytesResponse(http.StatusOK, "application/json", body, "", time.Time{}, false), nil
			}
		case request.Method == http.MethodGet || request.Method == http.MethodHead:
			if name, version, ok := parseCargoDownloadPath(path); ok {
				return s.serveStoredFile(repository, cargoCratePath(name, version), request.HeadOnly, "application/gzip")
			}
			if path == "api/v1/crates" {
				return s.cargoSearch(repository, request)
			}
			if indexPath, ok := cargoIndexAssetPath(path); ok {
				return s.serveStoredFile(repository, indexPath, request.HeadOnly, "text/plain")
			}
		}
	}
	return s.serveGeneric(repository, request)
}

func (s *Service) cargoPublish(repository *store.Repository, request *Request) (*Response, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, 512<<20))
	if err != nil {
		return nil, BadRequest("Unable to read Cargo publish body")
	}
	if len(body) < 8 {
		return nil, BadRequest("Invalid Cargo publish body")
	}
	jsonLength := int(binary.LittleEndian.Uint32(body[0:4]))
	if jsonLength < 0 || 4+jsonLength+4 > len(body) {
		return nil, BadRequest("Invalid Cargo publish metadata length")
	}
	var publish cargoPublish
	if err := json.Unmarshal(body[4:4+jsonLength], &publish); err != nil || publish.Name == "" || publish.Vers == "" {
		return nil, BadRequest("Invalid Cargo publish metadata")
	}
	crateLength := int(binary.LittleEndian.Uint32(body[4+jsonLength : 8+jsonLength]))
	crateStart := 8 + jsonLength
	if crateStart+crateLength > len(body) {
		return nil, BadRequest("Invalid Cargo crate length")
	}
	crate := body[crateStart : crateStart+crateLength]
	if len(crate) == 0 {
		return nil, BadRequest("Cargo crate is empty")
	}
	sum, size, _, err := s.store.SaveBlobBytes(crate)
	if err != nil {
		return nil, err
	}
	now := s.now()
	path := cargoCratePath(publish.Name, publish.Vers)
	crateAsset := &store.Asset{
		Repository: repository.Name, Format: "cargo", Path: path, Module: publish.Name, Version: publish.Vers,
		Kind: "crate", ContentType: "application/gzip", Size: size, SHA256: sum, Source: "hosted",
		CreatedAt: now, UpdatedAt: now,
	}
	component := &store.Component{
		Repository: repository.Name, Format: "cargo", Module: publish.Name, Version: publish.Vers,
		Kind: "cargo-crate", Time: now, CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]string{"cksum": sum, "description": publish.Description, "license": publish.License},
	}
	entry := cargoIndexEntry{
		Name: publish.Name, Vers: publish.Vers, Deps: publish.Deps, Cksum: sum,
		Features: publish.Features, Yanked: false,
	}
	if err := s.store.PutAsset(crateAsset, component, true); err != nil {
		return nil, err
	}
	if err := s.writeCargoIndex(repository, publish.Name, entry); err != nil {
		return nil, err
	}
	response, _ := jsonBytes(map[string]any{"ok": true})
	return bytesResponse(http.StatusOK, "application/json", response, "", time.Time{}, false), nil
}

func (s *Service) writeCargoIndex(repository *store.Repository, name string, entry cargoIndexEntry) error {
	indexPath := "index/" + cargoIndexPrefix(name)
	entries := s.cargoIndexEntries(repository, name)
	replaced := false
	for index := range entries {
		if entries[index].Vers == entry.Vers {
			entries[index] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Vers < entries[j].Vers })
	var buffer bytes.Buffer
	for _, item := range entries {
		line, _ := json.Marshal(item)
		buffer.Write(line)
		buffer.WriteByte('\n')
	}
	sum, size, _, err := s.store.SaveBlobBytes(buffer.Bytes())
	if err != nil {
		return err
	}
	now := s.now()
	asset := &store.Asset{
		Repository: repository.Name, Format: "cargo", Path: indexPath, Module: name,
		Kind: "index", ContentType: "text/plain", Size: size, SHA256: sum, Source: "hosted",
		CreatedAt: now, UpdatedAt: now,
	}
	return s.store.PutAsset(asset, nil, true)
}

func (s *Service) cargoIndexEntries(repository *store.Repository, name string) []cargoIndexEntry {
	asset, ok := s.store.GetAsset(repository.Name, "index/"+cargoIndexPrefix(name))
	if !ok {
		return nil
	}
	blob, err := s.store.ReadBlob(asset)
	if err != nil {
		return nil
	}
	entries := make([]cargoIndexEntry, 0)
	for _, line := range strings.Split(string(blob), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry cargoIndexEntry
		if json.Unmarshal([]byte(line), &entry) == nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (s *Service) cargoYank(repository *store.Repository, name, version string, yanked bool) error {
	entries := s.cargoIndexEntries(repository, name)
	found := false
	for index := range entries {
		if entries[index].Vers == version {
			entries[index].Yanked = yanked
			found = true
		}
	}
	if !found {
		return NotFound(name + " " + version)
	}
	var buffer bytes.Buffer
	for _, entry := range entries {
		line, _ := json.Marshal(entry)
		buffer.Write(line)
		buffer.WriteByte('\n')
	}
	sum, size, _, err := s.store.SaveBlobBytes(buffer.Bytes())
	if err != nil {
		return err
	}
	now := s.now()
	return s.store.PutAsset(&store.Asset{
		Repository: repository.Name, Format: "cargo", Path: "index/" + cargoIndexPrefix(name), Module: name,
		Kind: "index", ContentType: "text/plain", Size: size, SHA256: sum, Source: "hosted",
		CreatedAt: now, UpdatedAt: now,
	}, nil, true)
}

func (s *Service) cargoSearch(repository *store.Repository, request *Request) (*Response, error) {
	query := strings.ToLower(strings.TrimSpace(request.Query.Get("q")))
	components := s.store.ListComponents(repository.Name, "", query)
	crates := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, component := range components {
		if _, ok := seen[component.Module]; ok {
			continue
		}
		seen[component.Module] = struct{}{}
		crates = append(crates, map[string]any{
			"name":        component.Module,
			"max_version": component.Version,
			"description": component.Attributes["description"],
			"created_at":  component.CreatedAt,
			"updated_at":  component.UpdatedAt,
		})
	}
	body, _ := jsonBytes(map[string]any{"crates": crates, "meta": map[string]any{"total": len(crates)}})
	return bytesResponse(http.StatusOK, "application/json", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
}

func cargoCratePath(name, version string) string {
	return fmt.Sprintf("crates/%s/%s-%s.crate", strings.ToLower(name), name, version)
}

func cargoIndexPrefix(name string) string {
	lower := strings.ToLower(name)
	switch len(lower) {
	case 1:
		return "1/" + lower
	case 2:
		return "2/" + lower
	case 3:
		return "3/" + lower[:1] + "/" + lower
	default:
		return lower[:2] + "/" + lower[2:4] + "/" + lower
	}
}

func parseCargoDownloadPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for index := 0; index+5 < len(parts); index++ {
		if parts[index] == "api" && parts[index+1] == "v1" && parts[index+2] == "crates" && parts[index+5] == "download" {
			return parts[index+3], parts[index+4], true
		}
	}
	if len(parts) >= 4 && parts[0] == "crates" && parts[3] == "download" {
		return parts[1], parts[2], true
	}
	return "", "", false
}

func parseCargoYankPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 6 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "crates" {
		return "", "", false
	}
	action := parts[5]
	if action != "yank" && action != "unyank" {
		return "", "", false
	}
	return parts[3], parts[4], true
}

func cargoIndexAssetPath(path string) (string, bool) {
	path = strings.Trim(path, "/")
	if strings.HasPrefix(path, "index/") {
		return path, true
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", false
	}
	if parts[0] == "1" || parts[0] == "2" || parts[0] == "3" ||
		(len(parts[0]) == 2 && len(parts[1]) == 2) {
		return "index/" + path, true
	}
	return "", false
}
