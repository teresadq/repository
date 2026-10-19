package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type dockerUpload struct {
	repository string
	image      string
	temp       string
	size       int64
	createdAt  time.Time
}

func (s *Service) serveDocker(repository *store.Repository, request *Request) (*Response, error) {
	if repository.Type == "group" {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			return nil, MethodNotAllowed("Group repositories are read-only")
		}
		var lastErr error
		for _, member := range s.groupMembers(repository) {
			response, err := s.serveDocker(member, request)
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
		return s.serveDockerProxy(repository, request)
	}
	return s.serveDockerHosted(repository, request)
}

func (s *Service) serveDockerHosted(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if path == "v2" || path == "v2/" {
		return dockerPing(request.HeadOnly)
	}
	kind, image, reference, id := parseDockerPath(path)
	switch kind {
	case "catalog":
		return s.dockerCatalog(repository, request)
	case "tags":
		return s.dockerTags(repository, image, request)
	case "manifest":
		switch request.Method {
		case http.MethodGet, http.MethodHead:
			return s.dockerGetManifest(repository, image, reference, request)
		case http.MethodPut:
			return s.dockerPutManifest(repository, image, reference, request)
		case http.MethodDelete:
			return s.dockerDelete(repository, image, reference)
		}
	case "blob":
		switch request.Method {
		case http.MethodGet, http.MethodHead:
			return s.dockerGetBlob(repository, image, reference, request)
		case http.MethodDelete:
			deleted, err := s.store.DeleteAsset(repository.Name, "v2/blobs/"+reference)
			if err != nil {
				return nil, err
			}
			if !deleted {
				return nil, NotFound(reference)
			}
			return dockerEmpty(http.StatusAccepted), nil
		}
	case "upload-start":
		if request.Method == http.MethodPost {
			return s.dockerStartUpload(repository, image, request)
		}
	case "upload":
		switch request.Method {
		case http.MethodPatch:
			return s.dockerPatchUpload(repository, image, id, request)
		case http.MethodPut:
			return s.dockerFinishUpload(repository, image, id, request)
		case http.MethodDelete:
			s.dockerRemoveSession(id)
			return dockerEmpty(http.StatusNoContent), nil
		case http.MethodGet:
			return dockerEmpty(http.StatusOK), nil
		}
	case "referrers":
		body, _ := jsonBytes(map[string]any{
			"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []any{},
		})
		return bytesResponse(http.StatusOK, "application/vnd.oci.image.index.v1+json", body, "", time.Time{}, request.HeadOnly), nil
	}
	return nil, NotFound(path)
}

func parseDockerPath(path string) (kind, image, reference, id string) {
	path = strings.Trim(path, "/")
	if path == "v2" || path == "v2/" {
		return "ping", "", "", ""
	}
	if !strings.HasPrefix(path, "v2/") {
		return "", "", "", ""
	}
	rest := strings.TrimPrefix(path, "v2/")
	if rest == "_catalog" {
		return "catalog", "", "", ""
	}
	if index := strings.LastIndex(rest, "/manifests/"); index > 0 {
		return "manifest", rest[:index], strings.TrimPrefix(rest[index:], "/manifests/"), ""
	}
	if index := strings.LastIndex(rest, "/blobs/uploads/"); index > 0 {
		return "upload", rest[:index], "", strings.TrimPrefix(rest[index:], "/blobs/uploads/")
	}
	if strings.HasSuffix(rest, "/blobs/uploads") {
		return "upload-start", strings.TrimSuffix(rest, "/blobs/uploads"), "", ""
	}
	if index := strings.LastIndex(rest, "/blobs/"); index > 0 {
		return "blob", rest[:index], strings.TrimPrefix(rest[index:], "/blobs/"), ""
	}
	if strings.HasSuffix(rest, "/tags/list") {
		return "tags", strings.TrimSuffix(rest, "/tags/list"), "", ""
	}
	if index := strings.LastIndex(rest, "/referrers/"); index > 0 {
		return "referrers", rest[:index], strings.TrimPrefix(rest[index:], "/referrers/"), ""
	}
	return "", "", "", ""
}

