// Package store provides the persistent metadata and blob storage for
// kkrepo-go. Metadata is kept in an atomically replaced JSON document and
// blobs are content-addressed files on disk. This keeps the first milestone
// dependency-free and single-node; the Store API is the seam where a
// SQL-backed implementation can be added later.
package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotFound is returned when a repository, asset or component is absent.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists is returned when a repository or release already exists.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalid indicates invalid metadata supplied by a caller.
	ErrInvalid = errors.New("invalid")
	// ErrQuota indicates a repository storage quota would be exceeded.
	ErrQuota = errors.New("storage quota exceeded")
)

const stateVersion = 2

// Repository is a Go module repository (hosted, proxy or group).
type Repository struct {
	Name              string          `json:"name"`
	Recipe            string          `json:"recipe"`
	Format            string          `json:"format"`
	Type              string          `json:"type"`
	Online            bool            `json:"online"`
	Description       string          `json:"description,omitempty"`
	Hosted            *HostedSettings `json:"hosted,omitempty"`
	Proxy             *ProxySettings  `json:"proxy,omitempty"`
	Group             *GroupSettings  `json:"group,omitempty"`
	IncludePatterns   []string        `json:"includePatterns,omitempty"`
	ExcludePatterns   []string        `json:"excludePatterns,omitempty"`
	StorageQuotaBytes int64           `json:"storageQuotaBytes,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

// HostedSettings configures write behavior for hosted repositories.
type HostedSettings struct {
	WritePolicy   string `json:"writePolicy"`
	VersionPolicy string `json:"versionPolicy,omitempty"`
	LayoutPolicy  string `json:"layoutPolicy,omitempty"`
}

// ProxySettings configures upstream access and cache TTLs.
type ProxySettings struct {
	RemoteURL             string `json:"remoteUrl"`
	ContentMaxAgeMinutes  int    `json:"contentMaxAgeMinutes"`
	MetadataMaxAgeMinutes int    `json:"metadataMaxAgeMinutes"`
	AutoBlock             bool   `json:"autoBlock"`
	RemoteUsername        string `json:"remoteUsername,omitempty"`
	RemotePassword        string `json:"remotePassword,omitempty"`
	RemoteBearerToken     string `json:"remoteBearerToken,omitempty"`
}

// GroupSettings lists member repositories in resolution order.
type GroupSettings struct {
	MemberNames []string `json:"memberNames"`
}

// ContentMaxAge returns the configured content TTL, defaulting to 1440 minutes.
func (r *Repository) ContentMaxAge() int {
	if r.Proxy == nil || r.Proxy.ContentMaxAgeMinutes == 0 {
		return 1440
	}
	return r.Proxy.ContentMaxAgeMinutes
}

// MetadataMaxAge returns the configured metadata TTL, defaulting to 1440 minutes.
func (r *Repository) MetadataMaxAge() int {
	if r.Proxy == nil || r.Proxy.MetadataMaxAgeMinutes == 0 {
		return 1440
	}
	return r.Proxy.MetadataMaxAgeMinutes
}

// AutoBlock reports whether upstream failures should open the circuit.
func (r *Repository) AutoBlock() bool {
	return r.Proxy == nil || r.Proxy.AutoBlock
}

// MemberNames returns the group members, if any.
func (r *Repository) MemberNames() []string {
	if r.Group == nil {
		return nil
	}
	return r.Group.MemberNames
}

// Asset is a cached or published repository artifact. Module is the generic
// display name of the owning component (package, chart, image, file, ...).
type Asset struct {
	Repository       string            `json:"repository"`
	Format           string            `json:"format,omitempty"`
	Path             string            `json:"path"`
	Namespace        string            `json:"namespace,omitempty"`
	Module           string            `json:"module"`
	Version          string            `json:"version,omitempty"`
	Kind             string            `json:"kind"`
	ContentType      string            `json:"contentType"`
	Size             int64             `json:"size"`
	SHA256           string            `json:"sha256"`
	Source           string            `json:"source"`
	ETag             string            `json:"etag,omitempty"`
	LastModified     string            `json:"lastModified,omitempty"`
	Attributes       map[string]string `json:"attributes,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	LastDownloadedAt time.Time         `json:"lastDownloadedAt,omitempty"`
}

