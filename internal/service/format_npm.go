package service

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type npmVersion struct {
	Version string `json:"version"`
	Name    string `json:"name"`
	Dist    struct {
		Tarball string `json:"tarball"`
	} `json:"dist"`
}

type npmPackument struct {
	ID          string                     `json:"_id"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	DistTags    map[string]string          `json:"dist-tags"`
	Versions    map[string]json.RawMessage `json:"versions"`
	Attachments map[string]npmAttachment   `json:"_attachments"`
	Raw         map[string]json.RawMessage `json:"-"`
}

type npmAttachment struct {
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
	Length      int    `json:"length"`
}

func (s *Service) serveNpm(repository *store.Repository, request *Request) (*Response, error) {
	if repository.Type == "group" {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			return nil, MethodNotAllowed("Group repositories are read-only")
		}
		var lastErr error
		for _, member := range s.groupMembers(repository) {
			response, err := s.serveNpm(member, request)
			if err == nil {
				return response, nil
			}
			if isNotFoundError(err) {
				continue
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, NotFound(request.Path)
	}
	if repository.Type == "proxy" {
		return s.serveNpmProxy(repository, request)
	}
	path := strings.TrimLeft(request.Path, "/")
	switch {
	case path == "-/ping":
		return bytesResponse(http.StatusOK, "application/json", []byte("{}"), "", time.Time{}, request.HeadOnly), nil
	case path == "-/whoami":
		body, _ := jsonBytes(map[string]string{"username": principalName(request)})
		return bytesResponse(http.StatusOK, "application/json", body, "", time.Time{}, request.HeadOnly), nil
	case strings.HasPrefix(path, "-/v1/search"):
		return s.npmSearch(repository, request)
	case strings.HasPrefix(path, "-/package/"):
		return s.npmDistTags(repository, request, strings.TrimPrefix(path, "-/package/"))
	case strings.Contains(path, "/-/"):
		if request.Method == http.MethodGet || request.Method == http.MethodHead {
			return s.serveStoredFile(repository, path, request.HeadOnly, "application/octet-stream")
		}
		return nil, MethodNotAllowed("Unsupported npm tarball method")
	case request.Method == http.MethodPut || request.Method == http.MethodPost:
		return s.npmPublish(repository, request, path)
	case request.Method == http.MethodGet || request.Method == http.MethodHead:
		return s.npmGetMetadata(repository, request, path)
	case request.Method == http.MethodDelete:
		deleted, err := s.store.DeleteAsset(repository.Name, npmPackageName(path))
		if err != nil {
			return nil, err
		}
		if !deleted {
			return nil, NotFound(path)
		}
		return bytesResponse(http.StatusNoContent, "text/plain", nil, "", time.Time{}, true), nil
	}
	return s.serveGeneric(repository, request)
}

func (s *Service) serveNpmProxy(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return nil, MethodNotAllowed("Proxy repositories are read-only")
	}
	if strings.HasPrefix(path, "-/package/") {
		return s.npmProxyDistTags(repository, request, strings.TrimPrefix(path, "-/package/"))
	}
	response, err := s.serveGenericProxy(repository, request)
	if err != nil {
		return nil, err
	}
	if request.HeadOnly || strings.Contains(path, "/-/") || strings.HasSuffix(path, "/latest") ||
		strings.HasPrefix(path, "-/") {
		return response, nil
	}
	body, err := response.ReadBounded(maxMetadataBytes)
	response.Close()
	if err != nil {
		return nil, err
	}
	rewritten := rewriteNpmTarballs(body, remoteURL(repository),
		request.BaseURL+"/repository/"+repository.Name)
	return bytesResponse(http.StatusOK, "application/json", rewritten, sha256Hex(rewritten), time.Time{}, false), nil
}

func (s *Service) npmGetMetadata(repository *store.Repository, request *Request, path string) (*Response, error) {
	name := npmPackageName(path)
	asset, ok := s.store.GetAsset(repository.Name, name)
	if !ok {
		return nil, NotFound(path)
	}
	body, err := s.store.ReadBlob(asset)
	if err != nil {
		return nil, NotFound(path)
	}
	// A path with an extra segment requests one version's manifest.
	if version := npmVersionSuffix(path, name); version != "" {
		var document map[string]json.RawMessage
		if err := json.Unmarshal(body, &document); err == nil {
			var versions map[string]json.RawMessage
			if err := json.Unmarshal(document["versions"], &versions); err == nil {
				if manifest, ok := versions[version]; ok {
					return bytesResponse(http.StatusOK, "application/json", manifest, sha256Hex(manifest), time.Time{}, request.HeadOnly), nil
				}
			}
		}
		return nil, NotFound(path)
	}
	return bytesResponse(http.StatusOK, "application/json", body, asset.SHA256, asset.UpdatedAt, request.HeadOnly), nil
}

func (s *Service) npmPublish(repository *store.Repository, request *Request, path string) (*Response, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, 512<<20))
	if err != nil {
		return nil, BadRequest("Unable to read npm publish body")
	}
	var packument npmPackument
	if err := json.Unmarshal(body, &packument); err != nil {
		return nil, BadRequest("Invalid npm publish document")
	}
	name := firstNonEmpty(packument.Name, npmPackageName(path))
	if name == "" {
		return nil, BadRequest("npm package name is required")
	}
	now := s.now()
	fileVersions := map[string]string{}
	for version, raw := range packument.Versions {
		var manifest npmVersion
		_ = json.Unmarshal(raw, &manifest)
		if manifest.Dist.Tarball != "" {
			filename := manifest.Dist.Tarball
			if slash := strings.LastIndexByte(filename, '/'); slash >= 0 {
				filename = filename[slash+1:]
			}
			fileVersions[filename] = version
		}
	}
	for filename, attachment := range packument.Attachments {
		data, err := base64.StdEncoding.DecodeString(attachment.Data)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(attachment.Data)
			if err != nil {
				return nil, BadRequest("Invalid npm attachment: " + filename)
			}
		}
		sum, size, _, err := s.store.SaveBlobBytes(data)
		if err != nil {
			return nil, err
		}
		version := fileVersions[filename]
		tarballPath := name + "/-/" + filename
		asset := &store.Asset{
			Repository: repository.Name, Format: "npm", Namespace: npmNamespace(name), Path: tarballPath,
			Module: name, Version: version, Kind: "tarball", ContentType: "application/octet-stream",
			Size: size, SHA256: sum, Source: "hosted", CreatedAt: now, UpdatedAt: now,
		}
		var component *store.Component
		if version != "" {
			component = &store.Component{
				Repository: repository.Name, Format: "npm", Namespace: npmNamespace(name),
				Module: name, Version: version, Kind: "npm-package", Time: now, CreatedAt: now, UpdatedAt: now,
			}
		}
		if err := s.store.PutAsset(asset, component, true); err != nil {
			return nil, err
		}
	}
	// Remove attachments and rewrite tarball URLs to this repository.
	var document map[string]json.RawMessage
	_ = json.Unmarshal(body, &document)
	delete(document, "_attachments")
	if packument.DistTags == nil {
		packument.DistTags = map[string]string{}
	}
	if _, ok := packument.DistTags["latest"]; !ok {
		latest := ""
		for version := range packument.Versions {
			if latest == "" || version > latest {
				latest = version
			}
		}
		if latest != "" {
			packument.DistTags["latest"] = latest
		}
	}
	versions := map[string]json.RawMessage{}
	base := request.BaseURL + "/repository/" + repository.Name + "/" + name + "/-/"
	for version, raw := range packument.Versions {
		var manifest map[string]any
		if json.Unmarshal(raw, &manifest) == nil {
			if dist, ok := manifest["dist"].(map[string]any); ok {
				if tarball, ok := dist["tarball"].(string); ok && tarball != "" {
					if slash := strings.LastIndexByte(tarball, '/'); slash >= 0 {
						dist["tarball"] = base + tarball[slash+1:]
					}
				}
			}
			updated, _ := json.Marshal(manifest)
			versions[version] = updated
		} else {
			versions[version] = raw
		}
	}
	document["versions"], _ = json.Marshal(versions)
	document["dist-tags"], _ = json.Marshal(packument.DistTags)
	document["name"], _ = json.Marshal(name)
	document["_id"], _ = json.Marshal(name)
	updated, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	sum, size, _, err := s.store.SaveBlobBytes(updated)
	if err != nil {
		return nil, err
	}
	metadata := &store.Asset{
		Repository: repository.Name, Format: "npm", Namespace: npmNamespace(name), Path: name,
		Module: name, Kind: "packument", ContentType: "application/json", Size: size,
		SHA256: sum, Source: "hosted", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.PutAsset(metadata, nil, true); err != nil {
		return nil, err
	}
	response, _ := jsonBytes(map[string]any{"ok": true, "id": name, "rev": "1"})
	return bytesResponse(http.StatusCreated, "application/json", response, "", time.Time{}, false), nil
}

func (s *Service) npmDistTags(repository *store.Repository, request *Request, rest string) (*Response, error) {
	parts := strings.SplitN(strings.Trim(rest, "/"), "/dist-tags", 2)
	name := npmPackageName(parts[0])
	tag := ""
	if len(parts) == 2 {
		tag = strings.Trim(parts[1], "/")
	}
	asset, ok := s.store.GetAsset(repository.Name, name)
	if !ok {
		return nil, NotFound(name)
	}
	body, err := s.store.ReadBlob(asset)
	if err != nil {
		return nil, NotFound(name)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, BadRequest("Invalid npm packument")
	}
	tags := map[string]string{}
	if raw, ok := document["dist-tags"]; ok {
		_ = json.Unmarshal(raw, &tags)
	}
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		if tag == "" {
			response, _ := jsonBytes(tags)
			return bytesResponse(http.StatusOK, "application/json", response, "", time.Time{}, request.HeadOnly), nil
		}
		version, ok := tags[tag]
		if !ok {
			return nil, NotFound(tag)
		}
		response, _ := jsonBytes(version)
		return bytesResponse(http.StatusOK, "application/json", response, "", time.Time{}, request.HeadOnly), nil
	case http.MethodPut:
		versionBody, _ := io.ReadAll(io.LimitReader(request.Body, 1<<20))
		version := strings.Trim(strings.TrimSpace(string(versionBody)), `"'`)
		if strings.HasPrefix(version, "{") {
			var payload struct {
				Version string `json:"version"`
			}
			_ = json.Unmarshal(versionBody, &payload)
			version = payload.Version
		}
		if version == "" {
			return nil, BadRequest("dist-tag version is required")
		}
		tags[tag] = version
	case http.MethodDelete:
		delete(tags, tag)
	default:
		return nil, MethodNotAllowed("Unsupported dist-tag method")
	}
	document["dist-tags"], _ = json.Marshal(tags)
	updated, _ := json.Marshal(document)
	sum, size, _, err := s.store.SaveBlobBytes(updated)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := s.store.PutAsset(&store.Asset{
		Repository: repository.Name, Format: "npm", Path: name, Module: name, Kind: "packument",
		ContentType: "application/json", Size: size, SHA256: sum, Source: "hosted",
		CreatedAt: now, UpdatedAt: now,
	}, nil, true); err != nil {
		return nil, err
	}
	response, _ := jsonBytes(map[string]any{"ok": true})
	return bytesResponse(http.StatusOK, "application/json", response, "", time.Time{}, false), nil
}

