package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kkrepo-go/internal/store"
)

func runConcurrent(t *testing.T, count int, call func() error) {
	t.Helper()
	start := make(chan struct{})
	errors := make(chan error, count)
	var waitGroup sync.WaitGroup
	for index := 0; index < count; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			errors <- call()
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoProxySingleFlight(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("v1.0.0\n"))
	}))
	defer upstream.Close()

	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "flight-go", Recipe: "go-proxy", Format: "go", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: upstream.URL, AutoBlock: true},
	})
	runConcurrent(t, 20, func() error {
		response, err := svc.Serve(repo.Name, "example.com/demo/@v/list", false)
		if err != nil {
			return err
		}
		defer response.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		if string(body) != "v1.0.0\n" {
			return &testError{"unexpected body: " + string(body)}
		}
		return nil
	})
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits.Load())
	}
}

func TestGenericProxySingleFlight(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("pom-content"))
	}))
	defer upstream.Close()

	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "flight-maven", Recipe: "maven-proxy", Format: "maven", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: upstream.URL, AutoBlock: true},
	})
	runConcurrent(t, 20, func() error {
		response, err := svc.serveRepository(repo, &Request{
			Method: http.MethodGet, Path: "com/example/demo/1.0.0/demo.pom", Header: http.Header{},
		})
		if err != nil {
			return err
		}
		defer response.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		if string(body) != "pom-content" {
			return &testError{"unexpected body: " + string(body)}
		}
		return nil
	})
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits.Load())
	}
}

func TestDockerProxySingleFlight(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Header().Set("Docker-Content-Digest", "sha256:deadbeef")
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	}))
	defer upstream.Close()

	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "flight-docker", Recipe: "docker-proxy", Format: "docker", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: upstream.URL, AutoBlock: true},
	})
	runConcurrent(t, 20, func() error {
		response, err := svc.serveRepository(repo, &Request{
			Method: http.MethodGet, Path: "v2/demo/app/manifests/1.0",
			Header: http.Header{"Accept": []string{"application/vnd.oci.image.manifest.v1+json"}},
		})
		if err != nil {
			return err
		}
		defer response.Close()
		if _, err := io.ReadAll(response.Body); err != nil {
			return err
		}
		return nil
	})
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits.Load())
	}
}

func TestProxySingleFlightSharesErrors(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	svc := newTestService(t)
	repo := createRepo(t, svc.store, &store.Repository{
		Name: "flight-failure", Recipe: "maven-proxy", Format: "maven", Type: "proxy", Online: true,
		Proxy: &store.ProxySettings{RemoteURL: upstream.URL, AutoBlock: true},
	})
	runConcurrent(t, 10, func() error {
		if _, err := svc.serveRepository(repo, &Request{
			Method: http.MethodGet, Path: "com/example/demo/1.0.0/demo.pom", Header: http.Header{},
		}); err == nil {
			return &testError{"expected upstream failure"}
		}
		return nil
	})
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits.Load())
	}
}

type testError struct{ message string }

func (e *testError) Error() string { return e.message }