// Component is one version of a package, chart, image or other logical unit.
type Component struct {
	Repository string            `json:"repository"`
	Format     string            `json:"format,omitempty"`
	Namespace  string            `json:"namespace,omitempty"`
	Module     string            `json:"module"`
	Version    string            `json:"version"`
	Kind       string            `json:"kind"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Time       time.Time         `json:"time"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// ProxyState tracks upstream health for a proxy repository.
type ProxyState struct {
	FailCount     int       `json:"failCount"`
	BlockedUntil  time.Time `json:"blockedUntil"`
	LastError     string    `json:"lastError,omitempty"`
	LastErrorAt   time.Time `json:"lastErrorAt"`
	LastSuccessAt time.Time `json:"lastSuccessAt"`
}

type state struct {
	Version          int                                         `json:"version"`
	Repositories     map[string]*Repository                      `json:"repositories"`
	Assets           map[string]map[string]*Asset                `json:"assets"`
	Components       map[string]map[string]map[string]*Component `json:"components"`
	ProxyStates      map[string]*ProxyState                      `json:"proxyStates"`
	Users            map[string]*User                            `json:"users"`
	Roles            map[string]*Role                            `json:"roles"`
	Groups           map[string]*Group                           `json:"groups"`
	Tokens           map[string]*Token                           `json:"tokens"`
	Security         SecuritySettings                            `json:"security"`
	Audit            []AuditEntry                                `json:"audit"`
	AuditSeq         int64                                       `json:"auditSeq"`
	CleanupPolicies  map[string]*CleanupPolicy                   `json:"cleanupPolicies"`
	CleanupRuns      []CleanupRun                                `json:"cleanupRuns"`
	ContentSelectors map[string]*ContentSelector                 `json:"contentSelectors"`
}

// Store is a concurrency-safe metadata and blob store.
type Store struct {
	mu    sync.RWMutex
	root  string
	state state

	negMu    sync.Mutex
	negative map[string]time.Time
}

// New opens or creates a store rooted at root and seeds default repositories
// on first start.
func New(root string) (*Store, error) {
	for _, dir := range []string{root, filepath.Join(root, "blobs"), filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}
	s := &Store{root: root, negative: make(map[string]time.Time)}
	statePath := filepath.Join(root, "state.json")
	raw, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		s.state = seedState()
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	s.normalizeLocked()
	return s, nil
}

func seedState() state {
	now := time.Now().UTC()
	repos := map[string]*Repository{
		"go-proxy": {
			Name:        "go-proxy",
			Recipe:      "go-proxy",
			Format:      "go",
			Type:        "proxy",
			Online:      true,
			Description: "Go module proxy for proxy.golang.org",
			Proxy: &ProxySettings{
				RemoteURL:             "https://proxy.golang.org",
				ContentMaxAgeMinutes:  1440,
				MetadataMaxAgeMinutes: 1440,
				AutoBlock:             true,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		"go-hosted": {
			Name:        "go-hosted",
			Recipe:      "go-hosted",
			Format:      "go",
			Type:        "hosted",
			Online:      true,
			Description: "Hosted Go modules published through kkrepo-go",
			Hosted:      &HostedSettings{WritePolicy: "ALLOW"},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		"go-group": {
			Name:        "go-group",
			Recipe:      "go-group",
			Format:      "go",
			Type:        "group",
			Online:      true,
			Description: "Hosted modules first, then the public Go proxy",
			Group:       &GroupSettings{MemberNames: []string{"go-hosted", "go-proxy"}},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		"maven-central": {
			Name: "maven-central", Recipe: "maven-proxy", Format: "maven", Type: "proxy", Online: true,
			Description: "Maven Central proxy",
			Proxy:       &ProxySettings{RemoteURL: "https://repo1.maven.org/maven2", ContentMaxAgeMinutes: 1440, MetadataMaxAgeMinutes: 1440, AutoBlock: true},
			CreatedAt:   now, UpdatedAt: now,
		},
		"npmjs": {
			Name: "npmjs", Recipe: "npm-proxy", Format: "npm", Type: "proxy", Online: true,
			Description: "npm public registry proxy",
			Proxy:       &ProxySettings{RemoteURL: "https://registry.npmjs.org", ContentMaxAgeMinutes: 1440, MetadataMaxAgeMinutes: 1440, AutoBlock: true},
			CreatedAt:   now, UpdatedAt: now,
		},
		"pypi-proxy": {
			Name: "pypi-proxy", Recipe: "pypi-proxy", Format: "pypi", Type: "proxy", Online: true,
			Description: "PyPI public index proxy",
			Proxy:       &ProxySettings{RemoteURL: "https://pypi.org", ContentMaxAgeMinutes: 1440, MetadataMaxAgeMinutes: 1440, AutoBlock: true},
			CreatedAt:   now, UpdatedAt: now,
		},
		"docker-hub": {
			Name: "docker-hub", Recipe: "docker-proxy", Format: "docker", Type: "proxy", Online: true,
			Description: "Docker Hub registry proxy",
			Proxy:       &ProxySettings{RemoteURL: "https://registry-1.docker.io", ContentMaxAgeMinutes: 1440, MetadataMaxAgeMinutes: 1440, AutoBlock: true},
			CreatedAt:   now, UpdatedAt: now,
		},
		"raw-hosted": {
			Name: "raw-hosted", Recipe: "raw-hosted", Format: "raw", Type: "hosted", Online: true,
			Description: "Generic file repository",
			Hosted:      &HostedSettings{WritePolicy: "ALLOW"},
			CreatedAt:   now, UpdatedAt: now,
		},
	}
	nowRole := now
	return state{
		Version:      stateVersion,
		Repositories: repos,
		Assets:       map[string]map[string]*Asset{},
		Components:   map[string]map[string]map[string]*Component{},
		ProxyStates:  map[string]*ProxyState{},
		Users:        map[string]*User{},
		Roles: map[string]*Role{
			"nx-admin": {
				Name:        "nx-admin",
				Description: "Full system administration",
				Privileges:  []string{"*"},
				BuiltIn:     true,
				CreatedAt:   nowRole,
				UpdatedAt:   nowRole,
			},
			"nx-anonymous": {
				Name:        "nx-anonymous",
				Description: "Anonymous browse and download",
				Privileges: []string{
					"nexus:repository-view:*:*:browse",
					"nexus:repository-view:*:*:read",
				},
				BuiltIn:   true,
				CreatedAt: nowRole,
				UpdatedAt: nowRole,
			},
			"nx-developer": {
				Name:        "nx-developer",
				Description: "Browse, download and upload to hosted repositories",
				Privileges: []string{
					"nexus:repository-view:*:*:browse",
					"nexus:repository-view:*:*:read",
					"nexus:repository-view:*:*:write",
				},
				BuiltIn:   true,
				CreatedAt: nowRole,
				UpdatedAt: nowRole,
			},
		},
		Groups:           map[string]*Group{},
		Tokens:           map[string]*Token{},
		CleanupPolicies:  map[string]*CleanupPolicy{},
		ContentSelectors: map[string]*ContentSelector{},
		Security: SecuritySettings{
			AnonymousEnabled: true,
			AnonymousRoles:   []string{"nx-anonymous"},
		},
	}
}

func (s *Store) normalizeLocked() {
	loadedVersion := s.state.Version
	if s.state.Repositories == nil {
		s.state.Repositories = map[string]*Repository{}
	}
	if s.state.Assets == nil {
		s.state.Assets = map[string]map[string]*Asset{}
	}
	if s.state.Components == nil {
		s.state.Components = map[string]map[string]map[string]*Component{}
	}
	if s.state.ProxyStates == nil {
		s.state.ProxyStates = map[string]*ProxyState{}
	}
	if s.state.Users == nil {
		s.state.Users = map[string]*User{}
	}
	if s.state.Roles == nil {
		s.state.Roles = map[string]*Role{}
	}
	if s.state.Groups == nil {
		s.state.Groups = map[string]*Group{}
	}
	if s.state.Tokens == nil {
		s.state.Tokens = map[string]*Token{}
	}
	if s.state.CleanupPolicies == nil {
		s.state.CleanupPolicies = map[string]*CleanupPolicy{}
	}
	if s.state.ContentSelectors == nil {
		s.state.ContentSelectors = map[string]*ContentSelector{}
	}
	if loadedVersion < 2 {
		s.state.Security.AnonymousEnabled = true
	}
	if s.state.Security.AnonymousRoles == nil || len(s.state.Security.AnonymousRoles) == 0 {
		s.state.Security.AnonymousRoles = []string{"nx-anonymous"}
	}
	now := time.Now().UTC()
	if _, ok := s.state.Roles["nx-anonymous"]; !ok {
		s.state.Roles["nx-anonymous"] = &Role{
			Name: "nx-anonymous", Description: "Anonymous browse and download",
			Privileges: []string{"nexus:repository-view:*:*:browse", "nexus:repository-view:*:*:read"},
			BuiltIn:    true, CreatedAt: now, UpdatedAt: now,
		}
	}
	if _, ok := s.state.Roles["nx-developer"]; !ok {
		s.state.Roles["nx-developer"] = &Role{
			Name: "nx-developer", Description: "Browse, download and upload to hosted repositories",
			Privileges: []string{
				"nexus:repository-view:*:*:browse",
				"nexus:repository-view:*:*:read",
				"nexus:repository-view:*:*:write",
			},
			BuiltIn: true, CreatedAt: now, UpdatedAt: now,
		}
	}
	if loadedVersion != stateVersion {
		_ = s.persistLocked()
	}
	s.state.Version = stateVersion
}

func (s *Store) persistLocked() error {
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("create state temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(s.root, "state.json")); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

// Root returns the data directory.
func (s *Store) Root() string { return s.root }

// ListRepositories returns repositories sorted by name.
func (s *Store) ListRepositories() []*Repository {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Repository, 0, len(s.state.Repositories))
	for _, repo := range s.state.Repositories {
		out = append(out, cloneRepository(repo))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetRepository returns one repository.
func (s *Store) GetRepository(name string) (*Repository, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repo, ok := s.state.Repositories[name]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRepository(repo), nil
}

// CreateRepository inserts a new repository.
func (s *Store) CreateRepository(repo *Repository) (*Repository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.state.Repositories[repo.Name]; exists {
		return nil, ErrAlreadyExists
	}
	now := time.Now().UTC()
	copied := cloneRepository(repo)
	if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	copied.UpdatedAt = now
	s.state.Repositories[copied.Name] = copied
	if err := s.persistLocked(); err != nil {
		delete(s.state.Repositories, copied.Name)
		return nil, err
	}
	return cloneRepository(copied), nil
}

// UpdateRepository replaces settings for an existing repository.
func (s *Store) UpdateRepository(repo *Repository) (*Repository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.state.Repositories[repo.Name]
	if !ok {
		return nil, ErrNotFound
	}
	copied := cloneRepository(repo)
	copied.CreatedAt = existing.CreatedAt
	copied.UpdatedAt = time.Now().UTC()
	s.state.Repositories[copied.Name] = copied
	if err := s.persistLocked(); err != nil {
		s.state.Repositories[copied.Name] = existing
		return nil, err
	}
	s.invalidateNegativeLocked(copied.Name, "")
	return cloneRepository(copied), nil
}

// DeleteRepository removes a repository and all of its assets.
func (s *Store) DeleteRepository(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Repositories[name]; !ok {
		return ErrNotFound
	}
	delete(s.state.Repositories, name)
	removed := s.state.Assets[name]
	delete(s.state.Assets, name)
	delete(s.state.Components, name)
	delete(s.state.ProxyStates, name)
	if err := s.persistLocked(); err != nil {
		return err
	}
	s.removeUnreferencedBlobsLocked(removed)
	return nil
}

func cloneRepository(repo *Repository) *Repository {
	if repo == nil {
		return nil
	}
	copied := *repo
	if repo.Hosted != nil {
		hosted := *repo.Hosted
		copied.Hosted = &hosted
	}
	if repo.Proxy != nil {
		proxy := *repo.Proxy
		copied.Proxy = &proxy
	}
	if repo.Group != nil {
		group := *repo.Group
		group.MemberNames = append([]string(nil), repo.Group.MemberNames...)
		copied.Group = &group
	}
	copied.IncludePatterns = append([]string{}, repo.IncludePatterns...)
	copied.ExcludePatterns = append([]string{}, repo.ExcludePatterns...)
	return &copied
}

func cloneAsset(asset *Asset) *Asset {
	if asset == nil {
		return nil
	}
	copied := *asset
	return &copied
}

// GetAsset looks up an asset by repository and proxy path.
func (s *Store) GetAsset(repository, path string) (*Asset, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repoAssets, ok := s.state.Assets[repository]
	if !ok {
		return nil, false
	}
	asset, ok := repoAssets[path]
	if !ok {
		return nil, false
	}
	return cloneAsset(asset), true
}

// PutAsset inserts or replaces one asset and optionally upserts its component.
func (s *Store) PutAsset(asset *Asset, component *Component, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.putAssetLocked(asset, component, replace); err != nil {
		return err
	}
	if err := s.persistLocked(); err != nil {
		return err
	}
	s.invalidateNegativeLocked(asset.Repository, asset.Path)
	return nil
}

// PutRelease inserts the .mod/.info/.zip set and its component atomically.
func (s *Store) PutRelease(assets []*Asset, components []*Component, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !replace {
		for _, asset := range assets {
			if existing := s.state.Assets[asset.Repository][asset.Path]; existing != nil {
				return fmt.Errorf("%w: %s", ErrAlreadyExists, asset.Path)
			}
		}
	}
	for i, asset := range assets {
		var component *Component
		if i < len(components) {
			component = components[i]
		}
		if err := s.putAssetLocked(asset, component, replace); err != nil {
			return err
		}
	}
	if err := s.persistLocked(); err != nil {
		return err
	}
	for _, asset := range assets {
		s.invalidateNegativeLocked(asset.Repository, asset.Path)
	}
	return nil
}

func (s *Store) putAssetLocked(asset *Asset, component *Component, replace bool) error {
	if s.state.Assets[asset.Repository] == nil {
		s.state.Assets[asset.Repository] = map[string]*Asset{}
	}
	if quota := s.state.Repositories[asset.Repository].StorageQuotaBytes; quota > 0 {
		var usage int64
		for existingPath, existing := range s.state.Assets[asset.Repository] {
			if existingPath != asset.Path {
				usage += existing.Size
			}
		}
		if usage+asset.Size > quota {
			return fmt.Errorf("%w: %s (%d > %d bytes)", ErrQuota, asset.Repository, usage+asset.Size, quota)
		}
	}
	repoAssets := s.state.Assets[asset.Repository]
	existing := repoAssets[asset.Path]
	if existing != nil && !replace {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, asset.Path)
	}
	now := time.Now().UTC()
	copied := cloneAsset(asset)
	if existing != nil {
		copied.CreatedAt = existing.CreatedAt
		if existing.SHA256 != copied.SHA256 {
			defer s.removeBlobIfUnreferencedLocked(existing.SHA256)
		}
	}
	if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	if copied.UpdatedAt.IsZero() {
		copied.UpdatedAt = now
	}
	repoAssets[copied.Path] = copied
	if component != nil {
		s.upsertComponentLocked(component)
	}
	return nil
}

func (s *Store) upsertComponentLocked(component *Component) {
	if s.state.Components[component.Repository] == nil {
		s.state.Components[component.Repository] = map[string]map[string]*Component{}
	}
	modules := s.state.Components[component.Repository]
	if modules[component.Module] == nil {
		modules[component.Module] = map[string]*Component{}
	}
	now := time.Now().UTC()
	existing := modules[component.Module][component.Version]
	copied := *component
	copied.Kind = "go-module"
	if existing != nil {
		copied.CreatedAt = existing.CreatedAt
	} else if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	if copied.UpdatedAt.IsZero() {
		copied.UpdatedAt = now
	}
	modules[component.Module][component.Version] = &copied
}

// TouchAsset refreshes an asset's verification time without changing its blob.
func (s *Store) TouchAsset(repository, path string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	repoAssets := s.state.Assets[repository]
	if repoAssets == nil {
		return
	}
	if asset := repoAssets[path]; asset != nil {
		asset.UpdatedAt = at.UTC()
		_ = s.persistLocked()
		if asset.Version != "" {
			if modules := s.state.Components[repository]; modules != nil {
				if component := modules[asset.Module][asset.Version]; component != nil {
					component.UpdatedAt = at.UTC()
					_ = s.persistLocked()
				}
			}
		}
	}
}

// DeleteAsset removes a single asset and releases its blob when unreferenced.
func (s *Store) DeleteAsset(repository, path string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	assets := s.state.Assets[repository]
	if assets == nil {
		return false, nil
	}
	asset, ok := assets[path]
	if !ok {
		return false, nil
	}
	delete(assets, path)
	if err := s.persistLocked(); err != nil {
		return false, err
	}
	s.removeBlobIfUnreferencedLocked(asset.SHA256)
	s.invalidateNegativeLocked(repository, path)
	return true, nil
}

// ListComponents returns module versions sorted by module and SemVer.
func (s *Store) ListComponents(repository, moduleFilter, query string) []*Component {
	s.mu.RLock()
	defer s.mu.RUnlock()
	modules := s.state.Components[repository]
	if modules == nil {
		return nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]*Component, 0)
	for module, versions := range modules {
		if moduleFilter != "" && module != moduleFilter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(module), query) {
			matchesVersion := false
			for version := range versions {
				if strings.Contains(strings.ToLower(version), query) {
					matchesVersion = true
					break
				}
			}
			if !matchesVersion {
				continue
			}
		}
		for _, component := range versions {
			copied := *component
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// ComponentVersions returns the versions registered for a module.
func (s *Store) ComponentVersions(repository, module string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	modules := s.state.Components[repository]
	if modules == nil {
		return nil
	}
	versions := modules[module]
	out := make([]string, 0, len(versions))
	for version := range versions {
		out = append(out, version)
	}
	sort.Strings(out)
	return out
}

// GetComponent returns one module version.
func (s *Store) GetComponent(repository, module, version string) (*Component, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	modules := s.state.Components[repository]
	if modules == nil || modules[module] == nil {
		return nil, false
	}
	component, ok := modules[module][version]
	if !ok {
		return nil, false
	}
	copied := *component
	return &copied, true
}

// ListAssets returns assets sorted by most recently updated, filtered by query.
func (s *Store) ListAssets(repository, query string) []*Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query = strings.ToLower(strings.TrimSpace(query))
	repoAssets := s.state.Assets[repository]
	out := make([]*Asset, 0, len(repoAssets))
	for path, asset := range repoAssets {
		if query != "" && !strings.Contains(strings.ToLower(path), query) &&
			!strings.Contains(strings.ToLower(asset.Module), query) {
			continue
		}
		out = append(out, cloneAsset(asset))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// RecentAssets returns the most recently updated assets across repositories.
func (s *Store) RecentAssets(limit int) []*Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Asset, 0)
	for _, repoAssets := range s.state.Assets {
		for _, asset := range repoAssets {
			out = append(out, cloneAsset(asset))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Path < out[j].Path
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ProxyState returns the upstream health state for a repository.
func (s *Store) ProxyState(repository string) ProxyState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state := s.state.ProxyStates[repository]; state != nil {
		return *state
	}
	return ProxyState{}
}

// RecordProxySuccess clears the circuit breaker and failure count.
func (s *Store) RecordProxySuccess(repository string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state.ProxyStates[repository]
	if state == nil {
		state = &ProxyState{}
		s.state.ProxyStates[repository] = state
	}
	state.FailCount = 0
	state.BlockedUntil = time.Time{}
	state.LastSuccessAt = now.UTC()
	_ = s.persistLocked()
}

// RecordProxyFailure records an upstream failure and opens the circuit for
// backoff when backoff is positive.
func (s *Store) RecordProxyFailure(repository string, backoff time.Duration, message string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state.ProxyStates[repository]
	if state == nil {
		state = &ProxyState{}
		s.state.ProxyStates[repository] = state
	}
	state.FailCount++
	if backoff > 0 {
		state.BlockedUntil = now.Add(backoff).UTC()
	}
	state.LastError = message
	state.LastErrorAt = now.UTC()
	_ = s.persistLocked()
}

// IsProxyBlocked reports whether the proxy circuit is open.
func (s *Store) IsProxyBlocked(repository string, now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.state.ProxyStates[repository]
	return state != nil && state.BlockedUntil.After(now)
}

// RememberNotFound caches a negative proxy lookup.
func (s *Store) RememberNotFound(repository, path string, ttl time.Duration) {
	s.negMu.Lock()
	defer s.negMu.Unlock()
	s.negative[repository+"\x00"+path] = time.Now().Add(ttl)
}

// IsNotFoundCached reports whether a negative lookup is still valid.
func (s *Store) IsNotFoundCached(repository, path string) bool {
	s.negMu.Lock()
	defer s.negMu.Unlock()
	key := repository + "\x00" + path
	expiry, ok := s.negative[key]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.negative, key)
		return false
	}
	return true
}

// InvalidateNotFound clears a negative lookup.
func (s *Store) InvalidateNotFound(repository, path string) {
	s.negMu.Lock()
	defer s.negMu.Unlock()
	delete(s.negative, repository+"\x00"+path)
}

func (s *Store) invalidateNegativeLocked(repository, path string) {
	s.negMu.Lock()
	defer s.negMu.Unlock()
	for key := range s.negative {
		if path == "" {
			if strings.HasPrefix(key, repository+"\x00") {
				delete(s.negative, key)
			}
			continue
		}
		if key == repository+"\x00"+path {
			delete(s.negative, key)
		}
	}
}

// SaveBlob streams a blob into content-addressed storage and returns its
// SHA-256, size and relative blob key. At most maxBytes are accepted.
func (s *Store) SaveBlob(reader io.Reader, maxBytes int64) (string, int64, string, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "blob-*.tmp")
	if err != nil {
		return "", 0, "", fmt.Errorf("create blob temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hasher := sha256.New()
	limited := reader
	if maxBytes > 0 {
		limited = io.LimitReader(reader, maxBytes+1)
	}
	size, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		tmp.Close()
		return "", 0, "", fmt.Errorf("write blob: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", 0, "", fmt.Errorf("sync blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, "", fmt.Errorf("close blob: %w", err)
	}
	if maxBytes > 0 && size > maxBytes {
		return "", 0, "", fmt.Errorf("blob exceeds %d bytes", maxBytes)
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	target := s.blobPath(sum)
	if _, err := os.Stat(target); err == nil {
		return sum, size, sum, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", 0, "", fmt.Errorf("create blob directory: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return "", 0, "", fmt.Errorf("store blob: %w", err)
	}
	return sum, size, sum, nil
}

// SaveBlobBytes stores in-memory content.
func (s *Store) SaveBlobBytes(content []byte) (string, int64, string, error) {
	return s.SaveBlob(bytes.NewReader(content), int64(len(content))+1)
}

// SaveBlobFile stores a file without copying it through memory.
func (s *Store) SaveBlobFile(path string) (string, int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, "", err
	}
	defer file.Close()
	return s.SaveBlob(file, 0)
}

func (s *Store) blobPath(sum string) string {
	if len(sum) < 2 {
		return filepath.Join(s.root, "blobs", sum)
	}
	return filepath.Join(s.root, "blobs", sum[:2], sum)
}

// OpenBlob opens an asset's blob for reading.
func (s *Store) OpenBlob(asset *Asset) (io.ReadCloser, error) {
	if asset == nil || asset.SHA256 == "" {
		return nil, ErrNotFound
	}
	file, err := os.Open(s.blobPath(asset.SHA256))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return file, nil
}

// ReadBlob reads an asset's blob fully.
func (s *Store) ReadBlob(asset *Asset) ([]byte, error) {
	file, err := s.OpenBlob(asset)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func (s *Store) removeBlobIfUnreferencedLocked(sum string) {
	if sum == "" {
		return
	}
	for _, assets := range s.state.Assets {
		for _, asset := range assets {
			if asset.SHA256 == sum {
				return
			}
		}
	}
	_ = os.Remove(s.blobPath(sum))
}

func (s *Store) removeUnreferencedBlobsLocked(removed map[string]*Asset) {
	seen := make(map[string]struct{})
	for _, asset := range removed {
		if asset.SHA256 == "" {
			continue
		}
		if _, ok := seen[asset.SHA256]; ok {
			continue
		}
		seen[asset.SHA256] = struct{}{}
		s.removeBlobIfUnreferencedLocked(asset.SHA256)
	}
}

// TempDir returns a directory for temporary downloads and uploads.
func (s *Store) TempDir() string { return filepath.Join(s.root, "tmp") }
