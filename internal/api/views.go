package api

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

var repositoryNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type repositoryRequest struct {
	Name        string         `json:"name"`
	Recipe      string         `json:"recipe"`
	Online      *bool          `json:"online"`
	Description *string        `json:"description"`
	Hosted      *hostedRequest `json:"hosted"`
	Proxy       *proxyRequest  `json:"proxy"`
	Group       *groupRequest  `json:"group"`
}

type hostedRequest struct {
	WritePolicy string `json:"writePolicy"`
}

type proxyRequest struct {
	RemoteURL             string `json:"remoteUrl"`
	ContentMaxAgeMinutes  *int   `json:"contentMaxAgeMinutes"`
	MetadataMaxAgeMinutes *int   `json:"metadataMaxAgeMinutes"`
	AutoBlock             *bool  `json:"autoBlock"`
	RemoteUsername        string `json:"remoteUsername"`
	RemotePassword        string `json:"remotePassword"`
	RemoteBearerToken     string `json:"remoteBearerToken"`
}

type groupRequest struct {
	MemberNames []string `json:"memberNames"`
}

type repoView struct {
	Name        string                `json:"name"`
	Recipe      string                `json:"recipe"`
	Format      string                `json:"format"`
	Type        string                `json:"type"`
	Online      bool                  `json:"online"`
	Description string                `json:"description"`
	URL         string                `json:"url"`
	Hosted      *store.HostedSettings `json:"hosted,omitempty"`
	Proxy       *store.ProxySettings  `json:"proxy,omitempty"`
	Group       *store.GroupSettings  `json:"group,omitempty"`
	CreatedAt   time.Time             `json:"createdAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
	Stats       store.RepoStats       `json:"stats"`
	ProxyState  *proxyStateView       `json:"proxyState,omitempty"`
}

type proxyStateView struct {
	FailCount     int        `json:"failCount"`
	Blocked       bool       `json:"blocked"`
	BlockedUntil  *time.Time `json:"blockedUntil,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
	LastErrorAt   *time.Time `json:"lastErrorAt,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
}

type componentView struct {
	Module      string            `json:"module"`
	Namespace   string            `json:"namespace,omitempty"`
	Format      string            `json:"format,omitempty"`
	Version     string            `json:"version"`
	Kind        string            `json:"kind"`
	Time        time.Time         `json:"time"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	Assets      int               `json:"assets"`
	Bytes       int64             `json:"bytes"`
	ArchiveSize int64             `json:"archiveSize"`
	ArchiveSHA  string            `json:"archiveSha,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Downloads   map[string]string `json:"downloads"`
}

type assetView struct {
	Path        string            `json:"path"`
	Format      string            `json:"format,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	Module      string            `json:"module"`
	Version     string            `json:"version,omitempty"`
	Kind        string            `json:"kind"`
	ContentType string            `json:"contentType"`
	Size        int64             `json:"size"`
	SHA256      string            `json:"sha256"`
	Source      string            `json:"source"`
	URL         string            `json:"url"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

type activityView struct {
	Repository     string    `json:"repository"`
	RepositoryType string    `json:"repositoryType"`
	Format         string    `json:"format,omitempty"`
	Module         string    `json:"module"`
	Version        string    `json:"version,omitempty"`
	Kind           string    `json:"kind"`
	Path           string    `json:"path"`
	Size           int64     `json:"size"`
	Source         string    `json:"source"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (s *Server) buildRepository(body repositoryRequest, existing *store.Repository) (*store.Repository, error) {
	name := strings.TrimSpace(body.Name)
	if existing == nil {
		if !repositoryNameRE.MatchString(name) {
			return nil, fmt.Errorf("仓库名称只能包含小写字母、数字、点、下划线和连字符，且必须以字母或数字开头")
		}
	} else {
		name = existing.Name
	}
	recipe := strings.TrimSpace(body.Recipe)
	if existing == nil {
		if _, _, ok := parseRecipe(recipe); !ok {
			return nil, fmt.Errorf("unsupported recipe: %s", recipe)
		}
	} else {
		if recipe != "" && recipe != existing.Recipe {
			return nil, fmt.Errorf("repository recipe cannot be changed")
		}
		recipe = existing.Recipe
	}
	format, repoType, _ := parseRecipe(recipe)
	repo := &store.Repository{
		Name:   name,
		Recipe: recipe,
		Format: format,
		Type:   repoType,
	}
	if existing != nil {
		repo.CreatedAt = existing.CreatedAt
	}
	if body.Online != nil {
		repo.Online = *body.Online
	} else if existing != nil {
		repo.Online = existing.Online
	} else {
		repo.Online = true
	}
	if body.Description != nil {
		repo.Description = strings.TrimSpace(*body.Description)
	} else if existing != nil {
		repo.Description = existing.Description
	}

	switch repoType {
	case "hosted":
		policy := "ALLOW"
		if existing != nil && existing.Hosted != nil && existing.Hosted.WritePolicy != "" {
			policy = existing.Hosted.WritePolicy
		}
		if body.Hosted != nil && strings.TrimSpace(body.Hosted.WritePolicy) != "" {
			policy = strings.ToUpper(strings.TrimSpace(body.Hosted.WritePolicy))
		}
		if policy != "ALLOW" && policy != "DENY" {
			return nil, fmt.Errorf("write policy must be ALLOW or DENY")
		}
		repo.Hosted = &store.HostedSettings{WritePolicy: policy}
		if existing != nil && existing.Hosted != nil {
			repo.Hosted.VersionPolicy = existing.Hosted.VersionPolicy
			repo.Hosted.LayoutPolicy = existing.Hosted.LayoutPolicy
		}
	case "proxy":
		remote := ""
		contentMaxAge := 1440
		metadataMaxAge := 1440
		autoBlock := true
		remoteUsername, remotePassword, remoteBearer := "", "", ""
		if existing != nil && existing.Proxy != nil {
			remote = existing.Proxy.RemoteURL
			contentMaxAge = existing.Proxy.ContentMaxAgeMinutes
			metadataMaxAge = existing.Proxy.MetadataMaxAgeMinutes
			autoBlock = existing.Proxy.AutoBlock
			remoteUsername = existing.Proxy.RemoteUsername
			remotePassword = existing.Proxy.RemotePassword
			remoteBearer = existing.Proxy.RemoteBearerToken
		}
		if body.Proxy != nil {
			if strings.TrimSpace(body.Proxy.RemoteURL) != "" {
				remote = strings.TrimSpace(body.Proxy.RemoteURL)
			}
			if body.Proxy.ContentMaxAgeMinutes != nil {
				contentMaxAge = *body.Proxy.ContentMaxAgeMinutes
			}
			if body.Proxy.MetadataMaxAgeMinutes != nil {
				metadataMaxAge = *body.Proxy.MetadataMaxAgeMinutes
			}
			if body.Proxy.AutoBlock != nil {
				autoBlock = *body.Proxy.AutoBlock
			}
			if body.Proxy.RemoteUsername != "" {
				remoteUsername = body.Proxy.RemoteUsername
			}
			if body.Proxy.RemotePassword != "" {
				remotePassword = body.Proxy.RemotePassword
			}
			if body.Proxy.RemoteBearerToken != "" {
				remoteBearer = body.Proxy.RemoteBearerToken
			}
		}
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("代理仓库需要有效的 http(s) 上游地址")
		}
		if contentMaxAge < -1 || metadataMaxAge < -1 {
			return nil, fmt.Errorf("缓存有效期必须为 -1（永不过期）或非负分钟数")
		}
		repo.Proxy = &store.ProxySettings{
			RemoteURL:             strings.TrimRight(remote, "/"),
			ContentMaxAgeMinutes:  contentMaxAge,
			MetadataMaxAgeMinutes: metadataMaxAge,
			AutoBlock:             autoBlock,
			RemoteUsername:        remoteUsername,
			RemotePassword:        remotePassword,
			RemoteBearerToken:     remoteBearer,
		}
	case "group":
		members := []string(nil)
		if body.Group != nil {
			members = body.Group.MemberNames
		} else if existing != nil && existing.Group != nil {
			members = existing.Group.MemberNames
		}
		seen := map[string]struct{}{}
		cleaned := make([]string, 0, len(members))
		for _, memberName := range members {
			memberName = strings.TrimSpace(memberName)
			if memberName == "" {
				continue
			}
			if _, ok := seen[memberName]; ok {
				continue
			}
			seen[memberName] = struct{}{}
			if memberName == name {
				return nil, fmt.Errorf("group repository cannot reference itself")
			}
			member, err := s.store.GetRepository(memberName)
			if err != nil {
				return nil, fmt.Errorf("member repository not found: %s", memberName)
			}
			if member.Format != repo.Format || (member.Type != "hosted" && member.Type != "proxy") {
				return nil, fmt.Errorf("group members must be %s hosted or proxy repositories: %s", repo.Format, memberName)
			}
			cleaned = append(cleaned, memberName)
		}
		if len(cleaned) == 0 {
			return nil, fmt.Errorf("group repository requires at least one member")
		}
		repo.Group = &store.GroupSettings{MemberNames: cleaned}
	default:
		return nil, fmt.Errorf("unsupported repository type: %s", repoType)
	}
	return repo, nil
}

