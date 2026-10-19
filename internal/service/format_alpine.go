package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

// serveAlpine generates APKINDEX.tar.gz for hosted repositories. Index signing
// is not implemented; the archive contains an unsigned APKINDEX.
func (s *Service) serveAlpine(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" && (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
		(path == "alpine.rsa.pub" || strings.HasSuffix(path, "/alpine.rsa.pub")) {
		if s.alpineSigner == nil {
			return nil, &Error{Status: http.StatusNotImplemented, Message: "Alpine signing key is not configured"}
		}
		return bytesResponse(http.StatusOK, "application/x-pem-file", s.alpineSigner.PublicPEM(), "", time.Time{}, request.HeadOnly), nil
	}
	if repository.Type == "hosted" && (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
		strings.HasSuffix(path, "APKINDEX.tar.gz") {
		prefix := strings.TrimSuffix(path, "APKINDEX.tar.gz")
		prefix = strings.TrimSuffix(prefix, "/")
		body := s.alpineIndex(repository, prefix)
		return bytesResponse(http.StatusOK, "application/gzip", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
	}
	return s.serveGeneric(repository, request)
}

func (s *Service) alpineIndex(repository *store.Repository, prefix string) []byte {
	unsigned := s.alpineUnsignedIndex(repository, prefix)
	if s.alpineSigner == nil {
		return unsigned
	}
	name, signature, err := s.alpineSigner.Sign(unsigned)
	if err != nil {
		return unsigned
	}
	fragment, err := alpineSignatureFragment(name, signature, time.Now())
	if err != nil {
		return unsigned
	}
	return append(fragment, unsigned...)
}

func (s *Service) alpineUnsignedIndex(repository *store.Repository, prefix string) []byte {
	assets := s.store.ListAssets(repository.Name, ".apk")
	directory := ""
	if prefix != "" {
		directory = prefix + "/"
	}
	var index strings.Builder
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Path, directory) || !strings.HasSuffix(asset.Path, ".apk") {
			continue
		}
		name, version, release, _, ok := parseAPKFilename(asset.Path)
		if !ok {
			continue
		}
		fmt.Fprintf(&index, "P:%s\n", name)
		fmt.Fprintf(&index, "V:%s-r%s\n", version, release)
		fmt.Fprintf(&index, "A:%s\n", alpineArch(prefix))
		fmt.Fprintf(&index, "S:%d\n", asset.Size)
		fmt.Fprintf(&index, "I:%d\n", asset.Size)
		fmt.Fprintf(&index, "T:%s\n", name)
		fmt.Fprintf(&index, "U:\nL:\n")
		fmt.Fprintf(&index, "o:%s\n", name)
		fmt.Fprintf(&index, "m:\nt:%d\nc:\nD:\np:\n\n", asset.UpdatedAt.Unix())
	}
	return alpineTarGz([]byte(index.String()), "kkrepo-go Alpine repository")
}

func parseAPKFilename(path string) (string, string, string, string, bool) {
	filename := path[strings.LastIndexByte(path, '/')+1:]
	base := strings.TrimSuffix(filename, ".apk")
	last := strings.LastIndexByte(base, '-')
	if last <= 0 {
		return "", "", "", "", false
	}
	release := strings.TrimPrefix(base[last+1:], "r")
	rest := base[:last]
	second := strings.LastIndexByte(rest, '-')
	if second <= 0 {
		return "", "", "", "", false
	}
	return rest[:second], rest[second+1:], release, base[last+1:], true
}

func alpineArch(prefix string) string {
	if index := strings.LastIndexByte(prefix, '/'); index >= 0 {
		return prefix[index+1:]
	}
	return prefix
}

func alpineTarGz(index []byte, description string) []byte {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	now := time.Now()
	for name, content := range map[string][]byte{
		"APKINDEX":    index,
		"DESCRIPTION": []byte(description + "\n"),
	} {
		_ = tarWriter.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)), ModTime: now,
		})
		_, _ = tarWriter.Write(content)
	}
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	return buffer.Bytes()
}

func alpineSignatureFragment(entryName string, signature []byte, at time.Time) ([]byte, error) {
	var tarBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&tarBuffer)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: entryName, Mode: 0o644, Size: int64(len(signature)), ModTime: at,
	}); err != nil {
		return nil, err
	}
	if _, err := tarWriter.Write(signature); err != nil {
		return nil, err
	}
	_ = tarWriter.Close()
	fragmentSize := 512 + ((len(signature)+511)/512)*512
	if tarBuffer.Len() < fragmentSize {
		return nil, fmt.Errorf("incomplete Alpine signature fragment")
	}
	var gzipBuffer bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&gzipBuffer, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := gzipWriter.Write(tarBuffer.Bytes()[:fragmentSize]); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return gzipBuffer.Bytes(), nil
}
