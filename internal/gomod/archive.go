package gomod

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxCompressedBytes     = 500 << 20
	MaxExpandedBytes       = 500 << 20
	MaxGoModOrLicenseBytes = 16 << 20
	MaxArchiveEntries      = 100_000
)

var moduleDirectiveRE = regexp.MustCompile(`(?m)^[\t ]*module[\t ]+(?:"([^"\r\n]+)"|([^\s]+))[\t ]*(?://.*)?$`)

// Inspected is the result of validating a module zip.
type Inspected struct {
	Module  string
	Version string
	GoMod   []byte
}

// InspectModuleArchive validates the official Go module zip layout and
// extracts the effective root go.mod. It mirrors GoModuleArchiveInspector.
func InspectModuleArchive(archivePath, requestedVersion string) (Inspected, error) {
	version, err := RequireCanonicalVersion(requestedVersion)
	if err != nil {
		return Inspected{}, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return Inspected{}, fmt.Errorf("unable to inspect Go module archive: %w", err)
	}
	if info.Size() == 0 {
		return Inspected{}, badArchive("Go module archive is empty")
	}
	if info.Size() > MaxCompressedBytes {
		return Inspected{}, badArchive("Go module archive exceeds 500 MiB")
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return Inspected{}, badArchive("Go module archive is not a valid zip file")
	}
	defer reader.Close()

	root := ""
	module := ""
	var expanded int64
	var goMod []byte
	collisions := newPathCollisionChecker()

	for index, entry := range reader.File {
		if index+1 > MaxArchiveEntries {
			return Inspected{}, badArchive("Go module archive contains too many entries")
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return Inspected{}, badArchive("Go module archive must contain only regular files and directories")
		}
		directory := strings.HasSuffix(entry.Name, "/") || entry.FileInfo().IsDir()
		if !directory && !entry.Mode().IsRegular() && entry.Mode() != 0 {
			return Inspected{}, badArchive("Go module archive must contain only regular files and directories")
		}
		if !utf8.ValidString(entry.Name) {
			return Inspected{}, badArchive("Go module archive contains an unsafe path")
		}
		name, err := safeArchiveName(entry.Name)
		if err != nil {
			return Inspected{}, err
		}
		coordinateRoot, err := archiveCoordinateRoot(name)
		if err != nil {
			return Inspected{}, err
		}
		if root == "" {
			root = coordinateRoot
			module, err = archiveCoordinate(root, requestedVersion)
			if err != nil {
				return Inspected{}, err
			}
		} else if root != coordinateRoot {
			return Inspected{}, badArchive("Go module archive must contain exactly one top-level module directory")
		}

		normalizedName := strings.TrimRight(name, "/")
		if normalizedName == root {
			if !directory {
				return Inspected{}, badArchive("Go module files must be below the <module>@<version> directory")
			}
			continue
		}
		if !strings.HasPrefix(normalizedName, root+"/") {
			return Inspected{}, badArchive("Go module files must be below the <module>@<version> directory")
		}
		relative := normalizedName[len(root)+1:]
		if err := requireArchiveFilePath(relative); err != nil {
			return Inspected{}, err
		}
		if err := collisions.add(relative, directory); err != nil {
			return Inspected{}, err
		}
		if directory {
			continue
		}
		remaining := MaxExpandedBytes - expanded
		if remaining < 0 {
			remaining = 0
		}
		var read int64
		if relative == "go.mod" {
			if goMod != nil {
				return Inspected{}, badArchive("Go module archive contains more than one root go.mod")
			}
			limit := min64(MaxGoModOrLicenseBytes, remaining)
			goMod, read, err = readEntryBounded(entry, limit, "go.mod")
			if err != nil {
				return Inspected{}, err
			}
		} else {
			leaf := relative
			if slash := strings.LastIndexByte(relative, '/'); slash >= 0 {
				leaf = relative[slash+1:]
			}
			if strings.EqualFold(leaf, "go.mod") {
				return Inspected{}, badArchive("Go module archive must contain only one correctly-cased root go.mod")
			}
			limit := int64(MaxExpandedBytes)
			if relative == "LICENSE" {
				limit = MaxGoModOrLicenseBytes
			}
			if remaining < limit {
				limit = remaining
			}
			read, err = drainEntryBounded(entry, limit, relative)
			if err != nil {
				return Inspected{}, err
			}
		}
		if entry.UncompressedSize64 != 0 && int64(entry.UncompressedSize64) != read {
			return Inspected{}, badArchive("Go module archive entry size does not match its ZIP header: " + relative)
		}
		expanded += read
		if expanded > MaxExpandedBytes {
			return Inspected{}, badArchive("Go module archive expands beyond 500 MiB")
		}
	}
	if root == "" || module == "" {
		return Inspected{}, badArchive("Go module archive contains no entries")
	}
	effectiveGoMod := goMod
	if effectiveGoMod == nil {
		effectiveGoMod = []byte("module " + module + "\n")
	} else {
		if !utf8.Valid(effectiveGoMod) {
			return Inspected{}, badArchive("Root go.mod must be valid UTF-8")
		}
		effectiveGoMod, err = validateGoMod(module, effectiveGoMod)
		if err != nil {
			return Inspected{}, err
		}
	}
	return Inspected{Module: module, Version: version, GoMod: effectiveGoMod}, nil
}