func (s *Service) npmProxyDistTags(repository *store.Repository, request *Request, rest string) (*Response, error) {
	parts := strings.SplitN(strings.Trim(rest, "/"), "/dist-tags", 2)
	name := npmPackageName(parts[0])
	response, err := s.serveGenericProxy(repository, &Request{
		Method: http.MethodGet, Path: name, EscapedPath: escapePath(name),
		Header: request.Header, HeadOnly: false, BaseURL: request.BaseURL,
	})
	if err != nil {
		return nil, err
	}
	body, err := response.ReadBounded(maxMetadataBytes)
	response.Close()
	if err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, BadUpstream("Invalid upstream npm packument")
	}
	tags := document["dist-tags"]
	if len(parts) < 2 {
		return bytesResponse(http.StatusOK, "application/json", tags, "", time.Time{}, request.HeadOnly), nil
	}
	tag := strings.Trim(parts[1], "/")
	var values map[string]string
	_ = json.Unmarshal(tags, &values)
	version, ok := values[tag]
	if !ok {
		return nil, NotFound(tag)
	}
	responseBody, _ := jsonBytes(version)
	return bytesResponse(http.StatusOK, "application/json", responseBody, "", time.Time{}, request.HeadOnly), nil
}

func (s *Service) npmSearch(repository *store.Repository, request *Request) (*Response, error) {
	query := strings.ToLower(strings.TrimSpace(request.Query.Get("text")))
	components := s.store.ListComponents(repository.Name, "", query)
	objects := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, component := range components {
		if _, ok := seen[component.Module]; ok {
			continue
		}
		seen[component.Module] = struct{}{}
		objects = append(objects, map[string]any{
			"package": map[string]any{
				"name": component.Module, "version": component.Version,
				"description": component.Attributes["description"],
			},
		})
	}
	body, _ := jsonBytes(map[string]any{"objects": objects, "total": len(objects)})
	return bytesResponse(http.StatusOK, "application/json", body, "", time.Time{}, request.HeadOnly), nil
}

