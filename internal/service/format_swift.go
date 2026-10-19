package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

func (s *Service) serveSwift(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" {
		switch {
		case request.Method == http.MethodPut && isSwiftReleasePath(path):
			return s.swiftPublish(repository, request, path)
		case (request.Method == http.MethodGet || request.Method == http.MethodHead) && path == "identifiers":
			return s.swiftIdentifiers(repository, request)
		case (request.Method == http.MethodGet || request.Method == http.MethodHead):
			if scope, name, version, ok := parseSwiftManifestPath(path); ok {
				return withContentVersion(s.swiftManifest(repository, request, scope, name, version))
			}
			if scope, name, version, ok := parseSwiftArchivePath(path); ok {
				return withContentVersion(s.serveStoredFile(repository, swiftArchivePath(scope, name, version), request.HeadOnly, "application/zip"))
			}
			if scope, name, version, ok := parseSwiftReleaseMetadataPath(path); ok {
				return s.swiftMetadata(repository, request, scope, name, version)
			}
			if scope, name, ok := parseSwiftReleaseListPath(path); ok {
				return s.swiftReleases(repository, request, scope, name)
			}
		}
	}
	return s.serveGeneric(repository, request)
}

type swiftMetadataDocument struct {
	Metadata map[string]any `json:"metadata"`
}

func (s *Service) swiftPublish(repository *store.Repository, request *Request, path string) (*Response, error) {
	scope, name, version, _ := parseSwiftReleaseMetadataPath(path)
	reader := multipart.NewReader(io.LimitReader(request.Body, 512<<20), multipartBoundary(request.Header))
	form, err := reader.ReadForm(32 << 20)
	if err != nil {
		return nil, BadRequest("Invalid Swift release upload")
	}
	defer form.RemoveAll()
	var archive multipart.File
	for _, key := range []string{"source-archive", "source_archive", "file"} {
		if files := form.File[key]; len(files) > 0 {
			archive, err = files[0].Open()
			if err != nil {
				return nil, BadRequest("Unable to read Swift source archive")
			}
			break
		}
	}
	if archive == nil {
		return nil, BadRequest("Swift release requires a source-archive field")
	}
	defer archive.Close()
	sum, size, _, err := s.store.SaveBlob(archive, maxGenericUploadBytes)
	if err != nil {
		return nil, err
	}
	metadata := map[string]any{}
	if raw := formValue(form, "metadata"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &metadata)
	}
	if len(form.File["metadata"]) > 0 {
		if file, err := form.File["metadata"][0].Open(); err == nil {
			raw, _ := io.ReadAll(io.LimitReader(file, 1<<20))
			file.Close()
			var document swiftMetadataDocument
			if json.Unmarshal(raw, &document) == nil && document.Metadata != nil {
				metadata = document.Metadata
			} else {
				_ = json.Unmarshal(raw, &metadata)
			}
		}
	}
	now := s.now()
	archivePath := swiftArchivePath(scope, name, version)
	archiveAsset := &store.Asset{
		Repository: repository.Name, Format: "swift", Namespace: scope, Path: archivePath,
		Module: name, Version: version, Kind: "source-archive", ContentType: "application/zip",
		Size: size, SHA256: sum, Source: "hosted", CreatedAt: now, UpdatedAt: now,
	}
	metadataBody, _ := json.Marshal(map[string]any{
		"id":      scope + "." + name,
		"version": version,
		"resources": []map[string]any{{
			"name": "source-archive", "type": "application/zip",
			"checksum": sum,
			"url":      request.BaseURL + "/repository/" + repository.Name + "/" + archivePath,
		}},
		"metadata":    metadata,
		"publishedAt": now.Format(time.RFC3339),
	})
	metadataSum, metadataSize, _, err := s.store.SaveBlobBytes(metadataBody)
	if err != nil {
		return nil, err
	}
	metadataAsset := &store.Asset{
		Repository: repository.Name, Format: "swift", Namespace: scope,
		Path: scope + "/" + name + "/" + version, Module: name, Version: version,
		Kind: "release", ContentType: "application/vnd.swift.registry.v1+json",
		Size: metadataSize, SHA256: metadataSum, Source: "hosted", CreatedAt: now, UpdatedAt: now,
	}
	component := &store.Component{
		Repository: repository.Name, Format: "swift", Namespace: scope,
		Module: name, Version: version, Kind: "swift-package", Time: now, CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]string{"archive_sha256": sum},
	}
	if err := s.store.PutRelease([]*store.Asset{archiveAsset, metadataAsset}, []*store.Component{component}, true); err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Content-Version", "1")
	header.Set("Location", request.BaseURL+"/repository/"+repository.Name+"/"+scope+"/"+name+"/"+version)
	return &Response{Status: http.StatusCreated, Header: header}, nil
}

