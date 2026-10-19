package store

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	backupFormat          = "kkrepo-go-backup"
	backupVersion         = 1
	maxBackupStateBytes   = 512 << 20
	maxBackupBlobBytes    = 8 << 30
	maxBackupManifestSize = 1 << 20
)

// BackupManifest describes an exported archive.
type BackupManifest struct {
	Format       string    `json:"format"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	StateVersion int       `json:"stateVersion"`
	Repositories int       `json:"repositories"`
	Assets       int       `json:"assets"`
	Blobs        int       `json:"blobs"`
}

// ImportOptions controls how an archive is applied.
type ImportOptions struct {
	Mode  string // replace or merge
	Force bool   // required for replace
}

// ImportResult summarizes an import.
type ImportResult struct {
	Mode         string `json:"mode"`
	Repositories int    `json:"repositories"`
	Skipped      int    `json:"skipped"`
	Assets       int    `json:"assets"`
	Blobs        int    `json:"blobs"`
	StateVersion int    `json:"stateVersion"`
}

// Export writes a consistent tar.gz archive of metadata and referenced blobs.
// Writers are blocked for the duration so the state and blob set cannot change.
func (s *Store) Export(w io.Writer) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stateRaw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	shas := make([]string, 0)
	seen := map[string]struct{}{}
	for _, assets := range s.state.Assets {
		for _, asset := range assets {
			if asset.SHA256 == "" {
				continue
			}
			if _, ok := seen[asset.SHA256]; ok {
				continue
			}
			seen[asset.SHA256] = struct{}{}
			shas = append(shas, asset.SHA256)
		}
	}
	sort.Strings(shas)
	manifest := BackupManifest{
		Format: backupFormat, Version: backupVersion, CreatedAt: time.Now().UTC(),
		StateVersion: s.state.Version, Repositories: len(s.state.Repositories),
		Assets: len(seen), Blobs: len(shas),
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	gzipWriter := gzip.NewWriter(w)
	tarWriter := tar.NewWriter(gzipWriter)
	writeTar := func(name string, content []byte) error {
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)), ModTime: time.Now().UTC(),
		}); err != nil {
			return err
		}
		_, err := tarWriter.Write(content)
		return err
	}
	if err := writeTar("manifest.json", manifestRaw); err != nil {
		return err
	}
	if err := writeTar("state.json", stateRaw); err != nil {
		return err
	}
	for _, sum := range shas {
		file, err := os.Open(s.blobPath(sum))
		if err != nil {
			return fmt.Errorf("open blob %s: %w", sum, err)
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		header := &tar.Header{
			Name: "blobs/" + sum, Mode: 0o644, Size: info.Size(), ModTime: info.ModTime(),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			file.Close()
			return err
		}
		if _, err := io.Copy(tarWriter, file); err != nil {
			file.Close()
			return err
		}
		file.Close()
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

// ImportArchive restores an archive created by Export.
func (s *Store) ImportArchive(reader io.Reader, options ImportOptions) (*ImportResult, error) {
	mode := options.Mode
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "merge" {
		return nil, fmt.Errorf("import mode must be replace or merge")
	}
	if mode == "replace" && !options.Force {
		return nil, fmt.Errorf("replace import requires force=true")
	}
	tempDir, err := os.MkdirTemp(filepath.Join(s.root, "tmp"), "import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	if err := extractBackup(reader, tempDir); err != nil {
		return nil, err
	}
	manifestRaw, err := os.ReadFile(filepath.Join(tempDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("backup manifest is missing")
	}
	var manifest BackupManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil || manifest.Format != backupFormat {
		return nil, fmt.Errorf("invalid backup manifest")
	}
	if manifest.Version > backupVersion {
		return nil, fmt.Errorf("unsupported backup version %d", manifest.Version)
	}
	stateRaw, err := os.ReadFile(filepath.Join(tempDir, "state.json"))
	if err != nil {
		return nil, fmt.Errorf("backup state.json is missing")
	}
	if len(stateRaw) > maxBackupStateBytes {
		return nil, fmt.Errorf("backup state.json is too large")
	}
	var imported state
	if err := json.Unmarshal(stateRaw, &imported); err != nil {
		return nil, fmt.Errorf("invalid backup state: %w", err)
	}
	if imported.Version > stateVersion {
		return nil, fmt.Errorf("backup state version %d is newer than this build", imported.Version)
	}

	// Copy and verify blobs first; state is only replaced after all content is ready.
	blobCount, err := s.importBlobs(filepath.Join(tempDir, "blobs"))
	if err != nil {
		return nil, err
	}

	if mode == "replace" {
		for repoAssets := range imported.Assets {
			for assetPath, asset := range imported.Assets[repoAssets] {
				if _, err := os.Stat(s.blobPath(asset.SHA256)); err != nil {
					return nil, fmt.Errorf("imported asset references missing blob: %s", assetPath)
				}
			}
		}
	}
	result := &ImportResult{Mode: mode, StateVersion: imported.Version, Blobs: blobCount}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == "replace" {
		if err := s.backupCurrentStateLocked(); err != nil {
			return nil, err
		}
		normalizeImportedState(&imported)
		s.state = imported
		s.negative = map[string]time.Time{}
		result.Repositories = len(imported.Repositories)
		for _, assets := range imported.Assets {
			result.Assets += len(assets)
		}
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		return result, nil
	}

	// Merge: only repositories/assets/components/proxy states are imported;
	// users, roles, tokens, audit and security settings stay local.
	if s.state.Repositories == nil {
		s.state.Repositories = map[string]*Repository{}
	}
	if s.state.Assets == nil {
		s.state.Assets = map[string]map[string]*Asset{}
	}
	if s.state.Components == nil {
		s.state.Components = map[string]map[string]map[string]*Component{}
	}
	normalizeImportedState(&imported)
	for name, repo := range imported.Repositories {
		if _, exists := s.state.Repositories[name]; exists {
			result.Skipped++
			continue
		}
		s.state.Repositories[name] = cloneRepository(repo)
		s.state.Assets[name] = map[string]*Asset{}
		s.state.Components[name] = map[string]map[string]*Component{}
		for assetPath, asset := range imported.Assets[name] {
			if _, err := os.Stat(s.blobPath(asset.SHA256)); err != nil {
				return nil, fmt.Errorf("imported asset references missing blob: %s", assetPath)
			}
			s.state.Assets[name][assetPath] = cloneAsset(asset)
			result.Assets++
		}
		for module, versions := range imported.Components[name] {
			s.state.Components[name][module] = map[string]*Component{}
			for version, component := range versions {
				copied := *component
				s.state.Components[name][module][version] = &copied
			}
		}
		if state := imported.ProxyStates[name]; state != nil {
			if s.state.ProxyStates == nil {
				s.state.ProxyStates = map[string]*ProxyState{}
			}
			copied := *state
			s.state.ProxyStates[name] = &copied
		}
		result.Repositories++
	}
	return result, s.persistLocked()
}

// HasRepositoryData reports whether the store contains non-empty repository data.
func (s *Store) HasRepositoryData() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, assets := range s.state.Assets {
		if len(assets) > 0 {
			return true
		}
	}
	return false
}

func (s *Store) backupCurrentStateLocked() error {
	if _, err := os.Stat(filepath.Join(s.root, "state.json")); err != nil {
		return nil
	}
	timestamp := time.Now().UTC().Format("20060102-150405")
	source := filepath.Join(s.root, "state.json")
	target := filepath.Join(s.root, "state.json.bak-"+timestamp)
	return copyFile(source, target)
}

func copyFile(source, target string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	temp, err := os.CreateTemp(filepath.Dir(target), "state-bak-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := io.Copy(temp, file); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, target)
}

func (s *Store) importBlobs(directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sum := entry.Name()
		if len(sum) != 64 || !isHexString(sum) {
			return 0, fmt.Errorf("invalid blob name in backup: %s", sum)
		}
		if _, err := os.Stat(s.blobPath(sum)); err == nil {
			count++
			continue
		}
		file, err := os.Open(filepath.Join(directory, sum))
		if err != nil {
			return 0, err
		}
		hasher := sha256.New()
		temp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "import-blob-*.tmp")
		if err != nil {
			file.Close()
			return 0, err
		}
		tempName := temp.Name()
		size, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(file, maxBackupBlobBytes+1))
		file.Close()
		closeErr := temp.Close()
		if copyErr != nil {
			os.Remove(tempName)
			return 0, copyErr
		}
		if closeErr != nil {
			os.Remove(tempName)
			return 0, closeErr
		}
		if size > maxBackupBlobBytes {
			os.Remove(tempName)
			return 0, fmt.Errorf("backup blob is too large: %s", sum)
		}
		actual := hex.EncodeToString(hasher.Sum(nil))
		if actual != sum {
			os.Remove(tempName)
			return 0, fmt.Errorf("backup blob checksum mismatch: %s", sum)
		}
		target := s.blobPath(sum)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			os.Remove(tempName)
			return 0, err
		}
		if err := os.Rename(tempName, target); err != nil {
			os.Remove(tempName)
			return 0, err
		}
		count++
	}
	return count, nil
}

func extractBackup(reader io.Reader, targetDir string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("backup is not a gzip archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(header.Name)
		if name == "." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("unsafe path in backup: %s", header.Name)
		}
		if name != "manifest.json" && name != "state.json" && !strings.HasPrefix(name, "blobs/") {
			return fmt.Errorf("unexpected entry in backup: %s", name)
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("unsupported entry type in backup: %s", name)
		}
		limit := int64(maxBackupBlobBytes)
		if name == "manifest.json" {
			limit = maxBackupManifestSize
		} else if name == "state.json" {
			limit = maxBackupStateBytes
		}
		target := filepath.Join(targetDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		file, err := os.Create(target)
		if err != nil {
			return err
		}
		size, copyErr := io.Copy(file, io.LimitReader(tarReader, limit+1))
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if size > limit {
			return fmt.Errorf("backup entry is too large: %s", name)
		}
	}
	return nil
}

func normalizeImportedState(imported *state) {
	if imported.Version == 0 {
		imported.Version = stateVersion
	}
	if imported.Repositories == nil {
		imported.Repositories = map[string]*Repository{}
	}
	if imported.Assets == nil {
		imported.Assets = map[string]map[string]*Asset{}
	}
	if imported.Components == nil {
		imported.Components = map[string]map[string]map[string]*Component{}
	}
	if imported.ProxyStates == nil {
		imported.ProxyStates = map[string]*ProxyState{}
	}
	if imported.Users == nil {
		imported.Users = map[string]*User{}
	}
	if imported.Roles == nil {
		imported.Roles = map[string]*Role{}
	}
	if imported.Groups == nil {
		imported.Groups = map[string]*Group{}
	}
	if imported.Tokens == nil {
		imported.Tokens = map[string]*Token{}
	}
	if imported.CleanupPolicies == nil {
		imported.CleanupPolicies = map[string]*CleanupPolicy{}
	}
	if imported.ContentSelectors == nil {
		imported.ContentSelectors = map[string]*ContentSelector{}
	}
	if imported.Security.AnonymousRoles == nil {
		imported.Security.AnonymousRoles = []string{"nx-anonymous"}
	}
	if imported.Version < stateVersion {
		imported.Version = stateVersion
		imported.Security.AnonymousEnabled = true
	}
}

func isHexString(value string) bool {
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return value != ""
}