func (s *Service) dockerPutManifest(repository *store.Repository, image, reference string, request *Request) (*Response, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, 32<<20))
	if err != nil {
		return nil, BadRequest("Unable to read manifest")
	}
	if len(body) == 0 {
		return nil, BadRequest("Manifest is empty")
	}
	blobSum, blobSize, _, err := s.store.SaveBlobBytes(body)
	if err != nil {
		return nil, err
	}
	digest := "sha256:" + blobSum
	if strings.HasPrefix(reference, "sha256:") && reference != digest {
		return nil, BadRequest("Manifest digest does not match reference")
	}
	contentType := request.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/vnd.oci.image.manifest.v1+json"
	}
	now := s.now()
	namespace, name := dockerImageParts(image)
	manifestDigestPath := "v2/" + image + "/manifests/" + digest
	asset := &store.Asset{
		Repository: repository.Name, Format: "docker", Namespace: namespace, Path: manifestDigestPath,
		Module: name, Version: reference, Kind: "manifest", ContentType: contentType,
		Size: blobSize, SHA256: blobSum, Source: "hosted",
		CreatedAt: now, UpdatedAt: now, Attributes: map[string]string{"digest": digest},
	}
	tagAsset := *asset
	tagAsset.Path = "v2/" + image + "/manifests/" + reference
	component := &store.Component{
		Repository: repository.Name, Format: "docker", Namespace: namespace, Module: name,
		Version: reference, Kind: "docker-image", Time: now, CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]string{"digest": digest, "mediaType": contentType},
	}
	if err := s.store.PutRelease([]*store.Asset{asset, &tagAsset}, []*store.Component{component}, true); err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Docker-Content-Digest", digest)
	header.Set("Location", request.BaseURL+"/repository/"+repository.Name+"/v2/"+image+"/manifests/"+digest)
	return &Response{Status: http.StatusCreated, Header: header}, nil
}

func (s *Service) dockerGetManifest(repository *store.Repository, image, reference string, request *Request) (*Response, error) {
	path := "v2/" + image + "/manifests/" + reference
	asset, ok := s.store.GetAsset(repository.Name, path)
	if !ok {
		return nil, NotFound(path)
	}
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(path)
	}
	etag := asset.SHA256
	response := streamResponse(http.StatusOK, asset.ContentType, body, asset.Size, etag, asset.UpdatedAt, request.HeadOnly)
	if digest := asset.Attributes["digest"]; digest != "" {
		response.Header.Set("Docker-Content-Digest", digest)
	} else if asset.SHA256 != "" {
		response.Header.Set("Docker-Content-Digest", "sha256:"+asset.SHA256)
	}
	return response, nil
}

func (s *Service) dockerGetBlob(repository *store.Repository, image, digest string, request *Request) (*Response, error) {
	asset, ok := s.store.GetAsset(repository.Name, "v2/"+image+"/blobs/"+digest)
	if !ok {
		asset, ok = s.store.GetAsset(repository.Name, "v2/blobs/"+digest)
	}
	if !ok {
		return nil, NotFound(digest)
	}
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(digest)
	}
	file, isFile := body.(*os.File)
	if request.Header.Get("Range") == "" || !isFile {
		return streamResponse(http.StatusOK, "application/octet-stream", body, asset.Size, digest, asset.UpdatedAt, request.HeadOnly), nil
	}
	start, end, ok := parseRange(request.Header.Get("Range"), asset.Size)
	if !ok {
		body.Close()
		header := make(http.Header)
		header.Set("Content-Range", fmt.Sprintf("bytes */%d", asset.Size))
		return &Response{Status: http.StatusRequestedRangeNotSatisfiable, Header: header}, nil
	}
	section := io.NewSectionReader(file, start, end-start+1)
	header := make(http.Header)
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Length", fmt.Sprintf("%d", end-start+1))
	header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, asset.Size))
	header.Set("Docker-Content-Digest", digest)
	if request.HeadOnly {
		body.Close()
		return &Response{Status: http.StatusPartialContent, Header: header, Size: end - start + 1}, nil
	}
	return &Response{Status: http.StatusPartialContent, Header: header, Size: end - start + 1, Body: sectionCloser{SectionReader: section, file: file}}, nil
}

