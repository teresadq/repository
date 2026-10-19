package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kkrepo-go/internal/store"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(st)
}

func createRepo(t *testing.T, st *store.Store, repo *store.Repository) *store.Repository {
	t.Helper()
	if repo.Format == "" {
		repo.Format = "go"
	}
	created, err := st.CreateRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func readResponse(t *testing.T, response *Response) string {
	t.Helper()
	defer response.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestProxyCachesAndServesMetadataContent(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/example.com/demo/@v/list":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "v1.0.0\nv1.1.0\n")
		case "/example.com/demo/@v/v1.1.0.info":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, `{"Version":"v1.1.0","Time":"2025-01-02T03:04:05Z"}`)
		case "/example.com/demo/@v/v1.1.0.mod":
			fmt.Fprint(w, "module example.com/demo\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "test-proxy", Recipe: "go-proxy", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: upstream.URL, ContentMaxAgeMinutes: 1440, MetadataMaxAgeMinutes: 1440, AutoBlock: true},
	})

	list, err := svc.Serve(repo.Name, "example.com/demo/@v/list", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, list); got != "v1.0.0\nv1.1.0\n" {
		t.Fatalf("list = %q", got)
	}
	if list.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("list content type = %q", list.Header.Get("Content-Type"))
	}

	info, err := svc.Serve(repo.Name, "example.com/demo/@v/v1.1.0.info", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, info); !strings.Contains(got, `"Version":"v1.1.0"`) {
		t.Fatalf("info = %q", got)
	}
	if info.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("info content type = %q", info.Header.Get("Content-Type"))
	}

	before := hits.Load()
	again, err := svc.Serve(repo.Name, "example.com/demo/@v/list", false)
	if err != nil {
		t.Fatal(err)
	}
	readResponse(t, again)
	if hits.Load() != before {
		t.Fatalf("fresh cache should not hit upstream: %d -> %d", before, hits.Load())
	}

	component, ok := svc.store.GetComponent(repo.Name, "example.com/demo", "v1.1.0")
	if !ok || component.Version != "v1.1.0" {
		t.Fatalf("component not registered: %+v %v", component, ok)
	}
}

func TestProxyNegativeCacheAndAutoBlock(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "negative-proxy", Recipe: "go-proxy", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: upstream.URL, MetadataMaxAgeMinutes: 60, AutoBlock: true},
	})

	if _, err := svc.Serve(repo.Name, "example.com/missing/@v/list", false); err == nil {
		t.Fatal("expected 404")
	}
	if _, err := svc.Serve(repo.Name, "example.com/missing/@v/list", false); err == nil {
		t.Fatal("expected cached 404")
	}
	if hits.Load() != 1 {
		t.Fatalf("negative cache should suppress the second lookup, hits=%d", hits.Load())
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failing.Close()
	blocked := createRepo(t, svc.store, &store.Repository{
		Name: "blocked-proxy", Recipe: "go-proxy", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: failing.URL, AutoBlock: true},
	})
	if _, err := svc.Serve(blocked.Name, "example.com/demo/@v/list", false); err == nil {
		t.Fatal("expected upstream failure")
	}
	afterFirst := hits.Load()
	if _, err := svc.Serve(blocked.Name, "example.com/demo/@v/list", false); err == nil {
		t.Fatal("expected blocked upstream failure")
	}
	if hits.Load() != afterFirst {
		t.Fatalf("circuit should stay open without upstream calls, hits=%d -> %d", afterFirst, hits.Load())
	}
	if !svc.store.IsProxyBlocked(blocked.Name, time.Now()) {
		t.Fatal("proxy should be blocked")
	}
}

