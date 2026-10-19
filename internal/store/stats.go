package store

import (
	"sort"
	"time"
)

// RepoStats summarizes one repository's contents.
type RepoStats struct {
	Components  int       `json:"components"`
	Assets      int       `json:"assets"`
	Bytes       int64     `json:"bytes"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// DailyPoint is one day of asset activity.
type DailyPoint struct {
	Date   string `json:"date"`
	Assets int    `json:"assets"`
	Bytes  int64  `json:"bytes"`
}

// ModuleUsage is an aggregate across repositories.
type ModuleUsage struct {
	Module   string `json:"module"`
	Versions int    `json:"versions"`
	Bytes    int64  `json:"bytes"`
}

// Overview is the dashboard aggregate.
type Overview struct {
	Repositories int           `json:"repositories"`
	Online       int           `json:"online"`
	Hosted       int           `json:"hosted"`
	Proxy        int           `json:"proxy"`
	Groups       int           `json:"groups"`
	Components   int           `json:"components"`
	Assets       int           `json:"assets"`
	Bytes        int64         `json:"bytes"`
	Daily        []DailyPoint  `json:"daily"`
	TopModules   []ModuleUsage `json:"topModules"`
}

// RepoStats returns totals for one repository.
func (s *Store) RepoStats(repository string) RepoStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := RepoStats{}
	if modules := s.state.Components[repository]; modules != nil {
		for _, versions := range modules {
			stats.Components += len(versions)
		}
	}
	for _, asset := range s.state.Assets[repository] {
		stats.Assets++
		stats.Bytes += asset.Size
		if asset.UpdatedAt.After(stats.LastUpdated) {
			stats.LastUpdated = asset.UpdatedAt
		}
	}
	return stats
}

// AllRepoStats returns totals keyed by repository name.
func (s *Store) AllRepoStats() map[string]RepoStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]RepoStats, len(s.state.Repositories))
	for name := range s.state.Repositories {
		stats := RepoStats{}
		if modules := s.state.Components[name]; modules != nil {
			for _, versions := range modules {
				stats.Components += len(versions)
			}
		}
		for _, asset := range s.state.Assets[name] {
			stats.Assets++
			stats.Bytes += asset.Size
			if asset.UpdatedAt.After(stats.LastUpdated) {
				stats.LastUpdated = asset.UpdatedAt
			}
		}
		out[name] = stats
	}
	return out
}

// Overview aggregates dashboard metrics over the last days days.
func (s *Store) Overview(days int) Overview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if days <= 0 {
		days = 14
	}
	overview := Overview{Repositories: len(s.state.Repositories)}
	moduleVersions := map[string]int{}
	moduleBytes := map[string]int64{}
	blobBytes := map[string]int64{}

	for _, repo := range s.state.Repositories {
		if repo.Online {
			overview.Online++
		}
		switch repo.Type {
		case "hosted":
			overview.Hosted++
		case "proxy":
			overview.Proxy++
		case "group":
			overview.Groups++
		}
	}
	dayIndex := make(map[string]int, days)
	start := time.Now().UTC().AddDate(0, 0, -(days - 1))
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		dayIndex[key] = len(overview.Daily)
		overview.Daily = append(overview.Daily, DailyPoint{Date: key})
	}
	for _, modules := range s.state.Components {
		for module, versions := range modules {
			overview.Components += len(versions)
			moduleVersions[module] += len(versions)
		}
	}
	for _, assets := range s.state.Assets {
		for _, asset := range assets {
			overview.Assets++
			if _, seen := blobBytes[asset.SHA256]; !seen {
				blobBytes[asset.SHA256] = asset.Size
			}
			moduleBytes[asset.Module] += asset.Size
			key := asset.CreatedAt.UTC().Format("2006-01-02")
			if index, ok := dayIndex[key]; ok {
				overview.Daily[index].Assets++
				overview.Daily[index].Bytes += asset.Size
			}
		}
	}
	for _, size := range blobBytes {
		overview.Bytes += size
	}
	for module, count := range moduleVersions {
		overview.TopModules = append(overview.TopModules, ModuleUsage{
			Module:   module,
			Versions: count,
			Bytes:    moduleBytes[module],
		})
	}
	sort.Slice(overview.TopModules, func(i, j int) bool {
		if overview.TopModules[i].Versions != overview.TopModules[j].Versions {
			return overview.TopModules[i].Versions > overview.TopModules[j].Versions
		}
		return overview.TopModules[i].Module < overview.TopModules[j].Module
	})
	if len(overview.TopModules) > 8 {
		overview.TopModules = overview.TopModules[:8]
	}
	return overview
}

// AssetsForComponent returns the stored assets that belong to one module version.
func (s *Store) AssetsForComponent(repository, module, version string) []*Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Asset, 0, 3)
	for _, asset := range s.state.Assets[repository] {
		if asset.Module == module && asset.Version == version {
			out = append(out, cloneAsset(asset))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// RepoCounts returns asset and component counts without a full stats scan.
func (s *Store) RepoCounts(repository string) (assets int, components int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, versions := range s.state.Components[repository] {
		components += len(versions)
	}
	return len(s.state.Assets[repository]), components
}