func archiveCoordinate(root, requestedVersion string) (string, error) {
	separator := strings.LastIndexByte(root, '@')
	if separator <= 0 || separator == len(root)-1 {
		return "", badArchive("Top-level directory must be <module>@<version>")
	}
	module, err := RequireModulePath(root[:separator])
	if err != nil {
		return "", err
	}
	version, err := RequireCanonicalVersion(root[separator+1:])
	if err != nil {
		return "", err
	}
	if err := RequireVersionSuffix(module, version); err != nil {
		return "", err
	}
	if version != requestedVersion {
		return "", badArchive(fmt.Sprintf("Archive version %s does not match upload version %s", version, requestedVersion))
	}
	return module, nil
}

func validateGoMod(module string, contents []byte) ([]byte, error) {
	match := moduleDirectiveRE.FindSubmatch(contents)
	if match == nil {
		return nil, badArchive("Root go.mod is missing a module directive")
	}
	declared := string(match[1])
	if declared == "" {
		declared = string(match[2])
	}
	if declared != module {
		return nil, badArchive("go.mod module directive does not match archive root: " + declared)
	}
	return contents, nil
}

func safeArchiveName(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || strings.ContainsRune(raw, 0) || strings.Contains(raw, "\\") ||
		strings.HasPrefix(raw, "/") || strings.Contains(raw, "//") {
		return "", badArchive("Go module archive contains an unsafe path")
	}
	normalized := strings.TrimRight(raw, "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", badArchive("Go module archive contains an unsafe path: " + raw)
		}
	}
	return raw, nil
}

func archiveCoordinateRoot(name string) (string, error) {
	normalized := strings.TrimRight(name, "/")
	separator := strings.IndexByte(normalized, '@')
	if separator <= 0 {
		return "", badArchive("Top-level directory must be <module>@<version>")
	}
	rest := normalized[separator+1:]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return normalized[:separator+1+slash], nil
	}
	return normalized, nil
}

func requireArchiveFilePath(relative string) error {
	if strings.TrimSpace(relative) == "" || strings.HasPrefix(relative, "/") ||
		strings.HasSuffix(relative, "/") || strings.Contains(relative, "//") {
		return badArchive("Go module archive contains an invalid file path: " + relative)
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || onlyDots(segment) || strings.HasSuffix(segment, ".") {
			return badArchive("Go module archive contains an invalid file path: " + relative)
		}
		base := segment
		if dot := strings.IndexByte(base, '.'); dot >= 0 {
			base = base[:dot]
		}
		if _, reserved := windowsReserved[strings.ToLower(base)]; reserved {
			return badArchive("Go module archive contains a Windows-reserved file path: " + relative)
		}
		for _, ch := range segment {
			if !archiveFileNameCharacter(ch) {
				return badArchive("Go module archive contains an invalid file path: " + relative)
			}
		}
	}
	return nil
}

func archiveFileNameCharacter(ch rune) bool {
	if ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' {
		return true
	}
	if ch < 128 {
		return strings.ContainsRune("!#$%&()+,-.=@[]^_{}~ ", ch)
	}
	return unicode.IsLetter(ch)
}

type pathInfo struct {
	path      string
	directory bool
}

type pathCollisionChecker struct {
	paths map[string]pathInfo
}

func newPathCollisionChecker() *pathCollisionChecker {
	return &pathCollisionChecker{paths: make(map[string]pathInfo)}
}

func (c *pathCollisionChecker) add(path string, directory bool) error {
	folded := strings.ToUpper(path)
	folded = strings.ToLower(folded)
	prior, exists := c.paths[folded]
	if !exists {
		c.paths[folded] = pathInfo{path: path, directory: directory}
	} else if prior.path != path {
		return badArchive("Go module archive contains case-conflicting paths: " + prior.path + " and " + path)
	} else if prior.directory != directory {
		return badArchive("Go module archive path is both a file and directory: " + path)
	} else if !directory {
		return badArchive("Go module archive contains duplicate file: " + path)
	}
	if separator := strings.LastIndexByte(path, '/'); separator > 0 {
		return c.add(path[:separator], true)
	}
	return nil
}

func readEntryBounded(entry *zip.File, limit int64, name string) ([]byte, int64, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, 0, badArchive("Go module archive contains an unreadable entry: " + name)
	}
	defer reader.Close()
	var builder strings.Builder
	if limit < int64(entry.UncompressedSize64) {
		return nil, 0, badArchive("Go module archive entry is too large: " + name)
	}
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return nil, 0, badArchive("Go module archive entry is too large: " + name)
			}
			builder.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, badArchive("Go module archive entry could not be read: " + name)
		}
	}
	return []byte(builder.String()), total, nil
}

func drainEntryBounded(entry *zip.File, limit int64, name string) (int64, error) {
	reader, err := entry.Open()
	if err != nil {
		return 0, badArchive("Go module archive contains an unreadable entry: " + name)
	}
	defer reader.Close()
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return 0, badArchive("Go module archive entry is too large: " + name)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, badArchive("Go module archive entry could not be read: " + name)
		}
	}
	return total, nil
}

func badArchive(message string) error {
	return fmt.Errorf("%s", message)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
