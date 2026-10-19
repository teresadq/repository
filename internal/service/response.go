package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"kkrepo-go/internal/store"
)

// Error is an HTTP-aware service error.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// NotFound reports a missing repository, module version or upstream artifact.
func NotFound(message string) *Error {
	if message == "" {
		message = "not found"
	}
	return &Error{Status: http.StatusNotFound, Message: message}
}

// BadRequest reports an invalid client request.
func BadRequest(message string) *Error {
	return &Error{Status: http.StatusBadRequest, Message: message}
}

// BadUpstream reports an unreachable or failing upstream proxy.
func BadUpstream(message string) *Error {
	return &Error{Status: http.StatusBadGateway, Message: message}
}

// MethodNotAllowed reports an operation that does not fit the repository type.
func MethodNotAllowed(message string) *Error {
	return &Error{Status: http.StatusMethodNotAllowed, Message: message}
}

// WritePolicyDenied reports a write rejected by the repository write policy.
func WritePolicyDenied(message string) *Error {
	return &Error{Status: http.StatusForbidden, Message: message}
}

// Response is a protocol response with a closable body.
type Response struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
	Size   int64
}

// Close releases the response body.
func (r *Response) Close() {
	if r != nil && r.Body != nil {
		_ = r.Body.Close()
	}
}

// ReadBounded reads the response body, rejecting bodies over limit bytes.
func (r *Response) ReadBounded(limit int64) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

func bytesResponse(status int, contentType string, body []byte, etag string, lastModified time.Time, headOnly bool) *Response {
	header := make(http.Header)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	if etag != "" {
		header.Set("ETag", etag)
	}
	if !lastModified.IsZero() {
		header.Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	}
	response := &Response{Status: status, Header: header, Size: int64(len(body))}
	if !headOnly {
		response.Body = io.NopCloser(&byteReader{data: body})
	}
	return response
}

func streamResponse(status int, contentType string, body io.ReadCloser, size int64, etag string, lastModified time.Time, headOnly bool) *Response {
	header := make(http.Header)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", fmt.Sprintf("%d", size))
	if etag != "" {
		header.Set("ETag", etag)
	}
	if !lastModified.IsZero() {
		header.Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	}
	response := &Response{Status: status, Header: header, Size: size}
	if headOnly {
		_ = body.Close()
	} else {
		response.Body = body
	}
	return response
}

// byteReader avoids importing bytes just to expose a reader.
type byteReader struct {
	data []byte
}

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func jsonBytes(value any) ([]byte, error) {
	return json.Marshal(value)
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// InfoDocument is the GOPROXY .info document.
type InfoDocument struct {
	Version string `json:"Version"`
	Time    string `json:"Time"`
}

func infoBytes(version string, at time.Time) ([]byte, error) {
	return json.Marshal(InfoDocument{Version: version, Time: at.UTC().Format(time.RFC3339Nano)})
}

func parseInfo(body []byte) (string, time.Time, error) {
	var document InfoDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return "", time.Time{}, err
	}
	if document.Version == "" || document.Time == "" {
		return "", time.Time{}, fmt.Errorf("missing Go version info field")
	}
	at, err := time.Parse(time.RFC3339, document.Time)
	if err != nil {
		return "", time.Time{}, err
	}
	return document.Version, at, nil
}

func assetValidators(asset *store.Asset, expose bool) (string, time.Time) {
	if !expose || asset == nil {
		return "", time.Time{}
	}
	lastModified := asset.UpdatedAt
	if asset.LastModified != "" {
		if parsed, err := time.Parse(time.RFC3339, asset.LastModified); err == nil {
			lastModified = parsed
		}
	}
	return asset.ETag, lastModified
}
