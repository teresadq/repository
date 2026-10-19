package store

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// CleanupPolicy describes which assets a cleanup run may remove.
type CleanupPolicy struct {
	Name                        string    `json:"name"`
	Repository                  string    `json:"repository"`
	Format                      string    `json:"format"`
	Enabled                     bool      `json:"enabled"`
	Pattern                     string    `json:"pattern,omitempty"`
	PublishedOlderThanDays      int       `json:"publishedOlderThanDays,omitempty"`
	LastDownloadedOlderThanDays int       `json:"lastDownloadedOlderThanDays,omitempty"`
	RetainCount                 int       `json:"retainCount,omitempty"`
	CreatedAt                   time.Time `json:"createdAt"`
	UpdatedAt                   time.Time `json:"updatedAt"`
}

// CleanupItem is one matched asset in a cleanup run.
type CleanupItem struct {
	Path    string `json:"path"`
	Module  string `json:"module,omitempty"`
	Version string `json:"version,omitempty"`
	Size    int64  `json:"size"`
	Reason  string `json:"reason"`
}

// CleanupRun is the result of a preview or delete run.
type CleanupRun struct {
	ID      string        `json:"id"`
	Policy  string        `json:"policy"`
	At      time.Time     `json:"at"`
	DryRun  bool          `json:"dryRun"`
	Scanned int           `json:"scanned"`
	Matched int           `json:"matched"`
	Deleted int           `json:"deleted"`
	Bytes   int64         `json:"bytes"`
	Items   []CleanupItem `json:"items"`
	Error   string        `json:"error,omitempty"`
}

const maxCleanupRuns = 100