func (s *Server) repositorySummaries() []repoView {
	allStats := s.store.AllRepoStats()
	repositories := s.store.ListRepositories()
	out := make([]repoView, 0, len(repositories))
	for _, repo := range repositories {
		view := s.repositoryView(repo)
		view.Stats = allStats[repo.Name]
		out = append(out, view)
	}
	return out
}

func (s *Server) repositoryView(repo *store.Repository) repoView {
	view := repoView{
		Name:        repo.Name,
		Recipe:      repo.Recipe,
		Format:      repo.Format,
		Type:        repo.Type,
		Online:      repo.Online,
		Description: repo.Description,
		URL:         "/repository/" + repo.Name,
		Hosted:      repo.Hosted,
		Proxy:       repo.Proxy,
		Group:       repo.Group,
		CreatedAt:   repo.CreatedAt,
		UpdatedAt:   repo.UpdatedAt,
		Stats:       s.store.RepoStats(repo.Name),
	}
	if repo.Type == "proxy" {
		state := proxyStateViewFrom(s.store.ProxyState(repo.Name))
		view.ProxyState = &state
	}
	return view
}

func proxyStateViewFrom(state store.ProxyState) proxyStateView {
	view := proxyStateView{
		FailCount: state.FailCount,
		Blocked:   time.Now().Before(state.BlockedUntil),
		LastError: state.LastError,
	}
	if !state.BlockedUntil.IsZero() {
		value := state.BlockedUntil
		view.BlockedUntil = &value
	}
	if !state.LastErrorAt.IsZero() {
		value := state.LastErrorAt
		view.LastErrorAt = &value
	}
	if !state.LastSuccessAt.IsZero() {
		value := state.LastSuccessAt
		view.LastSuccessAt = &value
	}
	return view
}

