package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"kkrepo-go/internal/ldapauth"
	"kkrepo-go/internal/security"
	"kkrepo-go/internal/service"
	"kkrepo-go/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := security.HashPassword("admin123")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureAdmin("admin", "管理员", hash); err != nil {
		t.Fatal(err)
	}
	server := New(Options{
		Store:   st,
		Service: service.New(st),
		Auth:    NewAuth(st, "test-secret", time.Hour),
		Assets: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html>console</html>")},
		},
		Version:   "test",
		GoVersion: "go-test",
		DataDir:   t.TempDir(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer, st
}

func login(t *testing.T, base string) *http.Cookie {
	t.Helper()
	body := strings.NewReader(`{"username":"admin","password":"admin123"}`)
	response, err := http.Post(base+"/api/login", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", response.StatusCode)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == sessionCookie {
			return cookie
		}
	}
	t.Fatal("session cookie not issued")
	return nil
}

func doJSON(t *testing.T, method, url string, cookie *http.Cookie, payload any) (*http.Response, map[string]any) {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if len(raw) > 0 && strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(raw, &body)
	}
	return response, body
}

func TestManagementAPIRequiresSession(t *testing.T) {
	server, _ := newTestServer(t)
	response, _ := doJSON(t, http.MethodGet, server.URL+"/api/repositories", nil, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", response.StatusCode)
	}
	cookie := login(t, server.URL)
	response, body := doJSON(t, http.MethodGet, server.URL+"/api/repositories", cookie, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", response.StatusCode)
	}
	items, _ := body["items"].([]any)
	if len(items) != 8 {
		t.Fatalf("seeded repositories = %d", len(items))
	}
}

