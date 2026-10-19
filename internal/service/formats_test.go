package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"kkrepo-go/internal/signing"
	"kkrepo-go/internal/store"
)

func formatRepo(t *testing.T, svc *Service, name, format, repoType string) *store.Repository {
	t.Helper()
	return createRepo(t, svc.store, &store.Repository{
		Name: name, Recipe: format + "-" + repoType, Format: format, Type: repoType,
		Online: true,
		Hosted: &store.HostedSettings{WritePolicy: "ALLOW"},
	})
}

func serveFormat(t *testing.T, svc *Service, repo *store.Repository, request *Request) (*Response, []byte) {
	t.Helper()
	response, err := svc.serveRepository(repo, request)
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.Path, err)
	}
	defer response.Close()
	var body []byte
	if response.Body != nil {
		body, err = io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
	}
	return response, body
}

func TestMavenHostedAndGeneratedMetadata(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "maven-hosted", "maven", "hosted")

	if response, _ := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "com/acme/demo/1.0.0/demo-1.0.0.jar",
		Header: http.Header{}, Body: strings.NewReader("jar-content"),
	}); response.Status != http.StatusCreated {
		t.Fatalf("upload status = %d", response.Status)
	}
	_, body := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "com/acme/demo/1.0.0/demo-1.0.0.jar", Header: http.Header{},
	})
	if string(body) != "jar-content" {
		t.Fatalf("download = %q", body)
	}
	_, metadata := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "com/acme/demo/maven-metadata.xml", Header: http.Header{},
	})
	if !strings.Contains(string(metadata), "<version>1.0.0</version>") {
		t.Fatalf("metadata = %s", metadata)
	}
}

func TestHelmIndexGeneration(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "helm-hosted", "helm", "hosted")
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "demo-chart-1.2.3.tgz",
		Header: http.Header{}, Body: strings.NewReader("chart-bytes"),
	})
	_, index := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "index.yaml", Header: http.Header{}, BaseURL: "http://example.test",
	})
	for _, expected := range []string{"entries:", "demo-chart:", "version: 1.2.3", "/repository/helm-hosted/demo-chart-1.2.3.tgz", "digest:"} {
		if !strings.Contains(string(index), expected) {
			t.Fatalf("index missing %q:\n%s", expected, index)
		}
	}
}

func TestCargoPublishIndexAndDownload(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "cargo-hosted", "cargo", "hosted")
	metadata, _ := json.Marshal(map[string]any{
		"name": "mycrate", "vers": "0.1.0", "deps": []any{}, "features": map[string]any{},
	})
	crate := []byte("crate-bytes")
	var payload bytes.Buffer
	_ = binary.Write(&payload, binary.LittleEndian, uint32(len(metadata)))
	payload.Write(metadata)
	_ = binary.Write(&payload, binary.LittleEndian, uint32(len(crate)))
	payload.Write(crate)
	response, _ := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "api/v1/crates/new", Header: http.Header{}, Body: &payload,
	})
	if response.Status != http.StatusOK {
		t.Fatalf("publish status = %d", response.Status)
	}
	_, index := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "my/cr/mycrate", Header: http.Header{},
	})
	if !strings.Contains(string(index), `"name":"mycrate"`) || !strings.Contains(string(index), `"vers":"0.1.0"`) {
		t.Fatalf("index = %s", index)
	}
	_, downloaded := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "api/v1/crates/mycrate/0.1.0/download", Header: http.Header{},
	})
	if string(downloaded) != "crate-bytes" {
		t.Fatalf("crate = %q", downloaded)
	}
	_, config := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "config.json", Header: http.Header{}, BaseURL: "http://example.test",
	})
	if !strings.Contains(string(config), `"dl":"http://example.test/repository/cargo-hosted/api/v1/crates"`) {
		t.Fatalf("config = %s", config)
	}
}

