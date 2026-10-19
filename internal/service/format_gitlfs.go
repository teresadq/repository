package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type lfsBatchRequest struct {
	Operation string   `json:"operation"`
	Transfers []string `json:"transfers,omitempty"`
	Objects   []struct {
		OID  string `json:"oid"`
		Size int64  `json:"size"`
	} `json:"objects"`
}

type lfsAction struct {
	Href      string            `json:"href"`
	Header    map[string]string `json:"header,omitempty"`
	ExpiresAt string            `json:"expires_at,omitempty"`
}

type lfsObject struct {
	OID     string               `json:"oid"`
	Size    int64                `json:"size,omitempty"`
	Actions map[string]lfsAction `json:"actions,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *Service) serveGitLFS(repository *store.Repository, request *Request) (*Response, error) {
	if repository.Type != "hosted" {
		return nil, MethodNotAllowed("Git LFS supports hosted repositories only")
	}
	path := strings.TrimLeft(request.Path, "/")
	switch {
	case path == "info/lfs/objects/batch" && request.Method == http.MethodPost:
		return s.gitLFSBatch(repository, request)
	case strings.HasPrefix(path, "info/lfs/locks"):
		return nil, &Error{Status: http.StatusNotImplemented, Message: "Git LFS locking is not supported"}
	case strings.HasSuffix(path, "/verify") && request.Method == http.MethodPost:
		return bytesResponse(http.StatusOK, "application/vnd.git-lfs+json", []byte("{}"), "", time.Time{}, false), nil
	case request.Method == http.MethodPut:
		return s.gitLFSUpload(repository, request, path)
	case request.Method == http.MethodGet || request.Method == http.MethodHead:
		return s.serveGitLFSBlob(repository, request, path)
	}
	return nil, MethodNotAllowed("Unsupported Git LFS method")
}

func (s *Service) gitLFSBatch(repository *store.Repository, request *Request) (*Response, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, 32<<20))
	if err != nil {
		return nil, BadRequest("Unable to read Git LFS batch request")
	}
	var batch lfsBatchRequest
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, BadRequest("Invalid Git LFS batch request")
	}
	if len(batch.Objects) > 1000 {
		return nil, BadRequest("Git LFS batch is limited to 1000 objects")
	}
	if batch.Operation != "download" && batch.Operation != "upload" {
		return nil, BadRequest("Git LFS operation must be download or upload")
	}
	base := request.BaseURL + "/repository/" + repository.Name
	now := s.now()
	objects := make([]lfsObject, 0, len(batch.Objects))
	for _, object := range batch.Objects {
		item := lfsObject{OID: object.OID, Size: object.Size}
		if len(object.OID) != 64 || !isHex(object.OID) {
			item.Error = &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{Code: 422, Message: "OID must be a 64 character SHA-256"}
			objects = append(objects, item)
			continue
		}
		asset, exists := s.store.GetAsset(repository.Name, object.OID)
		if batch.Operation == "download" {
			if !exists {
				item.Error = &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{Code: 404, Message: "Object not found"}
			} else {
				item.Size = asset.Size
				item.Actions = map[string]lfsAction{
					"download": {Href: base + "/" + object.OID, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
				}
			}
		} else {
			if exists {
				item.Actions = map[string]lfsAction{
					"download": {Href: base + "/" + object.OID, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
				}
			} else {
				item.Actions = map[string]lfsAction{
					"upload": {Href: base + "/" + object.OID, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
				}
				item.Actions["verify"] = lfsAction{Href: base + "/" + object.OID + "/verify"}
			}
		}
		objects = append(objects, item)
	}
	response := map[string]any{"transfer": "basic", "objects": objects}
	encoded, _ := json.Marshal(response)
	return bytesResponse(http.StatusOK, "application/vnd.git-lfs+json", encoded, "", time.Time{}, false), nil
}

func (s *Service) gitLFSUpload(repository *store.Repository, request *Request, path string) (*Response, error) {
	oid := strings.Trim(path, "/")
	if len(oid) != 64 || !isHex(oid) {
		return nil, BadRequest("Git LFS OID must be a 64 character SHA-256")
	}
	sum, size, _, err := s.store.SaveBlob(request.Body, maxGenericUploadBytes)
	if err != nil {
		return nil, err
	}
	if sum != strings.ToLower(oid) {
		return nil, BadRequest("Git LFS content digest does not match OID")
	}
	now := s.now()
	asset := &store.Asset{
		Repository: repository.Name, Format: "gitlfs", Path: strings.ToLower(oid), Module: strings.ToLower(oid),
		Kind: "lfs-object", ContentType: "application/octet-stream", Size: size, SHA256: sum,
		Source: "hosted", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.PutAsset(asset, nil, true); err != nil {
		return nil, err
	}
	return bytesResponse(http.StatusOK, "application/vnd.git-lfs+json", []byte("{}"), "", time.Time{}, false), nil
}

func (s *Service) serveGitLFSBlob(repository *store.Repository, request *Request, path string) (*Response, error) {
	oid := strings.ToLower(strings.Trim(path, "/"))
	asset, ok := s.store.GetAsset(repository.Name, oid)
	if !ok {
		return nil, NotFound(path)
	}
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(path)
	}
	file, isFile := body.(*os.File)
	if request.Header.Get("Range") == "" || !isFile {
		return streamResponse(http.StatusOK, asset.ContentType, body, asset.Size, asset.SHA256, asset.UpdatedAt, request.HeadOnly), nil
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
	header.Set("Content-Type", asset.ContentType)
	header.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, asset.Size))
	header.Set("ETag", asset.SHA256)
	header.Set("Accept-Ranges", "bytes")
	if request.HeadOnly {
		body.Close()
		return &Response{Status: http.StatusPartialContent, Header: header, Size: end - start + 1}, nil
	}
	return &Response{Status: http.StatusPartialContent, Header: header, Size: end - start + 1, Body: sectionCloser{SectionReader: section, file: file}}, nil
}

type sectionCloser struct {
	*io.SectionReader
	file *os.File
}

func (s sectionCloser) Close() error { return s.file.Close() }

func parseRange(value string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(value, "bytes=") {
		return 0, 0, false
	}
	value = strings.TrimPrefix(value, "bytes=")
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	if parts[0] == "" {
		length, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || length <= 0 {
			return 0, 0, false
		}
		if length > size {
			length = size
		}
		return size - length, size - 1, true
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if parts[1] != "" {
		parsed, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || parsed < start {
			return 0, 0, false
		}
		if parsed < end {
			end = parsed
		}
	}
	return start, end, true
}

func isHex(value string) bool {
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F') {
			return false
		}
	}
	return value != ""
}