func (s *Service) dockerStartUpload(repository *store.Repository, image string, request *Request) (*Response, error) {
	s.sessionMu.Lock()
	for id, session := range s.dockerSessions {
		if time.Since(session.createdAt) > time.Hour {
			_ = os.Remove(session.temp)
			delete(s.dockerSessions, id)
		}
	}
	id := store.RandomID(16)
	temp, err := os.CreateTemp(s.store.TempDir(), "docker-upload-*")
	if err != nil {
		s.sessionMu.Unlock()
		return nil, err
	}
	temp.Close()
	s.dockerSessions[id] = &dockerUpload{repository: repository.Name, image: image, temp: temp.Name(), createdAt: s.now()}
	s.sessionMu.Unlock()
	header := make(http.Header)
	header.Set("Location", request.BaseURL+"/repository/"+repository.Name+"/v2/"+image+"/blobs/uploads/"+id)
	header.Set("Docker-Upload-UUID", id)
	header.Set("Range", "0-0")
	header.Set("Content-Length", "0")
	return &Response{Status: http.StatusAccepted, Header: header}, nil
}

func (s *Service) dockerPatchUpload(repository *store.Repository, image, id string, request *Request) (*Response, error) {
	s.sessionMu.Lock()
	session := s.dockerSessions[id]
	s.sessionMu.Unlock()
	if session == nil || session.repository != repository.Name || session.image != image {
		return nil, NotFound(id)
	}
	file, err := os.OpenFile(session.temp, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(file, io.LimitReader(request.Body, maxGenericUploadBytes))
	file.Close()
	if err != nil {
		return nil, err
	}
	s.sessionMu.Lock()
	session.size += n
	size := session.size
	s.sessionMu.Unlock()
	header := make(http.Header)
	header.Set("Location", request.BaseURL+"/repository/"+repository.Name+"/v2/"+image+"/blobs/uploads/"+id)
	header.Set("Range", fmt.Sprintf("0-%d", size-1))
	header.Set("Docker-Upload-UUID", id)
	return &Response{Status: http.StatusAccepted, Header: header}, nil
}

func (s *Service) dockerFinishUpload(repository *store.Repository, image, id string, request *Request) (*Response, error) {
	s.sessionMu.Lock()
	session := s.dockerSessions[id]
	delete(s.dockerSessions, id)
	s.sessionMu.Unlock()
	if session == nil || session.repository != repository.Name || session.image != image {
		return nil, NotFound(id)
	}
	if request.Body != nil {
		if file, err := os.OpenFile(session.temp, os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, _ = io.Copy(file, io.LimitReader(request.Body, maxGenericUploadBytes))
			file.Close()
		}
	}
	sum, size, _, err := s.store.SaveBlobFile(session.temp)
	defer os.Remove(session.temp)
	if err != nil {
		return nil, err
	}
	expected := request.Query.Get("digest")
	if expected == "" {
		expected = request.Header.Get("Docker-Content-Digest")
	}
	if expected != "" && expected != "sha256:"+sum {
		return nil, BadRequest("Blob digest does not match uploaded content")
	}
	digest := "sha256:" + sum
	now := s.now()
	asset := &store.Asset{
		Repository: repository.Name, Format: "docker", Path: "v2/blobs/" + digest,
		Module: image, Version: digest, Kind: "blob", ContentType: "application/octet-stream",
		Size: size, SHA256: sum, Source: "hosted", CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]string{"digest": digest},
	}
	if err := s.store.PutAsset(asset, nil, true); err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Location", request.BaseURL+"/repository/"+repository.Name+"/v2/"+image+"/blobs/"+digest)
	header.Set("Docker-Content-Digest", digest)
	return &Response{Status: http.StatusCreated, Header: header}, nil
}