func (s *Server) componentView(repo *store.Repository, component *store.Component) componentView {
	assets := s.store.AssetsForComponent(repo.Name, component.Module, component.Version)
	view := componentView{
		Module:     component.Module,
		Namespace:  component.Namespace,
		Format:     component.Format,
		Version:    component.Version,
		Kind:       component.Kind,
		Attributes: component.Attributes,
		Time:       component.Time,
		CreatedAt:  component.CreatedAt,
		UpdatedAt:  component.UpdatedAt,
		Assets:     len(assets),
		Downloads:  map[string]string{},
	}
	for _, asset := range assets {
		view.Bytes += asset.Size
		switch asset.Kind {
		case "PACKAGE":
			view.ArchiveSize = asset.Size
			view.ArchiveSHA = asset.SHA256
		}
		view.Downloads[strings.ToLower(asset.Kind)] = "/repository/" + repo.Name + "/" + asset.Path
	}
	return view
}

func (s *Server) assetView(asset *store.Asset) assetView {
	return assetView{
		Path:        asset.Path,
		Format:      asset.Format,
		Namespace:   asset.Namespace,
		Module:      asset.Module,
		Version:     asset.Version,
		Kind:        asset.Kind,
		ContentType: asset.ContentType,
		Size:        asset.Size,
		SHA256:      asset.SHA256,
		Source:      asset.Source,
		URL:         "/repository/" + asset.Repository + "/" + asset.Path,
		Attributes:  asset.Attributes,
		CreatedAt:   asset.CreatedAt,
		UpdatedAt:   asset.UpdatedAt,
	}
}

func (s *Server) activityViews(limit int) []activityView {
	repositories := s.store.ListRepositories()
	types := make(map[string]string, len(repositories))
	for _, repo := range repositories {
		types[repo.Name] = repo.Type
	}
	assets := s.store.RecentAssets(limit)
	out := make([]activityView, 0, len(assets))
	for _, asset := range assets {
		out = append(out, activityView{
			Repository:     asset.Repository,
			RepositoryType: types[asset.Repository],
			Format:         asset.Format,
			Module:         asset.Module,
			Version:        asset.Version,
			Kind:           asset.Kind,
			Path:           asset.Path,
			Size:           asset.Size,
			Source:         asset.Source,
			UpdatedAt:      asset.UpdatedAt,
		})
	}
	return out
}
