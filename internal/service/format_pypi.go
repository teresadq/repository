package service

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

var pypiNormalizeRE = regexp.MustCompile(`[-_.]+`)

func normalizePypiName(name string) string {
	return strings.ToLower(pypiNormalizeRE.ReplaceAllString(strings.TrimSpace(name), "-"))
}

func (s *Service) servePypi(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" {
		switch {
		case request.Method == http.MethodPost && path == "":
			return s.pypiUpload(repository, request)
		case (request.Method == http.MethodGet || request.Method == http.MethodHead) && (path == "simple" || path == "simple/"):
			return s.pypiRootIndex(repository, request)
		case (request.Method == http.MethodGet || request.Method == http.MethodHead) && strings.HasPrefix(path, "simple/"):
			project := normalizePypiName(strings.TrimSuffix(strings.TrimPrefix(path, "simple/"), "/"))
			if project == "" {
				return s.pypiRootIndex(repository, request)
			}
			return s.pypiProjectIndex(repository, project, request)
		case (request.Method == http.MethodGet || request.Method == http.MethodHead) && strings.HasPrefix(path, "pypi/") && strings.HasSuffix(path, "/json"):
			project := normalizePypiName(strings.TrimSuffix(strings.TrimPrefix(path, "pypi/"), "/json"))
			return s.pypiJSON(repository, project, request)
		}
	}
	return s.serveGeneric(repository, request)
}

func (s *Service) pypiUpload(repository *store.Repository, request *Request) (*Response, error) {
	reader := multipart.NewReader(io.LimitReader(request.Body, 512<<20), multipartBoundary(request.Header))
	form, err := reader.ReadForm(64 << 20)
	if err != nil {
		return nil, BadRequest("Invalid PyPI multipart upload")
	}
	defer form.RemoveAll()
	action := formValue(form, ":action")
	if action != "" && action != "file_upload" {
		return nil, BadRequest("Unsupported PyPI action: " + action)
	}
	name := normalizePypiName(formValue(form, "name"))
	version := strings.TrimSpace(formValue(form, "version"))
	if name == "" || version == "" {
		return nil, BadRequest("PyPI upload requires name and version")
	}
	files := form.File["content"]
	if len(files) == 0 {
		return nil, BadRequest("PyPI upload requires a content file")
	}
	header := files[0]
	file, err := header.Open()
	if err != nil {
		return nil, BadRequest("Unable to read uploaded distribution")
	}
	defer file.Close()
	sum, size, _, err := s.store.SaveBlob(file, maxGenericUploadBytes)
	if err != nil {
		return nil, err
	}
	path := "packages/" + name + "/" + version + "/" + header.Filename
	now := s.now()
	blob, _ := s.store.ReadBlob(&store.Asset{SHA256: sum})
	attributes := map[string]string{"filename": header.Filename}
	if blob != nil {
		md5sum := md5.Sum(blob)
		attributes["md5"] = hex.EncodeToString(md5sum[:])
	}
	asset := &store.Asset{
		Repository: repository.Name, Format: "pypi", Path: path, Module: name, Version: version,
		Kind: "distribution", ContentType: detectContentType("pypi", path), Size: size,
		SHA256: sum, Source: "hosted", CreatedAt: now, UpdatedAt: now, Attributes: attributes,
	}
	component := &store.Component{
		Repository: repository.Name, Format: "pypi", Module: name, Version: version,
		Kind: "python-package", Time: now, CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]string{"filename": header.Filename},
	}
	if err := s.store.PutAsset(asset, component, true); err != nil {
		return nil, err
	}
	return bytesResponse(http.StatusOK, "text/plain", []byte("OK"), "", time.Time{}, false), nil
}

func (s *Service) pypiRootIndex(repository *store.Repository, request *Request) (*Response, error) {
	components := s.store.ListComponents(repository.Name, "", "")
	names := map[string]struct{}{}
	for _, component := range components {
		names[component.Module] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	var builder strings.Builder
	builder.WriteString("<!DOCTYPE html><html><head><meta name=\"pypi:repository-version\" content=\"1.0\"><title>Simple index</title></head><body>\n")
	for _, name := range sorted {
		fmt.Fprintf(&builder, "<a href=\"%s/\">%s</a>\n", html.EscapeString(name), html.EscapeString(name))
	}
	builder.WriteString("</body></html>\n")
	body := []byte(builder.String())
	return bytesResponse(http.StatusOK, "text/html; charset=utf-8", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
}

func (s *Service) pypiProjectIndex(repository *store.Repository, project string, request *Request) (*Response, error) {
	if len(s.store.ComponentVersions(repository.Name, project)) == 0 {
		return nil, NotFound(project)
	}
	assets := s.store.ListAssets(repository.Name, "packages/"+project+"/")
	var builder strings.Builder
	builder.WriteString("<!DOCTYPE html><html><head><meta name=\"pypi:repository-version\" content=\"1.0\"><title>Links for " +
		html.EscapeString(project) + "</title></head><body><h1>Links for " + html.EscapeString(project) + "</h1>\n")
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Path, "packages/"+project+"/") {
			continue
		}
		filename := asset.Path[strings.LastIndexByte(asset.Path, '/')+1:]
		href := "../../" + asset.Path
		if md5sum := asset.Attributes["md5"]; md5sum != "" {
			href += "#md5=" + md5sum
		}
		requiresPython := asset.Attributes["requires-python"]
		if requiresPython != "" {
			fmt.Fprintf(&builder, "<a href=\"%s\" data-requires-python=\"%s\">%s</a><br/>\n",
				html.EscapeString(href), html.EscapeString(requiresPython), html.EscapeString(filename))
		} else {
			fmt.Fprintf(&builder, "<a href=\"%s\">%s</a><br/>\n", html.EscapeString(href), html.EscapeString(filename))
		}
	}
	builder.WriteString("</body></html>\n")
	body := []byte(builder.String())
	return bytesResponse(http.StatusOK, "text/html; charset=utf-8", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
}

func (s *Service) pypiJSON(repository *store.Repository, project string, request *Request) (*Response, error) {
	versions := s.store.ComponentVersions(repository.Name, project)
	if len(versions) == 0 {
		return nil, NotFound(project)
	}
	sort.Strings(versions)
	assets := s.store.ListAssets(repository.Name, "packages/"+project+"/")
	releases := map[string][]map[string]any{}
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Path, "packages/"+project+"/") {
			continue
		}
		filename := asset.Path[strings.LastIndexByte(asset.Path, '/')+1:]
		releases[asset.Version] = append(releases[asset.Version], map[string]any{
			"filename": filename,
			"url":      request.BaseURL + "/repository/" + repository.Name + "/" + asset.Path,
			"size":     asset.Size,
			"digests":  map[string]string{"sha256": asset.SHA256, "md5": asset.Attributes["md5"]},
		})
	}
	body, _ := jsonBytes(map[string]any{
		"info": map[string]any{
			"name": project, "version": versions[len(versions)-1],
			"summary": "", "requires_python": "",
		},
		"releases": releases,
		"urls":     releases[versions[len(versions)-1]],
	})
	return bytesResponse(http.StatusOK, "application/json", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
}

func formValue(form *multipart.Form, key string) string {
	if values := form.Value[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}
