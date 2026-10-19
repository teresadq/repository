// Package upstream performs conditional HTTP fetches for proxy repositories.
package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Result is the outcome of an upstream request.
type Result struct {
	Status       int
	ETag         string
	LastModified time.Time
	ContentType  string
	Body         []byte
	TempFile     string
	Size         int64
}

// Fetcher performs metadata and content requests with separate timeout budgets.
type Fetcher struct {
	metadata *http.Client
	content  *http.Client
}

// New builds a fetcher with sensible Go module proxy timeouts.
func New() *Fetcher {
	return &Fetcher{
		metadata: &http.Client{Timeout: 60 * time.Second},
		content:  &http.Client{Timeout: 30 * time.Minute},
	}
}

// FetchBytes retrieves a bounded metadata document.
func (f *Fetcher) FetchBytes(ctx context.Context, url, etag string, lastModified time.Time, maxBytes int64) (*Result, error) {
	return f.FetchBytesHeaders(ctx, url, etag, lastModified, maxBytes, nil)
}

// FetchBytesHeaders retrieves a bounded document with additional headers.
func (f *Fetcher) FetchBytesHeaders(ctx context.Context, url, etag string, lastModified time.Time, maxBytes int64, headers http.Header) (*Result, error) {
	response, err := f.do(ctx, f.metadata, url, etag, lastModified, headers)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	result := &Result{
		Status:       response.StatusCode,
		ETag:         response.Header.Get("ETag"),
		LastModified: parseTime(response.Header.Get("Last-Modified")),
		ContentType:  response.Header.Get("Content-Type"),
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, nil
	}
	limited := io.LimitReader(response.Body, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("upstream response exceeds %d bytes", maxBytes)
	}
	result.Body = body
	result.Size = int64(len(body))
	return result, nil
}

// FetchFile streams a large content response to a temporary file. The caller
// must remove Result.TempFile.
func (f *Fetcher) FetchFile(ctx context.Context, url, etag string, lastModified time.Time, maxBytes int64, tempDir string) (*Result, error) {
	return f.FetchFileHeaders(ctx, url, etag, lastModified, maxBytes, tempDir, nil)
}

// FetchFileHeaders streams a large response with additional request headers.
func (f *Fetcher) FetchFileHeaders(ctx context.Context, url, etag string, lastModified time.Time, maxBytes int64, tempDir string, headers http.Header) (*Result, error) {
	response, err := f.do(ctx, f.content, url, etag, lastModified, headers)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	result := &Result{
		Status:       response.StatusCode,
		ETag:         response.Header.Get("ETag"),
		LastModified: parseTime(response.Header.Get("Last-Modified")),
		ContentType:  response.Header.Get("Content-Type"),
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, nil
	}
	file, err := os.CreateTemp(tempDir, "upstream-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create upstream temp file: %w", err)
	}
	name := file.Name()
	limited := io.LimitReader(response.Body, maxBytes+1)
	size, copyErr := io.Copy(file, limited)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(name)
		return nil, fmt.Errorf("read upstream content: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(name)
		return nil, fmt.Errorf("close upstream temp file: %w", closeErr)
	}
	if size > maxBytes {
		os.Remove(name)
		return nil, fmt.Errorf("upstream content exceeds %d bytes", maxBytes)
	}
	result.TempFile = name
	result.Size = size
	return result, nil
}

func (f *Fetcher) do(ctx context.Context, client *http.Client, url, etag string, lastModified time.Time, headers http.Header) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	request.Header.Set("User-Agent", "kkrepo-go/0.2 (+artifact repository)")
	for _, name := range []string{"Accept", "Accept-Encoding", "Authorization", "X-Npm-Token", "X-Checksum-Sha1"} {
		if values := headers.Values(name); len(values) > 0 {
			for _, value := range values {
				request.Header.Add(name, value)
			}
		}
	}
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	if !lastModified.IsZero() {
		request.Header.Set("If-Modified-Since", lastModified.UTC().Format(http.TimeFormat))
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	return response, nil
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := http.ParseTime(value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}
