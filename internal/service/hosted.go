package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"kkrepo-go/internal/gomod"
	"kkrepo-go/internal/store"
)

// Published describes a successful hosted module upload.
type Published struct {
	Module      string    `json:"module"`
	Version     string    `json:"version"`
	ArchivePath string    `json:"archivePath"`
	PublishedAt time.Time `json:"publishedAt"`
}

func (s *Service) serveHosted(repository *store.Repository, rawPath string, headOnly bool) (*Response, error) {
	path, err := parsePath(rawPath)
	if err != nil {
		return nil, err
	}
	switch path.Kind {
	case gomod.KindList:
		versions, err := s.hostedVersions(repository, path.Module)
		if err != nil {
			return nil, err
		}
		body := []byte(strings.Join(versions, "\n"))
		return bytesResponse(200, "text/plain", body, "", time.Time{}, headOnly), nil
	case gomod.KindLatest:
		return s.hostedLatest(repository, path, headOnly)
	default:
		return s.serveHostedAsset(repository, path, headOnly, path.Kind != gomod.KindInfo, "")
	}
}

func (s *Service) hostedVersions(repository *store.Repository, module string) ([]string, error) {
	versions := s.store.ComponentVersions(repository.Name, module)
	candidates := make([]string, 0, len(versions))
	for _, version := range versions {
		if gomod.IsCanonicalVersion(version) {
			candidates = append(candidates, version)
		}
	}
	if len(candidates) == 0 {
		return nil, NotFound(fmt.Sprintf("%s/@v/list", escapeModule(module)))
	}
	return gomod.ListVersions(candidates), nil
}

func (s *Service) hostedLatest(repository *store.Repository, path gomod.GoPath, headOnly bool) (*Response, error) {
	components := s.store.ListComponents(repository.Name, path.Module, "")
	candidates := make([]gomod.Candidate, 0, len(components))
	for _, component := range components {
		at := component.Time
		if at.IsZero() {
			at = component.UpdatedAt
		}
		candidates = append(candidates, gomod.Candidate{Version: component.Version, Time: at})
	}
	selected, ok := gomod.Latest(candidates)
	if !ok {
		return nil, NotFound(path.Path)
	}
	infoPath, err := gomod.VersionedPath(path.Module, selected.Version, gomod.KindInfo)
	if err != nil {
		return nil, NotFound(path.Path)
	}
	return s.serveHostedAsset(repository, infoPath, headOnly, true, "text/plain")
}

func (s *Service) serveHostedAsset(repository *store.Repository, path gomod.GoPath, headOnly, exposeValidators bool, contentTypeOverride string) (*Response, error) {
	asset, ok := s.store.GetAsset(repository.Name, path.Path)
	if !ok {
		return nil, NotFound(path.Path)
	}
	body, err := s.store.OpenBlob(asset)
	if err != nil {
		return nil, NotFound(path.Path)
	}
	contentType := path.ContentType()
	if contentTypeOverride != "" {
		contentType = contentTypeOverride
	}
	etag := ""
	var lastModified time.Time
	if exposeValidators {
		etag = asset.SHA256
		lastModified = asset.UpdatedAt
	}
	return streamResponse(200, contentType, body, asset.Size, etag, lastModified, headOnly), nil
}