func (s *Service) dockerRemoveSession(id string) {
	s.sessionMu.Lock()
	session := s.dockerSessions[id]
	delete(s.dockerSessions, id)
	s.sessionMu.Unlock()
	if session != nil {
		_ = os.Remove(session.temp)
	}
}

func (s *Service) dockerTags(repository *store.Repository, image string, request *Request) (*Response, error) {
	assets := s.store.ListAssets(repository.Name, "v2/"+image+"/manifests/")
	tags := make([]string, 0)
	for _, asset := range assets {
		prefix := "v2/" + image + "/manifests/"
		if !strings.HasPrefix(asset.Path, prefix) {
			continue
		}
		reference := strings.TrimPrefix(asset.Path, prefix)
		if !strings.HasPrefix(reference, "sha256:") {
			tags = append(tags, reference)
		}
	}
	body, _ := jsonBytes(map[string]any{"name": image, "tags": tags})
	return bytesResponse(http.StatusOK, "application/json", body, "", time.Time{}, request.HeadOnly), nil
}

func (s *Service) dockerCatalog(repository *store.Repository, request *Request) (*Response, error) {
	assets := s.store.ListAssets(repository.Name, "v2/")
	seen := map[string]struct{}{}
	repositories := make([]string, 0)
	for _, asset := range assets {
		kind, image, _, _ := parseDockerPath(asset.Path)
		if kind == "manifest" || kind == "tags" {
			if _, ok := seen[image]; !ok && image != "" {
				seen[image] = struct{}{}
				repositories = append(repositories, image)
			}
		}
	}
	body, _ := jsonBytes(map[string]any{"repositories": repositories})
	return bytesResponse(http.StatusOK, "application/json", body, "", time.Time{}, request.HeadOnly), nil
}

func (s *Service) dockerDelete(repository *store.Repository, image, reference string) (*Response, error) {
	deleted, err := s.store.DeleteAsset(repository.Name, "v2/"+image+"/manifests/"+reference)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, NotFound(reference)
	}
	return dockerEmpty(http.StatusAccepted), nil
}

func dockerPing(headOnly bool) (*Response, error) {
	header := make(http.Header)
	header.Set("Docker-Distribution-Api-Version", "registry/2.0")
	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", "2")
	response := &Response{Status: http.StatusOK, Header: header, Size: 2}
	if !headOnly {
		response.Body = io.NopCloser(strings.NewReader("{}"))
	}
	return response, nil
}

func dockerEmpty(status int) *Response {
	header := make(http.Header)
	header.Set("Docker-Distribution-Api-Version", "registry/2.0")
	header.Set("Content-Length", "0")
	return &Response{Status: status, Header: header}
}

func dockerImageParts(image string) (string, string) {
	parts := strings.Split(strings.Trim(image, "/"), "/")
	if len(parts) <= 1 {
		return "", image
	}
	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
}

