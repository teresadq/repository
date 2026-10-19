package service

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type pubUpload struct {
	name    string
	version string
	archive string
}

func (s *Service) servePub(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" {
		switch {
		case request.Method == http.MethodGet && path == "api/packages/versions/new":
			id := store.RandomID(16)
			s.sessionMu.Lock()
			s.pubSessions[id] = &pubUpload{}
			s.sessionMu.Unlock()
			body, _ := jsonBytes(map[string]any{
				"url":    request.BaseURL + "/repository/" + repository.Name + "/api/packages/versions/upload/" + id,
				"fields": map[string]string{},
			})
			return bytesResponse(http.StatusOK, "application/vnd.pub.v2+json", body, "", time.Time{}, false), nil
		case request.Method == http.MethodPost && strings.HasPrefix(path, "api/packages/versions/upload/"):
			id := strings.TrimPrefix(path, "api/packages/versions/upload/")
			return s.pubUploadPackage(repository, request, id)
		case request.Method == http.MethodGet && strings.HasPrefix(path, "api/packages/versions/finalize/"):
			body, _ := jsonBytes(map[string]any{"success": map[string]string{"message": "Successfully uploaded package."}})
			return bytesResponse(http.StatusOK, "application/vnd.pub.v2+json", body, "", time.Time{}, request.HeadOnly), nil
		case request.Method == http.MethodGet && path == "api/package-names":
			return s.pubPackageNames(repository, request, "packages")
		case request.Method == http.MethodGet && path == "api/package-name-completion-data":
			return s.pubPackageNames(repository, request, "packages")
		case request.Method == http.MethodGet && strings.HasPrefix(path, "api/packages/"):
			rest := strings.TrimPrefix(path, "api/packages/")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) >= 1 && parts[0] != "" {
				if len(parts) >= 3 && parts[1] == "versions" {
					return s.pubVersionMetadata(repository, request, parts[0], parts[2])
				}
				return s.pubPackageMetadata(repository, request, parts[0])
			}
		case request.Method == http.MethodGet || request.Method == http.MethodHead:
			if name, version, ok := parsePubArchivePath(path); ok {
				return s.serveStoredFile(repository, pubArchivePath(name, version), request.HeadOnly, "application/gzip")
			}
		}
	}
	return s.serveGeneric(repository, request)
}

func (s *Service) pubUploadPackage(repository *store.Repository, request *Request, id string) (*Response, error) {
	reader := multipart.NewReader(io.LimitReader(request.Body, 512<<20), multipartBoundary(request.Header))
	form, err := reader.ReadForm(32 << 20)
	if err != nil {
		return nil, BadRequest("Invalid Pub upload")
	}
	defer form.RemoveAll()
	var file multipart.File
	for _, key := range []string{"file", "content"} {
		if files := form.File[key]; len(files) > 0 {
			file, err = files[0].Open()
			if err != nil {
				return nil, BadRequest("Unable to read Pub archive")
			}
			break
		}
	}
	if file == nil {
		return nil, BadRequest("Pub upload requires a file")
	}
	defer file.Close()
	temp, err := os.CreateTemp(s.store.TempDir(), "pub-*.tar.gz")
	if err != nil {
		return nil, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := io.Copy(temp, io.LimitReader(file, maxGenericUploadBytes)); err != nil {
		temp.Close()
		return nil, BadRequest("Unable to store Pub archive")
	}
	temp.Close()
	name, version, description := inspectPubspec(tempName)
	if name == "" || version == "" {
		return nil, BadRequest("Pub archive must contain a pubspec.yaml with name and version")
	}
	sum, size, _, err := s.store.SaveBlobFile(tempName)
	if err != nil {
		return nil, err
	}
	now := s.now()
	archivePath := pubArchivePath(name, version)
	asset := &store.Asset{
		Repository: repository.Name, Format: "pub", Path: archivePath, Module: name, Version: version,
		Kind: "archive", ContentType: "application/gzip", Size: size, SHA256: sum, Source: "hosted",
		CreatedAt: now, UpdatedAt: now, Attributes: map[string]string{"description": description},
	}
	component := &store.Component{
		Repository: repository.Name, Format: "pub", Module: name, Version: version,
		Kind: "pub-package", Time: now, CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]string{"description": description, "archive_sha256": sum},
	}
	if err := s.store.PutAsset(asset, component, true); err != nil {
		return nil, err
	}
	s.sessionMu.Lock()
	delete(s.pubSessions, id)
	s.sessionMu.Unlock()
	location := request.BaseURL + "/repository/" + repository.Name + "/api/packages/versions/finalize/" + store.RandomID(8)
	header := make(http.Header)
	header.Set("Location", location)
	return &Response{Status: http.StatusSeeOther, Header: header}, nil
}

