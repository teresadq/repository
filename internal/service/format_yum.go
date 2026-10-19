package service

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"kkrepo-go/internal/store"
)

type yumPackage struct {
	name, version, release, arch, path, checksum string
	size, buildTime                              int64
}

// serveYum generates repodata for hosted repositories.
func (s *Service) serveYum(repository *store.Repository, request *Request) (*Response, error) {
	path := strings.TrimLeft(request.Path, "/")
	if repository.Type == "hosted" && (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
		strings.Contains(path, "repodata/") {
		base, file := splitYumPath(path)
		packages := s.yumPackages(repository, base, s.now())
		switch file {
		case "repomd.xml":
			body := yumRepomd(packages, s.now())
			return bytesResponse(http.StatusOK, "application/xml", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
		case "primary.xml.gz":
			body := gzipBytes([]byte(yumPrimaryXML(packages)))
			return bytesResponse(http.StatusOK, "application/x-gzip", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
		case "filelists.xml.gz":
			body := gzipBytes([]byte(yumFilelistsXML(len(packages))))
			return bytesResponse(http.StatusOK, "application/x-gzip", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
		case "other.xml.gz":
			body := gzipBytes([]byte(yumOtherXML(len(packages))))
			return bytesResponse(http.StatusOK, "application/x-gzip", body, sha256Hex(body), time.Time{}, request.HeadOnly), nil
		}
	}
	return s.serveGeneric(repository, request)
}

func splitYumPath(path string) (base, file string) {
	index := strings.Index(path, "repodata/")
	if index < 0 {
		return "", path
	}
	base = strings.TrimSuffix(path[:index], "/")
	file = path[index+len("repodata/"):]
	return base, file
}

func (s *Service) yumPackages(repository *store.Repository, base string, now time.Time) []yumPackage {
	assets := s.store.ListAssets(repository.Name, ".rpm")
	prefix := ""
	if base != "" {
		prefix = base + "/"
	}
	out := make([]yumPackage, 0)
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Path, prefix) || !strings.HasSuffix(asset.Path, ".rpm") {
			continue
		}
		name, version, release, arch, ok := parseRPMFilename(asset.Path)
		if !ok {
			continue
		}
		buildTime := asset.UpdatedAt.Unix()
		if buildTime <= 0 {
			buildTime = now.Unix()
		}
		out = append(out, yumPackage{
			name: name, version: version, release: release, arch: arch,
			path: asset.Path, checksum: asset.SHA256, size: asset.Size, buildTime: buildTime,
		})
	}
	return out
}

func parseRPMFilename(path string) (string, string, string, string, bool) {
	filename := path[strings.LastIndexByte(path, '/')+1:]
	base := strings.TrimSuffix(filename, ".rpm")
	dot := strings.LastIndexByte(base, '.')
	if dot <= 0 {
		return "", "", "", "", false
	}
	arch := base[dot+1:]
	rest := base[:dot]
	last := strings.LastIndexByte(rest, '-')
	if last <= 0 {
		return "", "", "", "", false
	}
	release := rest[last+1:]
	rest = rest[:last]
	second := strings.LastIndexByte(rest, '-')
	if second <= 0 {
		return "", "", "", "", false
	}
	return rest[:second], rest[second+1:], release, arch, true
}

func yumRepomd(packages []yumPackage, now time.Time) []byte {
	primary := []byte(yumPrimaryXML(packages))
	filelists := []byte(yumFilelistsXML(len(packages)))
	other := []byte(yumOtherXML(len(packages)))
	timestamp := now.Unix()
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	builder.WriteString(`<repomd xmlns="http://linux.duke.edu/metadata/repo">` + "\n")
	fmt.Fprintf(&builder, "  <revision>%d</revision>\n", timestamp)
	writeYumData(&builder, "primary", "repodata/primary.xml.gz", primary, timestamp)
	writeYumData(&builder, "filelists", "repodata/filelists.xml.gz", filelists, timestamp)
	writeYumData(&builder, "other", "repodata/other.xml.gz", other, timestamp)
	builder.WriteString("</repomd>\n")
	return []byte(builder.String())
}

func writeYumData(builder *strings.Builder, kind, href string, content []byte, timestamp int64) {
	gz := gzipBytes(content)
	sum := sha256.Sum256(gz)
	openSum := sha256.Sum256(content)
	fmt.Fprintf(builder, "  <data type=%q>\n", kind)
	fmt.Fprintf(builder, "    <checksum type=\"sha256\">%s</checksum>\n", hex.EncodeToString(sum[:]))
	fmt.Fprintf(builder, "    <open-checksum type=\"sha256\">%s</open-checksum>\n", hex.EncodeToString(openSum[:]))
	fmt.Fprintf(builder, "    <location href=%q/>\n", href)
	fmt.Fprintf(builder, "    <timestamp>%d</timestamp>\n", timestamp)
	fmt.Fprintf(builder, "    <size>%d</size>\n    <open-size>%d</open-size>\n", len(gz), len(content))
	builder.WriteString("  </data>\n")
}

func yumPrimaryXML(packages []yumPackage) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&builder, `<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">`+"\n", len(packages))
	for _, pkg := range packages {
		builder.WriteString("  <package type=\"rpm\">\n")
		fmt.Fprintf(&builder, "    <name>%s</name>\n    <arch>%s</arch>\n", html.EscapeString(pkg.name), html.EscapeString(pkg.arch))
		fmt.Fprintf(&builder, "    <version epoch=\"0\" ver=%q rel=%q/>\n", pkg.version, pkg.release)
		fmt.Fprintf(&builder, "    <checksum type=\"sha256\" pkgid=\"YES\">%s</checksum>\n", pkg.checksum)
		fmt.Fprintf(&builder, "    <summary>%s</summary>\n    <description>%s</description>\n", html.EscapeString(pkg.name), html.EscapeString(pkg.name))
		builder.WriteString("    <packager></packager>\n    <url></url>\n")
		fmt.Fprintf(&builder, "    <time file=\"%d\" build=\"%d\"/>\n", pkg.buildTime, pkg.buildTime)
		fmt.Fprintf(&builder, "    <size package=\"%d\" installed=\"0\" archive=\"%d\"/>\n", pkg.size, pkg.size)
		fmt.Fprintf(&builder, "    <location href=%q/>\n", pkg.path)
		builder.WriteString("    <format>\n      <rpm:license></rpm:license>\n      <rpm:vendor></rpm:vendor>\n")
		builder.WriteString("      <rpm:group></rpm:group>\n      <rpm:buildhost>kkrepo-go</rpm:buildhost>\n")
		builder.WriteString("      <rpm:sourcerpm></rpm:sourcerpm>\n      <rpm:header-range start=\"0\" end=\"0\"/>\n")
		builder.WriteString("      <rpm:provides/>\n      <rpm:requires/>\n    </format>\n  </package>\n")
	}
	builder.WriteString("</metadata>\n")
	return builder.String()
}

func yumFilelistsXML(packages int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<filelists xmlns="http://linux.duke.edu/metadata/filelists" packages="%d">`+"\n</filelists>\n", packages)
}

func yumOtherXML(packages int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<otherdata xmlns="http://linux.duke.edu/metadata/other" packages="%d">`+"\n</otherdata>\n", packages)
}

func gzipBytes(content []byte) []byte {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, _ = writer.Write(content)
	_ = writer.Close()
	return buffer.Bytes()
}