func (s *Service) serveDockerProxy(repository *store.Repository, request *Request) (*Response, error) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return nil, MethodNotAllowed("Proxy repositories are read-only")
	}
	path := strings.TrimLeft(request.Path, "/")
	if path == "v2" || path == "v2/" {
		return dockerPing(request.HeadOnly)
	}
	kind, image, reference, _ := parseDockerPath(path)
	if kind == "tags" {
		// Tags are derived from cached manifests for simplicity.
		return s.dockerTags(repository, image, request)
	}
	if kind != "manifest" && kind != "blob" {
		return nil, NotFound(path)
	}
	cachePath := path
	if asset, ok := s.store.GetAsset(repository.Name, cachePath); ok {
		ttl := repository.MetadataMaxAge()
		if kind == "blob" {
			ttl = repository.ContentMaxAge()
		}
		if isFresh(asset, ttl, s.now()) {
			return s.serveDockerCached(asset, kind, request.HeadOnly)
		}
	}
	target := buildRemoteURL(remoteURL(repository), path)
	response, err := s.dockerFetch(repository, request, target, image, kind)
	if err != nil {
		return nil, BadUpstream("Docker upstream error: " + err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, NotFound(path)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &Error{Status: response.StatusCode, Message: fmt.Sprintf("Docker upstream returned %d", response.StatusCode)}
	}
	var tempFile string
	var size int64
	if kind == "blob" {
		file, err := os.CreateTemp(s.store.TempDir(), "docker-upstream-*")
		if err != nil {
			return nil, err
		}
		tempFile = file.Name()
		size, err = io.Copy(file, io.LimitReader(response.Body, maxGenericUploadBytes))
		file.Close()
		if err != nil {
			os.Remove(tempFile)
			return nil, err
		}
	} else {
		file, err := os.CreateTemp(s.store.TempDir(), "docker-manifest-*")
		if err != nil {
			return nil, err
		}
		tempFile = file.Name()
		size, err = io.Copy(file, io.LimitReader(response.Body, 32<<20))
		file.Close()
		if err != nil {
			os.Remove(tempFile)
			return nil, err
		}
	}
	defer os.Remove(tempFile)
	sum, size, _, err := s.store.SaveBlobFile(tempFile)
	if err != nil {
		return nil, err
	}
	now := s.now()
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	asset := &store.Asset{
		Repository: repository.Name, Format: "docker", Path: cachePath, Module: image, Version: reference,
		Kind: kind, ContentType: contentType, Size: size, SHA256: sum, Source: "proxy",
		ETag: response.Header.Get("ETag"), CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]string{"digest": response.Header.Get("Docker-Content-Digest")},
	}
	if err := s.store.PutAsset(asset, nil, true); err != nil {
		return nil, err
	}
	s.store.RecordProxySuccess(repository.Name, now)
	return s.serveDockerCached(asset, kind, request.HeadOnly)
}

func (s *Service) serveDockerCached(asset *store.Asset, kind string, headOnly bool) (*Response, error) {
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(asset.Path)
	}
	response := streamResponse(http.StatusOK, asset.ContentType, body, asset.Size, asset.ETag, asset.UpdatedAt, headOnly)
	if digest := asset.Attributes["digest"]; digest != "" {
		response.Header.Set("Docker-Content-Digest", digest)
	}
	return response, nil
}

func (s *Service) dockerFetch(repository *store.Repository, request *Request, target, image, kind string) (*http.Response, error) {
	client := &http.Client{Timeout: 30 * time.Minute}
	headers := http.Header{}
	applyUpstreamAuth(headers, repository)
	for _, name := range []string{"Accept"} {
		if values := request.Header.Values(name); len(values) > 0 {
			for _, value := range values {
				headers.Add(name, value)
			}
		}
	}
	response, err := dockerDo(client, target, headers)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusUnauthorized {
		return response, nil
	}
	challenge := response.Header.Get("WWW-Authenticate")
	response.Body.Close()
	token, err := dockerToken(client, challenge, image, repository)
	if err != nil {
		return nil, err
	}
	headers.Set("Authorization", "Bearer "+token)
	return dockerDo(client, target, headers)
}

func dockerDo(client *http.Client, target string, headers http.Header) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "kkrepo-go/0.2")
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	return client.Do(request)
}

func dockerToken(client *http.Client, challenge, image string, repository *store.Repository) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", fmt.Errorf("unsupported auth challenge: %s", challenge)
	}
	params := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(challenge, "Bearer "), ",") {
		part = strings.TrimSpace(part)
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		params[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("missing token realm")
	}
	if params["scope"] == "" {
		params["scope"] = "repository:" + image + ":pull"
	}
	query := url.Values{}
	query.Set("service", params["service"])
	query.Set("scope", params["scope"])
	target := realm + "?" + query.Encode()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	if repository.Proxy != nil && repository.Proxy.RemoteUsername != "" {
		request.SetBasicAuth(repository.Proxy.RemoteUsername, repository.Proxy.RemotePassword)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token service returned %d", response.StatusCode)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	return payload.AccessToken, nil
}