func (s *Service) publishHosted(repository *store.Repository, rawUploadPath string, body io.Reader) (*Published, error) {
	if repository.Type != "hosted" {
		return nil, MethodNotAllowed("Operation is only valid on hosted Go repositories")
	}
	version, err := uploadVersion(rawUploadPath)
	if err != nil {
		return nil, err
	}
	writePolicy := "ALLOW"
	if repository.Hosted != nil && repository.Hosted.WritePolicy != "" {
		writePolicy = strings.ToUpper(repository.Hosted.WritePolicy)
	}
	if writePolicy == "DENY" {
		return nil, WritePolicyDenied("Write policy DENY forbids Go module upload")
	}
	archive, err := s.copyUpload(body)
	if err != nil {
		return nil, err
	}
	defer os.Remove(archive)

	inspected, err := gomod.InspectModuleArchive(archive, version)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	publishedAt := s.now().Truncate(time.Millisecond)
	modulePath, err := gomod.VersionedPath(inspected.Module, version, gomod.KindModule)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	infoPath, err := gomod.VersionedPath(inspected.Module, version, gomod.KindInfo)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	archivePath, err := gomod.VersionedPath(inspected.Module, version, gomod.KindPackage)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	infoBody, err := infoBytes(version, publishedAt)
	if err != nil {
		return nil, fmt.Errorf("encode module info: %w", err)
	}

	moduleSHA, moduleSize, _, err := s.store.SaveBlobBytes(inspected.GoMod)
	if err != nil {
		return nil, fmt.Errorf("store go.mod: %w", err)
	}
	infoSHA, infoSize, _, err := s.store.SaveBlobBytes(infoBody)
	if err != nil {
		return nil, fmt.Errorf("store info: %w", err)
	}
	archiveSHA, archiveSize, _, err := s.store.SaveBlobFile(archive)
	if err != nil {
		return nil, fmt.Errorf("store module archive: %w", err)
	}

	assets := []*store.Asset{
		newAsset(repository.Name, modulePath, "text/plain", moduleSHA, moduleSize, "hosted", publishedAt),
		newAsset(repository.Name, infoPath, "application/json", infoSHA, infoSize, "hosted", publishedAt),
		newAsset(repository.Name, archivePath, "application/zip", archiveSHA, archiveSize, "hosted", publishedAt),
	}
	component := &store.Component{
		Repository: repository.Name,
		Module:     inspected.Module,
		Version:    version,
		Kind:       "go-module",
		Time:       publishedAt,
		CreatedAt:  publishedAt,
		UpdatedAt:  publishedAt,
	}
	replace := writePolicy == "ALLOW"
	if err := s.store.PutRelease(assets, []*store.Component{component}, replace); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			return nil, WritePolicyDenied(fmt.Sprintf("Go module version already exists: %s %s", inspected.Module, version))
		}
		return nil, fmt.Errorf("persist Go release: %w", err)
	}
	return &Published{
		Module:      inspected.Module,
		Version:     version,
		ArchivePath: archivePath.Path,
		PublishedAt: publishedAt,
	}, nil
}

func (s *Service) copyUpload(body io.Reader) (string, error) {
	if body == nil {
		return "", BadRequest("Go module archive is required")
	}
	file, err := os.CreateTemp(s.store.TempDir(), "go-hosted-*.zip")
	if err != nil {
		return "", fmt.Errorf("create upload temp file: %w", err)
	}
	name := file.Name()
	size, copyErr := io.Copy(file, io.LimitReader(body, gomod.MaxCompressedBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(name)
		return "", fmt.Errorf("read Go module archive: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(name)
		return "", fmt.Errorf("close Go module archive: %w", closeErr)
	}
	if size > gomod.MaxCompressedBytes {
		os.Remove(name)
		return "", BadRequest("Go module archive exceeds 500 MiB")
	}
	if size == 0 {
		os.Remove(name)
		return "", BadRequest("Go module archive is empty")
	}
	return name, nil
}

func uploadVersion(rawUploadPath string) (string, error) {
	path := strings.TrimSpace(rawUploadPath)
	path = strings.TrimLeft(path, "/")
	if path == "" || strings.Contains(path, "/") || !strings.HasSuffix(path, ".zip") {
		return "", BadRequest("Go hosted upload path must be <version>.zip")
	}
	version, err := gomod.RequireCanonicalVersion(strings.TrimSuffix(path, ".zip"))
	if err != nil {
		return "", BadRequest(err.Error())
	}
	return version, nil
}

func newAsset(repository string, path gomod.GoPath, contentType, sha string, size int64, source string, at time.Time) *store.Asset {
	return &store.Asset{
		Repository:  repository,
		Path:        path.Path,
		Module:      path.Module,
		Version:     path.Version,
		Kind:        path.Kind.String(),
		ContentType: contentType,
		Size:        size,
		SHA256:      sha,
		Source:      source,
		CreatedAt:   at,
		UpdatedAt:   at,
	}
}

func escapeModule(module string) string {
	escaped, err := gomod.EscapeModulePath(module)
	if err != nil {
		return module
	}
	return escaped
}