func (s *Service) pubPackageMetadata(repository *store.Repository, request *Request, name string) (*Response, error) {
	versions := s.pubVersions(repository, request, name)
	if len(versions) == 0 {
		return nil, NotFound(name)
	}
	body, _ := jsonBytes(map[string]any{
		"name":     name,
		"latest":   versions[len(versions)-1],
		"versions": versions,
	})
	return bytesResponse(http.StatusOK, "application/vnd.pub.v2+json", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
}

func (s *Service) pubVersionMetadata(repository *store.Repository, request *Request, name, version string) (*Response, error) {
	component, ok := s.store.GetComponent(repository.Name, name, version)
	if !ok {
		return nil, NotFound(name + " " + version)
	}
	entry := map[string]any{
		"version":        version,
		"pubspec":        map[string]any{"name": name, "version": version, "description": component.Attributes["description"]},
		"archive_url":    request.BaseURL + "/repository/" + repository.Name + "/" + pubArchivePath(name, version),
		"archive_sha256": component.Attributes["archive_sha256"],
		"published":      component.Time.UTC().Format(time.RFC3339),
	}
	body, _ := jsonBytes(entry)
	return bytesResponse(http.StatusOK, "application/vnd.pub.v2+json", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
}

func (s *Service) pubVersions(repository *store.Repository, request *Request, name string) []map[string]any {
	components := s.store.ListComponents(repository.Name, name, "")
	entries := make([]map[string]any, 0, len(components))
	for _, component := range components {
		entries = append(entries, map[string]any{
			"version":        component.Version,
			"pubspec":        map[string]any{"name": name, "version": component.Version, "description": component.Attributes["description"]},
			"archive_url":    request.BaseURL + "/repository/" + repository.Name + "/" + pubArchivePath(name, component.Version),
			"archive_sha256": component.Attributes["archive_sha256"],
			"published":      component.Time.UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return fmt.Sprint(entries[i]["version"]) < fmt.Sprint(entries[j]["version"])
	})
	return entries
}

func (s *Service) pubPackageNames(repository *store.Repository, request *Request, key string) (*Response, error) {
	components := s.store.ListComponents(repository.Name, "", "")
	seen := map[string]struct{}{}
	packages := make([]map[string]string, 0)
	for _, component := range components {
		if _, ok := seen[component.Module]; ok {
			continue
		}
		seen[component.Module] = struct{}{}
		packages = append(packages, map[string]string{"name": component.Module})
	}
	body, _ := jsonBytes(map[string]any{key: packages})
	return bytesResponse(http.StatusOK, "application/json", body, "", time.Time{}, request.HeadOnly), nil
}

func pubArchivePath(name, version string) string {
	return "api/archives/" + name + "-" + version + ".tar.gz"
}

func parsePubArchivePath(path string) (string, string, bool) {
	path = strings.Trim(path, "/")
	if strings.HasPrefix(path, "api/archives/") {
		path = strings.TrimPrefix(path, "api/archives/")
	} else if strings.HasPrefix(path, "packages/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 4 && parts[2] == "versions" {
			file := parts[len(parts)-1]
			name := parts[1]
			return name, strings.TrimSuffix(strings.TrimPrefix(file, name+"-"), ".tar.gz"), true
		}
	} else {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			name := parts[0]
			version := parts[1]
			return name, version, true
		}
	}
	if !strings.HasSuffix(path, ".tar.gz") {
		return "", "", false
	}
	base := strings.TrimSuffix(path, ".tar.gz")
	for index := len(base) - 1; index > 0; index-- {
		if base[index] == '-' && index+1 < len(base) && base[index+1] >= '0' && base[index+1] <= '9' {
			return base[:index], base[index+1:], true
		}
	}
	return "", "", false
}

func inspectPubspec(path string) (string, string, string) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", "", ""
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			return "", "", ""
		}
		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(header.Name, "pubspec.yaml") {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, 1<<20))
		if err != nil {
			return "", "", ""
		}
		name, version, description := "", "", ""
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "name:") && name == "":
				name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "name:")), `"'`)
			case strings.HasPrefix(trimmed, "version:") && version == "":
				version = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "version:")), `"'`)
			case strings.HasPrefix(trimmed, "description:") && description == "":
				description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "description:")), `"'`)
			}
		}
		return name, version, description
	}
}