func TestNpmPublishAndDistTags(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "npm-hosted", "npm", "hosted")
	tarball := []byte("npm-tarball")
	document := map[string]any{
		"name": "demo", "dist-tags": map[string]string{"latest": "1.0.0"},
		"versions": map[string]any{"1.0.0": map[string]any{
			"name": "demo", "version": "1.0.0",
			"dist": map[string]string{"tarball": "http://upstream/demo/-/demo-1.0.0.tgz"},
		}},
		"_attachments": map[string]any{"demo-1.0.0.tgz": map[string]any{
			"content_type": "application/octet-stream",
			"data":         base64.StdEncoding.EncodeToString(tarball),
			"length":       len(tarball),
		}},
	}
	encoded, _ := json.Marshal(document)
	response, _ := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "demo", Header: http.Header{}, Body: bytes.NewReader(encoded),
		BaseURL: "http://example.test",
	})
	if response.Status != http.StatusCreated {
		t.Fatalf("publish status = %d", response.Status)
	}
	_, metadata := serveFormat(t, svc, repo, &Request{Method: http.MethodGet, Path: "demo", Header: http.Header{}})
	if strings.Contains(string(metadata), "_attachments") {
		t.Fatalf("packument leaked attachments: %s", metadata)
	}
	if !strings.Contains(string(metadata), "http://example.test/repository/npm-hosted/demo/-/demo-1.0.0.tgz") {
		t.Fatalf("tarball URL not rewritten: %s", metadata)
	}
	_, tarballBody := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "demo/-/demo-1.0.0.tgz", Header: http.Header{},
	})
	if string(tarballBody) != "npm-tarball" {
		t.Fatalf("tarball = %q", tarballBody)
	}
	_, tags := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "-/package/demo/dist-tags", Header: http.Header{},
	})
	if !strings.Contains(string(tags), `"latest":"1.0.0"`) {
		t.Fatalf("dist-tags = %s", tags)
	}
}

func TestPypiUploadAndSimpleIndex(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "pypi-hosted", "pypi", "hosted")
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	_ = writer.WriteField(":action", "file_upload")
	_ = writer.WriteField("name", "Demo_Pkg")
	_ = writer.WriteField("version", "1.0.0")
	part, _ := writer.CreateFormFile("content", "demo_pkg-1.0.0.tar.gz")
	_, _ = part.Write([]byte("sdist-bytes"))
	_ = writer.Close()
	response, _ := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPost, Path: "", BaseURL: "http://example.test",
		Header: http.Header{"Content-Type": []string{writer.FormDataContentType()}}, Body: body,
	})
	if response.Status != http.StatusOK {
		t.Fatalf("upload status = %d", response.Status)
	}
	_, index := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "simple/demo-pkg/", Header: http.Header{},
	})
	if !strings.Contains(string(index), "demo_pkg-1.0.0.tar.gz") || !strings.Contains(string(index), "#md5=") {
		t.Fatalf("simple index = %s", index)
	}
	_, file := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "packages/demo-pkg/1.0.0/demo_pkg-1.0.0.tar.gz", Header: http.Header{},
	})
	if string(file) != "sdist-bytes" {
		t.Fatalf("file = %q", file)
	}
}

func TestDockerPushAndPull(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "docker-hosted", "docker", "hosted")
	blob := []byte("layer-bytes")
	digest := "sha256:" + sha256Hex(blob)
	start, _ := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPost, Path: "v2/demo/app/blobs/uploads/", Header: http.Header{}, BaseURL: "http://example.test",
	})
	if start.Status != http.StatusAccepted {
		t.Fatalf("start status = %d", start.Status)
	}
	location := start.Header.Get("Location")
	id := location[strings.LastIndexByte(location, '/')+1:]
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPatch, Path: "v2/demo/app/blobs/uploads/" + id, Header: http.Header{}, Body: bytes.NewReader(blob),
	})
	finished, _ := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "v2/demo/app/blobs/uploads/" + id,
		Query: map[string][]string{"digest": {digest}}, Header: http.Header{}, BaseURL: "http://example.test",
	})
	if finished.Status != http.StatusCreated {
		t.Fatalf("finish status = %d", finished.Status)
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digest, "size": len(blob)},
		"layers": []any{},
	})
	put, _ := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "v2/demo/app/manifests/1.0", Header: http.Header{
			"Content-Type": []string{"application/vnd.oci.image.manifest.v1+json"},
		}, Body: bytes.NewReader(manifest), BaseURL: "http://example.test",
	})
	if put.Status != http.StatusCreated || put.Header.Get("Docker-Content-Digest") == "" {
		t.Fatalf("manifest status = %d", put.Status)
	}
	_, fetched := serveFormat(t, svc, repo, &Request{Method: http.MethodGet, Path: "v2/demo/app/manifests/1.0", Header: http.Header{}})
	if !bytes.Equal(fetched, manifest) {
		t.Fatal("manifest round trip mismatch")
	}
	_, pulled := serveFormat(t, svc, repo, &Request{Method: http.MethodGet, Path: "v2/demo/app/blobs/" + digest, Header: http.Header{}})
	if !bytes.Equal(pulled, blob) {
		t.Fatal("blob round trip mismatch")
	}
	_, tags := serveFormat(t, svc, repo, &Request{Method: http.MethodGet, Path: "v2/demo/app/tags/list", Header: http.Header{}})
	if !strings.Contains(string(tags), `"tags":["1.0"]`) {
		t.Fatalf("tags = %s", tags)
	}
}