func TestRepositoryCRUDAndProxyProtocol(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/example.com/demo/@v/list" {
			fmt.Fprint(w, "v1.0.0\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	server, _ := newTestServer(t)
	cookie := login(t, server.URL)

	response, body := doJSON(t, http.MethodPost, server.URL+"/api/repositories", cookie, map[string]any{
		"name":   "public-proxy",
		"recipe": "go-proxy",
		"proxy": map[string]any{
			"remoteUrl":             upstream.URL,
			"contentMaxAgeMinutes":  30,
			"metadataMaxAgeMinutes": 5,
			"autoBlock":             true,
		},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body=%v", response.StatusCode, body)
	}
	if body["type"] != "proxy" {
		t.Fatalf("created type = %v", body["type"])
	}

	// The protocol endpoint is anonymous.
	protocol, err := http.Get(server.URL + "/repository/public-proxy/example.com/demo/@v/list")
	if err != nil {
		t.Fatal(err)
	}
	listBody, _ := io.ReadAll(protocol.Body)
	protocol.Body.Close()
	if protocol.StatusCode != http.StatusOK || string(listBody) != "v1.0.0\n" {
		t.Fatalf("proxy list = %d %q", protocol.StatusCode, listBody)
	}

	// Group creation rejects nested groups and unknown members.
	response, body = doJSON(t, http.MethodPost, server.URL+"/api/repositories", cookie, map[string]any{
		"name":   "bad-group",
		"recipe": "go-group",
		"group":  map[string]any{"memberNames": []string{"public-proxy", "missing"}},
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad group status = %d body=%v", response.StatusCode, body)
	}

	response, body = doJSON(t, http.MethodPost, server.URL+"/api/repositories", cookie, map[string]any{
		"name":   "good-group",
		"recipe": "go-group",
		"group":  map[string]any{"memberNames": []string{"go-hosted", "public-proxy"}},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("group create status = %d body=%v", response.StatusCode, body)
	}
	protocol, err = http.Get(server.URL + "/repository/good-group/example.com/demo/@v/list")
	if err != nil {
		t.Fatal(err)
	}
	listBody, _ = io.ReadAll(protocol.Body)
	protocol.Body.Close()
	if protocol.StatusCode != http.StatusOK || string(listBody) != "v1.0.0" {
		t.Fatalf("group list = %d %q", protocol.StatusCode, listBody)
	}

	// Deleting a referenced member is refused.
	response, _ = doJSON(t, http.MethodDelete, server.URL+"/api/repositories/go-hosted", cookie, nil)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("delete referenced status = %d", response.StatusCode)
	}
	response, _ = doJSON(t, http.MethodDelete, server.URL+"/api/repositories/good-group", cookie, nil)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete group status = %d", response.StatusCode)
	}
}

func TestHostedUploadThroughProtocolEndpoint(t *testing.T) {
	server, _ := newTestServer(t)
	archive := moduleZip(t, "example.com/hello", "v1.0.0")
	request, err := http.NewRequest(http.MethodPut,
		server.URL+"/repository/go-hosted/v1.0.0.zip", bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	request.SetBasicAuth("admin", "admin123")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", response.StatusCode)
	}

	// Uploads without credentials are rejected.
	anonymous, err := http.NewRequest(http.MethodPut,
		server.URL+"/repository/go-hosted/v9.9.9.zip", bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	anonymousResponse, err := http.DefaultClient.Do(anonymous)
	if err != nil {
		t.Fatal(err)
	}
	anonymousResponse.Body.Close()
	if anonymousResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous upload status = %d", anonymousResponse.StatusCode)
	}
	protocol, err := http.Get(server.URL + "/repository/go-hosted/example.com/hello/@latest")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(protocol.Body)
	protocol.Body.Close()
	if protocol.StatusCode != http.StatusOK || !strings.Contains(string(body), `"Version":"v1.0.0"`) {
		t.Fatalf("latest = %d %q", protocol.StatusCode, body)
	}
}

func moduleZip(t *testing.T, module, version string) []byte {
	t.Helper()
	buffer := new(bytes.Buffer)
	writer := zip.NewWriter(buffer)
	entry, err := writer.Create(module + "@" + version + "/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(entry, "module %s\n", module)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestRepositoryBrowseTree(t *testing.T) {
	server, _ := newTestServer(t)
	cookie := login(t, server.URL)

	upload := func(path, content string) {
		request, err := http.NewRequest(http.MethodPut,
			server.URL+"/repository/raw-hosted/"+path, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		request.SetBasicAuth("admin", "admin123")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("upload %s status = %d", path, response.StatusCode)
		}
	}
	upload("docs/guide/intro.txt", "intro")
	upload("docs/readme.txt", "readme")

	browse := func(path string) []any {
		url := server.URL + "/api/repositories/raw-hosted/browse"
		if path != "" {
			url += "?path=" + path
		}
		request, _ := http.NewRequest(http.MethodGet, url, nil)
		request.AddCookie(cookie)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		entries, _ := body["entries"].([]any)
		return entries
	}

	root := browse("")
	if len(root) != 1 {
		t.Fatalf("root entries = %v", root)
	}
	docs := root[0].(map[string]any)
	if docs["type"] != "directory" || docs["name"] != "docs" || docs["count"].(float64) != 2 {
		t.Fatalf("docs entry = %v", docs)
	}
	children := browse("docs")
	if len(children) != 2 {
		t.Fatalf("docs entries = %v", children)
	}
	if children[0].(map[string]any)["type"] != "directory" || children[0].(map[string]any)["name"] != "guide" {
		t.Fatalf("first docs entry = %v", children[0])
	}
	leaves := browse("docs/guide")
	if len(leaves) != 1 || leaves[0].(map[string]any)["name"] != "intro.txt" {
		t.Fatalf("guide entries = %v", leaves)
	}
	if leaves[0].(map[string]any)["url"] != "/repository/raw-hosted/docs/guide/intro.txt" {
		t.Fatalf("leaf url = %v", leaves[0].(map[string]any)["url"])
	}
}

func TestContentSelectorRestrictsProtocolPaths(t *testing.T) {
	server, _ := newTestServer(t)
	admin := login(t, server.URL)

	response, body := doJSON(t, http.MethodPost, server.URL+"/api/content-selectors", admin, map[string]any{
		"name": "public-only", "expression": `path.startsWith("public/")`,
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("selector create = %d %v", response.StatusCode, body)
	}
	response, body = doJSON(t, http.MethodPost, server.URL+"/api/roles", admin, map[string]any{
		"name": "selector-reader", "privileges": []string{
			"nexus:repository-content-selector:raw:*:public-only:read",
		},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("role create = %d %v", response.StatusCode, body)
	}
	response, body = doJSON(t, http.MethodPost, server.URL+"/api/users", admin, map[string]any{
		"username": "selector-user", "password": "secret123", "roles": []string{"selector-reader"},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("user create = %d %v", response.StatusCode, body)
	}
	for _, path := range []string{"public/a.txt", "private/b.txt"} {
		request, _ := http.NewRequest(http.MethodPut,
			server.URL+"/repository/raw-hosted/"+path, strings.NewReader("content"))
		request.SetBasicAuth("admin", "admin123")
		upload, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		upload.Body.Close()
		if upload.StatusCode != http.StatusCreated {
			t.Fatalf("upload %s = %d", path, upload.StatusCode)
		}
	}
	loginBody := strings.NewReader(`{"username":"selector-user","password":"secret123"}`)
	loginResponse, err := http.Post(server.URL+"/api/login", "application/json", loginBody)
	if err != nil {
		t.Fatal(err)
	}
	defer loginResponse.Body.Close()
	var userCookie *http.Cookie
	for _, cookie := range loginResponse.Cookies() {
		if cookie.Name == sessionCookie {
			userCookie = cookie
		}
	}
	if userCookie == nil {
		t.Fatal("selector user login failed")
	}
	publicRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/repository/raw-hosted/public/a.txt", nil)
	publicRequest.AddCookie(userCookie)
	publicResponse, err := http.DefaultClient.Do(publicRequest)
	if err != nil {
		t.Fatal(err)
	}
	publicResponse.Body.Close()
	if publicResponse.StatusCode != http.StatusOK {
		t.Fatalf("public path status = %d", publicResponse.StatusCode)
	}
	privateRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/repository/raw-hosted/private/b.txt", nil)
	privateRequest.AddCookie(userCookie)
	privateResponse, err := http.DefaultClient.Do(privateRequest)
	if err != nil {
		t.Fatal(err)
	}
	privateResponse.Body.Close()
	if privateResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("private path status = %d", privateResponse.StatusCode)
	}
}

type fakeLDAP struct{}

func (fakeLDAP) Authenticate(username, password string) (*ldapauth.User, bool) {
	if password != "ldap-password" {
		return nil, false
	}
	return &ldapauth.User{Username: username, Name: "LDAP User", Email: "ldap@example.test",
		Groups: []string{"nx-anonymous"}}, true
}
func (fakeLDAP) DefaultRoles() []string { return []string{"nx-developer"} }

func TestLDAPLoginProvisionsUser(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	auth := NewAuth(st, "test-secret", time.Hour)
	auth.SetLDAP(fakeLDAP{})
	if _, ok := auth.Login("ldap-user", "wrong"); ok {
		t.Fatal("wrong LDAP password should fail")
	}
	principal, ok := auth.Login("ldap-user", "ldap-password")
	if !ok || principal.Username != "ldap-user" {
		t.Fatalf("principal = %+v ok=%v", principal, ok)
	}
	user, err := st.GetUser("ldap-user")
	if err != nil {
		t.Fatal(err)
	}
	roles := strings.Join(user.Roles, ",")
	if !strings.Contains(roles, "nx-developer") || !strings.Contains(roles, "nx-anonymous") {
		t.Fatalf("roles = %v", user.Roles)
	}
}
