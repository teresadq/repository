package api

import (
	"net/http"
	"strings"
)

type recipeInfo struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Type   string `json:"type"`
	Label  string `json:"label"`
}

var availableRecipes = []recipeInfo{
	{Name: "maven-hosted", Format: "maven", Type: "hosted", Label: "Maven 本地"},
	{Name: "maven-proxy", Format: "maven", Type: "proxy", Label: "Maven 代理"},
	{Name: "maven-group", Format: "maven", Type: "group", Label: "Maven 分组"},
	{Name: "npm-hosted", Format: "npm", Type: "hosted", Label: "npm 本地"},
	{Name: "npm-proxy", Format: "npm", Type: "proxy", Label: "npm 代理"},
	{Name: "npm-group", Format: "npm", Type: "group", Label: "npm 分组"},
	{Name: "pypi-hosted", Format: "pypi", Type: "hosted", Label: "PyPI 本地"},
	{Name: "pypi-proxy", Format: "pypi", Type: "proxy", Label: "PyPI 代理"},
	{Name: "pypi-group", Format: "pypi", Type: "group", Label: "PyPI 分组"},
	{Name: "go-hosted", Format: "go", Type: "hosted", Label: "Go 本地"},
	{Name: "go-proxy", Format: "go", Type: "proxy", Label: "Go 代理"},
	{Name: "go-group", Format: "go", Type: "group", Label: "Go 分组"},
	{Name: "helm-hosted", Format: "helm", Type: "hosted", Label: "Helm 本地"},
	{Name: "helm-proxy", Format: "helm", Type: "proxy", Label: "Helm 代理"},
	{Name: "helm-group", Format: "helm", Type: "group", Label: "Helm 分组"},
	{Name: "cargo-hosted", Format: "cargo", Type: "hosted", Label: "Cargo 本地"},
	{Name: "cargo-proxy", Format: "cargo", Type: "proxy", Label: "Cargo 代理"},
	{Name: "cargo-group", Format: "cargo", Type: "group", Label: "Cargo 分组"},
	{Name: "pub-hosted", Format: "pub", Type: "hosted", Label: "Dart Pub 本地"},
	{Name: "pub-proxy", Format: "pub", Type: "proxy", Label: "Dart Pub 代理"},
	{Name: "pub-group", Format: "pub", Type: "group", Label: "Dart Pub 分组"},
	{Name: "docker-hosted", Format: "docker", Type: "hosted", Label: "Docker 本地"},
	{Name: "docker-proxy", Format: "docker", Type: "proxy", Label: "Docker 代理"},
	{Name: "docker-group", Format: "docker", Type: "group", Label: "Docker 分组"},
	{Name: "gitlfs-hosted", Format: "gitlfs", Type: "hosted", Label: "Git LFS 本地"},
	{Name: "conda-hosted", Format: "conda", Type: "hosted", Label: "Conda 本地"},
	{Name: "conda-proxy", Format: "conda", Type: "proxy", Label: "Conda 代理"},
	{Name: "conda-group", Format: "conda", Type: "group", Label: "Conda 分组"},
	{Name: "swift-hosted", Format: "swift", Type: "hosted", Label: "Swift 本地"},
	{Name: "swift-proxy", Format: "swift", Type: "proxy", Label: "Swift 代理"},
	{Name: "swift-group", Format: "swift", Type: "group", Label: "Swift 分组"},
	{Name: "apt-hosted", Format: "apt", Type: "hosted", Label: "APT 本地"},
	{Name: "apt-proxy", Format: "apt", Type: "proxy", Label: "APT 代理"},
	{Name: "alpine-hosted", Format: "alpine", Type: "hosted", Label: "Alpine 本地"},
	{Name: "alpine-proxy", Format: "alpine", Type: "proxy", Label: "Alpine 代理"},
	{Name: "alpine-group", Format: "alpine", Type: "group", Label: "Alpine 分组"},
	{Name: "yum-hosted", Format: "yum", Type: "hosted", Label: "Yum 本地"},
	{Name: "yum-proxy", Format: "yum", Type: "proxy", Label: "Yum 代理"},
	{Name: "yum-group", Format: "yum", Type: "group", Label: "Yum 分组"},
	{Name: "raw-hosted", Format: "raw", Type: "hosted", Label: "Raw 本地"},
	{Name: "raw-proxy", Format: "raw", Type: "proxy", Label: "Raw 代理"},
	{Name: "raw-group", Format: "raw", Type: "group", Label: "Raw 分组"},
}

func parseRecipe(recipe string) (string, string, bool) {
	for _, item := range availableRecipes {
		if item.Name == recipe {
			return item.Format, item.Type, true
		}
	}
	// Accept format/type pairs separated by a dash for forward compatibility.
	index := strings.LastIndexByte(recipe, '-')
	if index <= 0 {
		return "", "", false
	}
	format, repoType := recipe[:index], recipe[index+1:]
	if repoType != "hosted" && repoType != "proxy" && repoType != "group" {
		return "", "", false
	}
	for _, item := range availableRecipes {
		if item.Format == format {
			return format, repoType, true
		}
	}
	return "", "", false
}

func (s *Server) handleRecipes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": availableRecipes})
}