func TestGitLFSBatchAndUpload(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "lfs-hosted", "gitlfs", "hosted")
	content := []byte("lfs-object")
	oid := sha256Hex(content)
	batch, _ := json.Marshal(map[string]any{
		"operation": "upload", "objects": []map[string]any{{"oid": oid, "size": len(content)}},
	})
	response, body := serveFormat(t, svc, repo, &Request{
		Method: http.MethodPost, Path: "info/lfs/objects/batch", Header: http.Header{},
		Body: bytes.NewReader(batch), BaseURL: "http://example.test",
	})
	if response.Status != http.StatusOK || !strings.Contains(string(body), `"upload"`) {
		t.Fatalf("batch = %d %s", response.Status, body)
	}
	serveFormat(t, svc, repo, &Request{Method: http.MethodPut, Path: oid, Header: http.Header{}, Body: bytes.NewReader(content)})
	_, download := serveFormat(t, svc, repo, &Request{Method: http.MethodGet, Path: oid, Header: http.Header{}})
	if string(download) != "lfs-object" {
		t.Fatalf("download = %q", download)
	}
}

func TestCondaRepodataGeneration(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "conda-hosted", "conda", "hosted")
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "main/linux-64/demo-1.0.0-0.conda",
		Header: http.Header{}, Body: strings.NewReader("conda-package"),
	})
	_, body := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "main/linux-64/repodata.json", Header: http.Header{},
	})
	var document struct {
		Info     map[string]any            `json:"info"`
		Packages map[string]map[string]any `json:"packages.conda"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	entry := document.Packages["demo-1.0.0-0.conda"]
	if entry == nil || entry["name"] != "demo" || entry["version"] != "1.0.0" || entry["build"] != "0" {
		t.Fatalf("conda entry = %+v", document)
	}
	if document.Info["subdir"] != "linux-64" {
		t.Fatalf("subdir = %v", document.Info)
	}
}

func TestYumRepodataGeneration(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "yum-hosted", "yum", "hosted")
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "base/x86_64/demo-1.0.0-1.x86_64.rpm",
		Header: http.Header{}, Body: strings.NewReader("rpm-bytes"),
	})
	_, repomd := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "base/x86_64/repodata/repomd.xml", Header: http.Header{},
	})
	if !strings.Contains(string(repomd), "primary") || !strings.Contains(string(repomd), "repodata/primary.xml.gz") {
		t.Fatalf("repomd = %s", repomd)
	}
	_, gz := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "base/x86_64/repodata/primary.xml.gz", Header: http.Header{},
	})
	reader, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	primary, _ := io.ReadAll(reader)
	reader.Close()
	for _, expected := range []string{"<name>demo</name>", `ver="1.0.0"`, `rel="1"`, `href="base/x86_64/demo-1.0.0-1.x86_64.rpm"`} {
		if !strings.Contains(string(primary), expected) {
			t.Fatalf("primary missing %q:\n%s", expected, primary)
		}
	}
}

func TestAptPackagesAndRelease(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "apt-hosted", "apt", "hosted")
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "pool/main/d/demo/demo_1.0.0_amd64.deb",
		Header: http.Header{}, Body: strings.NewReader("deb-bytes"),
	})
	_, packages := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "dists/stable/main/binary-amd64/Packages", Header: http.Header{},
	})
	for _, expected := range []string{"Package: demo", "Version: 1.0.0", "Architecture: amd64", "Filename: pool/main/d/demo/demo_1.0.0_amd64.deb", "SHA256:"} {
		if !strings.Contains(string(packages), expected) {
			t.Fatalf("Packages missing %q:\n%s", expected, packages)
		}
	}
	_, release := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "dists/stable/Release", Header: http.Header{},
	})
	if !strings.Contains(string(release), "main/binary-amd64/Packages") || !strings.Contains(string(release), "SHA256:") {
		t.Fatalf("Release = %s", release)
	}
}

func TestAlpineIndexGeneration(t *testing.T) {
	svc := newTestService(t)
	repo := formatRepo(t, svc, "alpine-hosted", "alpine", "hosted")
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "v3.20/main/x86_64/demo-1.0.0-r0.apk",
		Header: http.Header{}, Body: strings.NewReader("apk-bytes"),
	})
	_, indexGz := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "v3.20/main/x86_64/APKINDEX.tar.gz", Header: http.Header{},
	})
	gzipReader, err := gzip.NewReader(bytes.NewReader(indexGz))
	if err != nil {
		t.Fatal(err)
	}
	tarReader := tar.NewReader(gzipReader)
	var index []byte
	for {
		header, err := tarReader.Next()
		if err != nil {
			break
		}
		if header.Name == "APKINDEX" {
			index, _ = io.ReadAll(tarReader)
		}
	}
	gzipReader.Close()
	for _, expected := range []string{"P:demo", "V:1.0.0-r0", "A:x86_64"} {
		if !strings.Contains(string(index), expected) {
			t.Fatalf("APKINDEX missing %q:\n%s", expected, index)
		}
	}
}

func TestAPTSignedMetadata(t *testing.T) {
	svc := newTestService(t)
	aptKey, err := signing.LoadOrCreateAPT(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alpineKey, err := signing.LoadOrCreateAlpine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetSigningKeys(aptKey, alpineKey)
	repo := formatRepo(t, svc, "apt-signed", "apt", "hosted")
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "pool/main/d/demo/demo_1.0.0_amd64.deb",
		Header: http.Header{}, Body: strings.NewReader("deb-bytes"),
	})
	_, inRelease := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "dists/stable/InRelease", Header: http.Header{},
	})
	if !strings.HasPrefix(string(inRelease), "-----BEGIN PGP SIGNED MESSAGE-----") {
		t.Fatalf("InRelease = %.80s", inRelease)
	}
	_, detached := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "dists/stable/Release.gpg", Header: http.Header{},
	})
	if !strings.HasPrefix(string(detached), "-----BEGIN PGP SIGNATURE-----") {
		t.Fatalf("Release.gpg = %.80s", detached)
	}
	_, publicKey := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "gpg.key", Header: http.Header{},
	})
	if !strings.Contains(string(publicKey), "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Fatal("APT public key missing")
	}
}

func TestAlpineSignedIndex(t *testing.T) {
	svc := newTestService(t)
	alpineKey, err := signing.LoadOrCreateAlpine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetSigningKeys(nil, alpineKey)
	repo := formatRepo(t, svc, "alpine-signed", "alpine", "hosted")
	serveFormat(t, svc, repo, &Request{
		Method: http.MethodPut, Path: "v3.20/main/x86_64/demo-1.0.0-r0.apk",
		Header: http.Header{}, Body: strings.NewReader("apk-bytes"),
	})
	_, signed := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "v3.20/main/x86_64/APKINDEX.tar.gz", Header: http.Header{},
	})
	gzipReader, err := gzip.NewReader(bytes.NewReader(signed))
	if err != nil {
		t.Fatal(err)
	}
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(header.Name, ".SIGN.RSA.") {
		t.Fatalf("first entry = %s", header.Name)
	}
	if header.Size == 0 || !strings.HasSuffix(header.Name, "kkrepo-go.rsa.pub") {
		t.Fatalf("signature entry = %+v", header)
	}
	gzipReader.Close()
	_, publicKey := serveFormat(t, svc, repo, &Request{
		Method: http.MethodGet, Path: "alpine.rsa.pub", Header: http.Header{},
	})
	if !strings.Contains(string(publicKey), "BEGIN PUBLIC KEY") {
		t.Fatal("Alpine public key missing")
	}
}