func TestHostedPublishAndServe(t *testing.T) {
	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "test-hosted", Recipe: "go-hosted", Type: "hosted", Online: true,
		Hosted: &store.HostedSettings{WritePolicy: "ALLOW"},
	})
	archive := buildModuleZip(t, "example.com/hello", "v0.1.0", map[string]string{
		"go.mod":   "module example.com/hello\n\ngo 1.22\n",
		"hello.go": "package hello\n",
	})
	published, err := svc.Publish(repo.Name, "v0.1.0.zip", bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if published.Module != "example.com/hello" || published.Version != "v0.1.0" {
		t.Fatalf("published = %+v", published)
	}

	list, err := svc.Serve(repo.Name, "example.com/hello/@v/list", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, list); got != "v0.1.0" {
		t.Fatalf("list = %q", got)
	}

	latest, err := svc.Serve(repo.Name, "example.com/hello/@latest", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, latest); !strings.Contains(got, `"Version":"v0.1.0"`) {
		t.Fatalf("latest = %q", got)
	}
	if latest.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("latest content type = %q", latest.Header.Get("Content-Type"))
	}

	info, err := svc.Serve(repo.Name, "example.com/hello/@v/v0.1.0.info", false)
	if err != nil {
		t.Fatal(err)
	}
	readResponse(t, info)
	if info.Header.Get("ETag") != "" {
		t.Fatal("hosted .info must not expose validators")
	}

	mod, err := svc.Serve(repo.Name, "example.com/hello/@v/v0.1.0.mod", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, mod); got != "module example.com/hello\n\ngo 1.22\n" {
		t.Fatalf("mod = %q", got)
	}
	if mod.Header.Get("ETag") == "" {
		t.Fatal("hosted .mod should expose an ETag")
	}

	zipResponse, err := svc.Serve(repo.Name, "example.com/hello/@v/v0.1.0.zip", false)
	if err != nil {
		t.Fatal(err)
	}
	zipResponse.Close()
	if zipResponse.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("zip content type = %q", zipResponse.Header.Get("Content-Type"))
	}

	// A second publish is allowed by ALLOW and replaces the release.
	if _, err := svc.Publish(repo.Name, "v0.1.0.zip", bytes.NewReader(archive)); err != nil {
		t.Fatalf("ALLOW should replace: %v", err)
	}

	denied := createRepo(t, svc.store, &store.Repository{
		Name: "deny-hosted", Recipe: "go-hosted", Type: "hosted", Online: true,
		Hosted: &store.HostedSettings{WritePolicy: "DENY"},
	})
	if _, err := svc.Publish(denied.Name, "v0.1.0.zip", bytes.NewReader(archive)); err == nil {
		t.Fatal("DENY should reject all uploads")
	}
}

func TestHostedUppercaseModulePath(t *testing.T) {
	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "case-hosted", Recipe: "go-hosted", Type: "hosted", Online: true,
		Hosted: &store.HostedSettings{WritePolicy: "ALLOW"},
	})
	archive := buildModuleZip(t, "example.com/Acme/Demo", "v1.2.3", map[string]string{
		"go.mod": "module example.com/Acme/Demo\n",
	})
	if _, err := svc.Publish(repo.Name, "v1.2.3.zip", bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	response, err := svc.Serve(repo.Name, "example.com/!acme/!demo/@v/v1.2.3.mod", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, response); got != "module example.com/Acme/Demo\n" {
		t.Fatalf("mod = %q", got)
	}
}

func TestGroupAggregatesMembers(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example.com/demo/@v/list":
			fmt.Fprint(w, "v1.0.0\nv2.0.0\n")
		case "/example.com/demo/@latest":
			fmt.Fprint(w, `{"Version":"v2.0.0","Time":"2025-02-01T00:00:00Z"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := newTestService(t)
	proxy := createRepo(t, svc.store, &store.Repository{
		Name: "g-proxy", Recipe: "go-proxy", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: upstream.URL, AutoBlock: true},
	})
	hosted := createRepo(t, svc.store, &store.Repository{
		Name: "g-hosted", Recipe: "go-hosted", Type: "hosted", Online: true,
		Hosted: &store.HostedSettings{WritePolicy: "ALLOW"},
	})
	archive := buildModuleZip(t, "example.com/demo", "v1.5.0", map[string]string{
		"go.mod": "module example.com/demo\n",
	})
	if _, err := svc.Publish(hosted.Name, "v1.5.0.zip", bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	group := createRepo(t, svc.store, &store.Repository{
		Name: "g-group", Recipe: "go-group", Type: "group", Online: true,
		Group: &store.GroupSettings{MemberNames: []string{hosted.Name, proxy.Name}},
	})

	list, err := svc.Serve(group.Name, "example.com/demo/@v/list", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, list); got != "v1.0.0\nv1.5.0\nv2.0.0" {
		t.Fatalf("group list = %q", got)
	}

	latest, err := svc.Serve(group.Name, "example.com/demo/@latest", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, latest); !strings.Contains(got, `"Version":"v2.0.0"`) {
		t.Fatalf("group latest = %q", got)
	}

	mod, err := svc.Serve(group.Name, "example.com/demo/@v/v1.5.0.mod", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, mod); got != "module example.com/demo\n" {
		t.Fatalf("group first-hit mod = %q", got)
	}
}

func buildModuleZip(t *testing.T, module, version string, files map[string]string) []byte {
	t.Helper()
	buffer := new(bytes.Buffer)
	writer := zip.NewWriter(buffer)
	root := module + "@" + version + "/"
	for name, content := range files {
		entry, err := writer.Create(root + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