// ListCleanupPolicies returns policies sorted by name.
func (s *Store) ListCleanupPolicies() []*CleanupPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CleanupPolicy, 0, len(s.state.CleanupPolicies))
	for _, policy := range s.state.CleanupPolicies {
		copied := *policy
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetCleanupPolicy returns one policy.
func (s *Store) GetCleanupPolicy(name string) (*CleanupPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	policy, ok := s.state.CleanupPolicies[name]
	if !ok {
		return nil, ErrNotFound
	}
	copied := *policy
	return &copied, nil
}

// PutCleanupPolicy creates or replaces a policy.
func (s *Store) PutCleanupPolicy(policy *CleanupPolicy) (*CleanupPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if policy.Name == "" || policy.Repository == "" {
		return nil, ErrInvalid
	}
	if _, ok := s.state.Repositories[policy.Repository]; !ok {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	copied := *policy
	if existing, ok := s.state.CleanupPolicies[policy.Name]; ok {
		copied.CreatedAt = existing.CreatedAt
	} else if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	copied.UpdatedAt = now
	s.state.CleanupPolicies[copied.Name] = &copied
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	result := copied
	return &result, nil
}

// DeleteCleanupPolicy removes a policy.
func (s *Store) DeleteCleanupPolicy(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.CleanupPolicies[name]; !ok {
		return ErrNotFound
	}
	delete(s.state.CleanupPolicies, name)
	return s.persistLocked()
}

// ListCleanupRuns returns newest-first runs.
func (s *Store) ListCleanupRuns(limit int) []CleanupRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CleanupRun, 0, len(s.state.CleanupRuns))
	for index := len(s.state.CleanupRuns) - 1; index >= 0; index-- {
		out = append(out, s.state.CleanupRuns[index])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// RunCleanup evaluates a policy and optionally deletes matched assets.
func (s *Store) RunCleanup(policyName string, dryRun bool) (*CleanupRun, error) {
	s.mu.Lock()
	policy, ok := s.state.CleanupPolicies[policyName]
	if !ok {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	copied := *policy
	assets := make([]*Asset, 0)
	for _, asset := range s.state.Assets[copied.Repository] {
		copiedAsset := *asset
		assets = append(assets, &copiedAsset)
	}
	s.mu.Unlock()

	run := &CleanupRun{
		ID: RandomID(8), Policy: copied.Name, At: time.Now().UTC(), DryRun: dryRun,
	}
	now := time.Now().UTC()
	matched := make([]*Asset, 0)
	for _, asset := range assets {
		run.Scanned++
		if copied.Pattern != "" {
			ok, err := matchCleanupGlob(copied.Pattern, asset.Path)
			if err != nil {
				return nil, fmt.Errorf("invalid cleanup pattern: %w", err)
			}
			if !ok {
				continue
			}
		}
		reason := ""
		if copied.PublishedOlderThanDays > 0 {
			cutoff := now.AddDate(0, 0, -copied.PublishedOlderThanDays)
			if asset.CreatedAt.After(cutoff) {
				continue
			}
			reason = fmt.Sprintf("published > %d days", copied.PublishedOlderThanDays)
		}
		if copied.LastDownloadedOlderThanDays > 0 {
			cutoff := now.AddDate(0, 0, -copied.LastDownloadedOlderThanDays)
			last := asset.LastDownloadedAt
			if last.IsZero() {
				last = asset.UpdatedAt
			}
			if last.After(cutoff) {
				continue
			}
			if reason != "" {
				reason += "; "
			}
			reason += fmt.Sprintf("not downloaded for %d days", copied.LastDownloadedOlderThanDays)
		}
		if reason == "" && copied.RetainCount <= 0 && copied.PublishedOlderThanDays == 0 &&
			copied.LastDownloadedOlderThanDays == 0 {
			continue
		}
		matched = append(matched, asset)
		if reason == "" {
			reason = "retention"
		}
		run.Items = append(run.Items, CleanupItem{
			Path: asset.Path, Module: asset.Module, Version: asset.Version,
			Size: asset.Size, Reason: reason,
		})
	}

	if copied.RetainCount > 0 {
		byModule := map[string][]*Asset{}
		for _, asset := range matched {
			byModule[asset.Module] = append(byModule[asset.Module], asset)
		}
		retained := map[string]struct{}{}
		for _, group := range byModule {
			sort.Slice(group, func(i, j int) bool {
				return group[i].UpdatedAt.After(group[j].UpdatedAt)
			})
			for index, asset := range group {
				if index < copied.RetainCount {
					retained[asset.Path] = struct{}{}
				}
			}
		}
		filtered := matched[:0]
		items := run.Items[:0]
		for _, asset := range matched {
			if _, keep := retained[asset.Path]; keep {
				continue
			}
			filtered = append(filtered, asset)
			for _, item := range run.Items {
				if item.Path == asset.Path {
					items = append(items, item)
					break
				}
			}
		}
		matched = filtered
		run.Items = items
	}

	run.Matched = len(matched)
	for _, asset := range matched {
		run.Bytes += asset.Size
	}

	if !dryRun {
		paths := make([]string, 0, len(matched))
		for _, asset := range matched {
			paths = append(paths, asset.Path)
		}
		deleted, err := s.DeleteAssets(copied.Repository, paths)
		if err != nil {
			run.Error = err.Error()
		}
		run.Deleted = deleted
	}

	s.mu.Lock()
	s.state.CleanupRuns = append(s.state.CleanupRuns, *run)
	if len(s.state.CleanupRuns) > maxCleanupRuns {
		s.state.CleanupRuns = s.state.CleanupRuns[len(s.state.CleanupRuns)-maxCleanupRuns:]
	}
	_ = s.persistLocked()
	s.mu.Unlock()
	return run, nil
}

// DeleteAssets removes multiple assets in one state update.
func (s *Store) DeleteAssets(repository string, paths []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	assets := s.state.Assets[repository]
	if assets == nil {
		return 0, nil
	}
	removed := make([]*Asset, 0, len(paths))
	for _, assetPath := range paths {
		if asset, ok := assets[assetPath]; ok {
			removed = append(removed, asset)
			delete(assets, assetPath)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if err := s.persistLocked(); err != nil {
		return 0, err
	}
	for _, asset := range removed {
		s.removeBlobIfUnreferencedLocked(asset.SHA256)
		s.invalidateNegativeLocked(repository, asset.Path)
	}
	return len(removed), nil
}

// TouchDownload records a successful download for cleanup criteria.
func (s *Store) TouchDownload(repository, assetPath string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	assets := s.state.Assets[repository]
	if assets == nil {
		return
	}
	asset, ok := assets[assetPath]
	if !ok {
		return
	}
	asset.LastDownloadedAt = at.UTC()
	_ = s.persistLocked()
}

// matchCleanupGlob supports '/'-separated globs plus '**' for any depth.
func matchCleanupGlob(pattern, name string) (bool, error) {
	return matchGlobSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchGlobSegments(pattern, name []string) (bool, error) {
	if len(pattern) == 0 {
		return len(name) == 0, nil
	}
	if pattern[0] == "**" {
		for index := 0; index <= len(name); index++ {
			ok, err := matchGlobSegments(pattern[1:], name[index:])
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if len(name) == 0 {
		return false, nil
	}
	ok, err := path.Match(pattern[0], name[0])
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return matchGlobSegments(pattern[1:], name[1:])
}