func rewriteNpmTarballs(body []byte, upstreamBase, localBase string) []byte {
	var document map[string]json.RawMessage
	if json.Unmarshal(body, &document) != nil {
		return body
	}
	var versions map[string]json.RawMessage
	if raw, ok := document["versions"]; !ok || json.Unmarshal(raw, &versions) != nil {
		return body
	}
	changed := false
	for version, raw := range versions {
		var manifest map[string]any
		if json.Unmarshal(raw, &manifest) != nil {
			continue
		}
		dist, ok := manifest["dist"].(map[string]any)
		if !ok {
			continue
		}
		tarball, _ := dist["tarball"].(string)
		if tarball == "" {
			continue
		}
		if strings.HasPrefix(tarball, upstreamBase) {
			dist["tarball"] = localBase + strings.TrimPrefix(tarball, upstreamBase)
			updated, _ := json.Marshal(manifest)
			versions[version] = updated
			changed = true
		}
	}
	if !changed {
		return body
	}
	document["versions"], _ = json.Marshal(versions)
	updated, err := json.Marshal(document)
	if err != nil {
		return body
	}
	return updated
}

func npmPackageName(path string) string {
	path = strings.Trim(path, "/")
	if index := strings.Index(path, "/-/"); index >= 0 {
		return path[:index]
	}
	if strings.HasPrefix(path, "@") {
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	if index := strings.IndexByte(path, '/'); index >= 0 {
		return path[:index]
	}
	return path
}

func npmVersionSuffix(path, name string) string {
	rest := strings.TrimPrefix(strings.Trim(path, "/"), name)
	rest = strings.Trim(rest, "/")
	return rest
}

func npmNamespace(name string) string {
	if strings.HasPrefix(name, "@") {
		if index := strings.IndexByte(name, '/'); index > 0 {
			return name[:index]
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
