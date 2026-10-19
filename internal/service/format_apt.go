package service

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type aptPackage struct {
	name, version, arch, filename string
	size                          int64
	sha256, md5                   string
}

// serveApt generates Packages/Release metadata for hosted repositories.
func (s *Service) serveApt(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" && (request.Method == http.MethodGet || request.Method == http.MethodHead) {
		if path == "gpg.key" || strings.HasSuffix(path, "/gpg.key") {
			if s.aptSigner == nil {
				return nil, &Error{Status: http.StatusNotImplemented, Message: "APT signing key is not configured"}
			}
			return bytesResponse(http.StatusOK, "application/pgp-keys", s.aptSigner.PublicArmor(), "", time.Time{}, request.HeadOnly), nil
		}
		if strings.Contains(path, "/dists/") || strings.HasPrefix(path, "dists/") {
			index := strings.Index(path, "dists/")
			rest := path[index+len("dists/"):]
			parts := strings.Split(rest, "/")
			if len(parts) >= 4 && strings.HasPrefix(parts[2], "binary-") {
				arch := strings.TrimPrefix(parts[2], "binary-")
				switch parts[3] {
				case "Packages":
					body := []byte(s.aptPackages(repository, parts[1], arch))
					return bytesResponse(http.StatusOK, "text/plain; charset=utf-8", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
				case "Packages.gz":
					body := gzipBytes([]byte(s.aptPackages(repository, parts[1], arch)))
					return bytesResponse(http.StatusOK, "application/gzip", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
				}
			}
			if len(parts) >= 2 && (parts[1] == "Release" || parts[1] == "InRelease" || parts[1] == "Release.gpg") {
				release := []byte(s.aptRelease(repository, parts[0]))
				switch parts[1] {
				case "Release":
					return bytesResponse(http.StatusOK, "text/plain; charset=utf-8", release, sha256Hex(release), time.Time{}, request.HeadOnly), nil
				case "InRelease":
					if s.aptSigner == nil {
						return nil, &Error{Status: http.StatusNotImplemented, Message: "APT signing key is not configured"}
					}
					inRelease, _, err := s.aptSigner.SignRelease(release)
					if err != nil {
						return nil, err
					}
					return bytesResponse(http.StatusOK, "text/plain; charset=utf-8", inRelease, sha256Hex(inRelease), time.Time{}, request.HeadOnly), nil
				case "Release.gpg":
					if s.aptSigner == nil {
						return nil, &Error{Status: http.StatusNotImplemented, Message: "APT signing key is not configured"}
					}
					_, detached, err := s.aptSigner.SignRelease(release)
					if err != nil {
						return nil, err
					}
					return bytesResponse(http.StatusOK, "application/pgp-signature", detached, sha256Hex(detached), time.Time{}, request.HeadOnly), nil
				}
			}
		}
	}
	return s.serveGeneric(repository, request)
}

func (s *Service) aptPackages(repository *store.Repository, component, arch string) string {
	packages := s.aptPackageEntries(repository)
	var builder strings.Builder
	for _, pkg := range packages {
		if arch != "" && pkg.arch != arch {
			continue
		}
		fmt.Fprintf(&builder, "Package: %s\nVersion: %s\nArchitecture: %s\n", pkg.name, pkg.version, pkg.arch)
		fmt.Fprintf(&builder, "Filename: %s\nSize: %d\n", pkg.filename, pkg.size)
		if pkg.md5 != "" {
			fmt.Fprintf(&builder, "MD5sum: %s\n", pkg.md5)
		}
		if pkg.sha256 != "" {
			fmt.Fprintf(&builder, "SHA256: %s\n", pkg.sha256)
		}
		fmt.Fprintf(&builder, "Description: %s\n\n", pkg.name)
	}
	return builder.String()
}

func (s *Service) aptRelease(repository *store.Repository, dist string) string {
	packages := s.aptPackageEntries(repository)
	arches := map[string]struct{}{}
	for _, pkg := range packages {
		arches[pkg.arch] = struct{}{}
	}
	archList := make([]string, 0, len(arches))
	for arch := range arches {
		archList = append(archList, arch)
	}
	sort.Strings(archList)
	var builder strings.Builder
	fmt.Fprintf(&builder, "Origin: kkrepo-go\nLabel: kkrepo-go\nSuite: %s\nCodename: %s\n", dist, dist)
	fmt.Fprintf(&builder, "Architectures: %s\nComponents: main\n", strings.Join(archList, " "))
	fmt.Fprintf(&builder, "Date: %s\n", time.Now().UTC().Format(http.TimeFormat))
	builder.WriteString("SHA256:\n")
	for _, arch := range archList {
		content := []byte(s.aptPackages(repository, "main", arch))
		fmt.Fprintf(&builder, " %s %d main/binary-%s/Packages\n", sha256Hex(content), len(content), arch)
		gz := gzipBytes(content)
		fmt.Fprintf(&builder, " %s %d main/binary-%s/Packages.gz\n", sha256Hex(gz), len(gz), arch)
	}
	return builder.String()
}

func (s *Service) aptPackageEntries(repository *store.Repository) []aptPackage {
	assets := s.store.ListAssets(repository.Name, ".deb")
	out := make([]aptPackage, 0)
	for _, asset := range assets {
		if !strings.HasSuffix(asset.Path, ".deb") {
			continue
		}
		name, version, arch, ok := parseDebFilename(asset.Path)
		if !ok {
			continue
		}
		md5sum := asset.Attributes["md5"]
		if md5sum == "" {
			if blob, err := s.store.ReadBlob(asset); err == nil {
				sum := md5.Sum(blob)
				md5sum = hex.EncodeToString(sum[:])
			}
		}
		out = append(out, aptPackage{
			name: name, version: version, arch: arch, filename: asset.Path,
			size: asset.Size, sha256: asset.SHA256, md5: md5sum,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].version < out[j].version
	})
	return out
}

func parseDebFilename(path string) (string, string, string, bool) {
	filename := path[strings.LastIndexByte(path, '/')+1:]
	base := strings.TrimSuffix(filename, ".deb")
	last := strings.LastIndexByte(base, '_')
	if last <= 0 {
		return "", "", "", false
	}
	arch := base[last+1:]
	rest := base[:last]
	second := strings.LastIndexByte(rest, '_')
	if second <= 0 {
		return "", "", "", false
	}
	return rest[:second], rest[second+1:], arch, true
}