func (s *Service) swiftReleases(repository *store.Repository, request *Request, scope, name string) (*Response, error) {
	components := s.store.ListComponents(repository.Name, name, "")
	releases := map[string]map[string]string{}
	for _, component := range components {
		if component.Namespace != "" && component.Namespace != scope {
			continue
		}
		releases[component.Version] = map[string]string{
			"url": request.BaseURL + "/repository/" + repository.Name + "/" + scope + "/" + name + "/" + component.Version,
		}
	}
	if len(releases) == 0 {
		return nil, NotFound(scope + "/" + name)
	}
	body, _ := jsonBytes(map[string]any{"releases": releases})
	return swiftJSONResponse(body, request.HeadOnly)
}

func (s *Service) swiftMetadata(repository *store.Repository, request *Request, scope, name, version string) (*Response, error) {
	path := scope + "/" + name + "/" + version
	if asset, ok := s.store.GetAsset(repository.Name, path); ok {
		body, err := s.store.ReadBlob(asset)
		if err == nil {
			return swiftJSONResponse(body, request.HeadOnly)
		}
	}
	component, ok := s.store.GetComponent(repository.Name, name, version)
	if !ok {
		return nil, NotFound(path)
	}
	archivePath := swiftArchivePath(scope, name, version)
	archiveSHA := ""
	if archive, ok := s.store.GetAsset(repository.Name, archivePath); ok {
		archiveSHA = archive.SHA256
	}
	body, _ := jsonBytes(map[string]any{
		"id":      scope + "." + name,
		"version": version,
		"resources": []map[string]any{{
			"name": "source-archive", "type": "application/zip",
			"checksum": archiveSHA,
			"url":      request.BaseURL + "/repository/" + repository.Name + "/" + archivePath,
		}},
		"metadata":    map[string]any{},
		"publishedAt": component.Time.UTC().Format(time.RFC3339),
	})
	return swiftJSONResponse(body, request.HeadOnly)
}

func (s *Service) swiftManifest(repository *store.Repository, request *Request, scope, name, version string) (*Response, error) {
	asset, ok := s.store.GetAsset(repository.Name, swiftArchivePath(scope, name, version))
	if !ok {
		return nil, NotFound(scope + "/" + name + "/" + version)
	}
	blob, err := s.store.ReadBlob(asset)
	if err != nil {
		return nil, NotFound(asset.Path)
	}
	reader, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return nil, BadRequest("Source archive is not a valid zip")
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.HasSuffix(file.Name, "Package.swift") {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			continue
		}
		content, _ := io.ReadAll(io.LimitReader(handle, 4<<20))
		handle.Close()
		return bytesResponse(http.StatusOK, "text/x-swift", content, sha256Hex(content), time.Time{}, request.HeadOnly), nil
	}
	return nil, NotFound("Package.swift")
}

func (s *Service) swiftIdentifiers(repository *store.Repository, request *Request) (*Response, error) {
	components := s.store.ListComponents(repository.Name, "", "")
	seen := map[string]struct{}{}
	identifiers := make([]string, 0)
	for _, component := range components {
		if _, ok := seen[component.Module]; ok {
			continue
		}
		seen[component.Module] = struct{}{}
		identifiers = append(identifiers, component.Module)
	}
	body, _ := jsonBytes(map[string]any{"identifiers": identifiers})
	return swiftJSONResponse(body, request.HeadOnly)
}

func swiftJSONResponse(body []byte, headOnly bool) (*Response, error) {
	return withContentVersion(bytesResponse(http.StatusOK, "application/json", body, sha256Hex(body), time.Time{}, headOnly), nil)
}

func withContentVersion(response *Response, err error) (*Response, error) {
	if response != nil {
		response.Header.Set("Content-Version", "1")
	}
	return response, err
}

func swiftArchivePath(scope, name, version string) string {
	return scope + "/" + name + "/" + version + ".zip"
}

func isSwiftReleasePath(path string) bool {
	_, _, _, ok := parseSwiftReleaseMetadataPath(path)
	return ok
}

func parseSwiftReleaseListPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && !strings.Contains(parts[1], ".") {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func parseSwiftReleaseMetadataPath(path string) (string, string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 3 && !strings.HasSuffix(parts[2], ".zip") && parts[2] != "Package.swift" {
		return parts[0], parts[1], parts[2], true
	}
	return "", "", "", false
}

func parseSwiftArchivePath(path string) (string, string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 3 && strings.HasSuffix(parts[2], ".zip") {
		return parts[0], parts[1], strings.TrimSuffix(parts[2], ".zip"), true
	}
	return "", "", "", false
}

func parseSwiftManifestPath(path string) (string, string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 4 && parts[3] == "Package.swift" {
		return parts[0], parts[1], parts[2], true
	}
	return "", "", "", false
}
