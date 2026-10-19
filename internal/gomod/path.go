package gomod

import (
	"fmt"
	"strings"
)

// AssetKind identifies the kind of a Go module proxy artifact.
type AssetKind int

const (
	KindPackage AssetKind = iota // .zip
	KindInfo                     // .info
	KindList                     // @v/list
	KindLatest                   // @latest
	KindModule                   // .mod
)

func (k AssetKind) String() string {
	switch k {
	case KindPackage:
		return "PACKAGE"
	case KindInfo:
		return "INFO"
	case KindList:
		return "LIST"
	case KindLatest:
		return "LATEST"
	case KindModule:
		return "MODULE"
	default:
		return "UNKNOWN"
	}
}

// AssetKindFromString parses a persisted kind.
func AssetKindFromString(value string) AssetKind {
	switch strings.ToUpper(value) {
	case "PACKAGE", "ZIP":
		return KindPackage
	case "INFO":
		return KindInfo
	case "LIST":
		return KindList
	case "LATEST":
		return KindLatest
	case "MODULE", "MOD":
		return KindModule
	default:
		return KindPackage
	}
}

// Extension returns the file extension for versioned kinds.
func (k AssetKind) Extension() (string, error) {
	switch k {
	case KindPackage:
		return "zip", nil
	case KindInfo:
		return "info", nil
	case KindModule:
		return "mod", nil
	default:
		return "", fmt.Errorf("Go metadata kind has no file extension: %s", k)
	}
}

// GoPath is a parsed Go module proxy request path.
type GoPath struct {
	Path    string
	Module  string
	Version string
	Kind    AssetKind
}

const versionMarker = "/@v/"

// ParseGoPath parses a raw path such as:
//
//	github.com/!burnt!sushi/toml/@v/list
//	github.com/!burnt!sushi/toml/@latest
//	github.com/!burnt!sushi/toml/@v/v1.2.3.info
//
// Leading slashes are ignored.
func ParseGoPath(rawPath string) (GoPath, error) {
	path := strings.TrimLeft(rawPath, "/")
	if strings.TrimSpace(path) == "" || strings.HasSuffix(path, "/") {
		return GoPath{}, fmt.Errorf("invalid Go module proxy path: %s", path)
	}
	if strings.HasSuffix(path, "/@latest") {
		module, err := requireModule(path[:len(path)-len("/@latest")], path)
		if err != nil {
			return GoPath{}, err
		}
		return GoPath{Path: path, Module: module, Kind: KindLatest}, nil
	}
	if strings.HasSuffix(path, "/@v/list") {
		module, err := requireModule(path[:len(path)-len("/@v/list")], path)
		if err != nil {
			return GoPath{}, err
		}
		return GoPath{Path: path, Module: module, Kind: KindList}, nil
	}

	marker := strings.LastIndex(path, versionMarker)
	if marker <= 0 {
		return GoPath{}, fmt.Errorf("invalid Go module proxy path: %s", path)
	}
	escapedModule := path[:marker]
	file := path[marker+len(versionMarker):]
	dot := strings.LastIndexByte(file, '.')
	if dot <= 0 || dot == len(file)-1 {
		return GoPath{}, fmt.Errorf("invalid Go module version path: %s", path)
	}
	escapedVersion := file[:dot]
	extension := file[dot+1:]
	var kind AssetKind
	switch extension {
	case "zip":
		kind = KindPackage
	case "info":
		kind = KindInfo
	case "mod":
		kind = KindModule
	default:
		return GoPath{}, fmt.Errorf("unsupported Go module extension: %s", extension)
	}
	version, err := UnescapeVersion(escapedVersion)
	if err != nil {
		return GoPath{}, err
	}
	module, err := UnescapeModulePath(escapedModule)
	if err != nil {
		return GoPath{}, fmt.Errorf("invalid Go module path: %s", path)
	}
	if err := RequireVersionSuffix(module, version); err != nil {
		return GoPath{}, err
	}
	module, err = requireModule(escapedModule, path)
	if err != nil {
		return GoPath{}, err
	}
	return GoPath{Path: path, Module: module, Version: version, Kind: kind}, nil
}

// VersionedPath builds the canonical proxy path for a versioned kind.
func VersionedPath(module, version string, kind AssetKind) (GoPath, error) {
	if kind != KindPackage && kind != KindInfo && kind != KindModule {
		return GoPath{}, fmt.Errorf("Go path kind is not versioned: %s", kind)
	}
	canonicalModule, err := RequireModulePath(module)
	if err != nil {
		return GoPath{}, err
	}
	canonicalVersion, err := RequireCanonicalVersion(version)
	if err != nil {
		return GoPath{}, err
	}
	if err := RequireVersionSuffix(canonicalModule, canonicalVersion); err != nil {
		return GoPath{}, err
	}
	escapedModule, _ := EscapeModulePath(canonicalModule)
	escapedVersion, _ := EscapeVersion(canonicalVersion)
	extension, _ := kind.Extension()
	path := escapedModule + versionMarker + escapedVersion + "." + extension
	return GoPath{Path: path, Module: canonicalModule, Version: canonicalVersion, Kind: kind}, nil
}

// HasComponent reports whether the kind participates in module components.
func (p GoPath) HasComponent() bool {
	return p.Kind == KindPackage || p.Kind == KindInfo || p.Kind == KindModule
}

// IsMetadata reports whether the kind is generated metadata rather than content.
func (p GoPath) IsMetadata() bool {
	return p.Kind != KindPackage
}

// FileName returns the final path segment.
func (p GoPath) FileName() string {
	if slash := strings.LastIndexByte(p.Path, '/'); slash >= 0 {
		return p.Path[slash+1:]
	}
	return p.Path
}

// ContentType is the media type used for hosted repositories.
func (p GoPath) ContentType() string {
	switch p.Kind {
	case KindPackage:
		return "application/zip"
	case KindInfo:
		return "application/json"
	case KindLatest, KindList, KindModule:
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// ProxyContentType preserves the upstream media types for cached metadata.
func (p GoPath) ProxyContentType() string {
	switch p.Kind {
	case KindList:
		return "application/json"
	case KindInfo:
		return "text/plain"
	default:
		return p.ContentType()
	}
}

func requireModule(escapedModule, path string) (string, error) {
	if escapedModule == "" || strings.TrimSpace(escapedModule) == "" || strings.Contains(escapedModule, "@v") {
		return "", fmt.Errorf("invalid Go module path: %s", path)
	}
	module, err := UnescapeModulePath(escapedModule)
	if err != nil {
		return "", fmt.Errorf("invalid Go module path: %s", path)
	}
	return module, nil
}
