package service

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

// serveConda generates repodata/channeldata for hosted repositories and
// delegates archives and proxy/group traffic to the generic handler.
func (s *Service) serveConda(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" && (request.Method == http.MethodGet || request.Method == http.MethodHead) {
		switch {
		case strings.HasSuffix(path, "/repodata.json"), strings.HasSuffix(path, "/current_repodata.json"):
			prefix := strings.TrimSuffix(strings.TrimSuffix(path, "current_repodata.json"), "repodata.json")
			prefix = strings.TrimSuffix(prefix, "/")
			body := s.condaRepodata(repository, prefix)
			return bytesResponse(http.StatusOK, "application/json", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
		case path == "channeldata.json" || strings.HasSuffix(path, "/channeldata.json"):
			body := s.condaChannelData(repository)
			return bytesResponse(http.StatusOK, "application/json", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
		}
	}
	return s.serveGeneric(repository, request)
}

func (s *Service) condaRepodata(repository *store.Repository, prefix string) []byte {
	assets := s.store.ListAssets(repository.Name, "")
	subdir := prefix
	if index := strings.LastIndexByte(prefix, '/'); index >= 0 {
		subdir = prefix[index+1:]
	}
	packages := map[string]any{}
	condaPackages := map[string]any{}
	directory := prefix
	if directory != "" {
		directory += "/"
	}
	for _, asset := range assets {
		if directory != "" && !strings.HasPrefix(asset.Path, directory) {
			continue
		}
		name, version, build, ok := parseCondaFilename(asset.Path)
		if !ok {
			continue
		}
		entry := map[string]any{
			"name": name, "version": version, "build": build, "build_number": 0,
			"depends": []string{}, "sha256": asset.SHA256, "size": asset.Size, "subdir": subdir,
		}
		filename := asset.Path[strings.LastIndexByte(asset.Path, '/')+1:]
		if strings.HasSuffix(filename, ".conda") {
			condaPackages[filename] = entry
		} else {
			packages[filename] = entry
		}
	}
	document := map[string]any{
		"info":             map[string]any{"subdir": subdir},
		"packages":         packages,
		"packages.conda":   condaPackages,
		"repodata_version": 1,
	}
	body, _ := json.Marshal(document)
	return body
}

func (s *Service) condaChannelData(repository *store.Repository) []byte {
	assets := s.store.ListAssets(repository.Name, "")
	subdirs := map[string]struct{}{}
	packages := map[string]map[string]any{}
	for _, asset := range assets {
		name, version, _, ok := parseCondaFilename(asset.Path)
		if !ok {
			continue
		}
		dir := asset.Path
		if slash := strings.LastIndexByte(dir, '/'); slash >= 0 {
			dir = dir[:slash]
		} else {
			dir = ""
		}
		subdir := dir
		if slash := strings.LastIndexByte(dir, '/'); slash >= 0 {
			subdir = dir[slash+1:]
		}
		subdirs[subdir] = struct{}{}
		entry := packages[name]
		if entry == nil {
			entry = map[string]any{"name": name, "subdirs": []string{}, "version": version}
			packages[name] = entry
		}
	}
	subdirList := make([]string, 0, len(subdirs))
	for subdir := range subdirs {
		subdirList = append(subdirList, subdir)
	}
	sort.Strings(subdirList)
	for _, entry := range packages {
		entry["subdirs"] = subdirList
	}
	body, _ := json.Marshal(map[string]any{
		"channeldata_version": 1, "subdirs": subdirList, "packages": packages,
	})
	return body
}

func parseCondaFilename(path string) (string, string, string, bool) {
	filename := path[strings.LastIndexByte(path, '/')+1:]
	base := strings.TrimSuffix(strings.TrimSuffix(filename, ".conda"), ".tar.bz2")
	if base == filename {
		return "", "", "", false
	}
	last := strings.LastIndexByte(base, '-')
	if last <= 0 {
		return "", "", "", false
	}
	build := base[last+1:]
	rest := base[:last]
	second := strings.LastIndexByte(rest, '-')
	if second <= 0 {
		return "", "", "", false
	}
	return rest[:second], rest[second+1:], build, true
}
