package store

import (
	"testing"
	"time"
)

func TestCleanupRetainCountAndPreview(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepository(&Repository{
		Name: "cleanup-test", Recipe: "raw-hosted", Format: "raw", Type: "hosted", Online: true,
		Hosted: &HostedSettings{WritePolicy: "ALLOW"},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	versions := []struct {
		version string
		age     time.Duration
	}{
		{"1.0.0", 72 * time.Hour},
		{"2.0.0", 48 * time.Hour},
		{"3.0.0", 24 * time.Hour},
	}
	for _, version := range versions {
		sum, size, _, err := st.SaveBlobBytes([]byte("content-" + version.version))
		if err != nil {
			t.Fatal(err)
		}
		at := now.Add(-version.age)
		if err := st.PutAsset(&Asset{
			Repository: repo.Name, Format: "raw", Path: "pkg/" + version.version + "/artifact.bin",
			Module: "pkg", Version: version.version, Kind: "file", Source: "hosted",
			ContentType: "application/octet-stream", Size: size, SHA256: sum,
			CreatedAt: at, UpdatedAt: at,
		}, nil, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.PutCleanupPolicy(&CleanupPolicy{
		Name: "keep-one", Repository: repo.Name, Format: "raw", Enabled: true,
		Pattern: "pkg/**", RetainCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := st.RunCleanup("keep-one", true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Matched != 2 || preview.Deleted != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	if _, ok := st.GetAsset(repo.Name, "pkg/1.0.0/artifact.bin"); !ok {
		t.Fatal("preview must not delete assets")
	}
	run, err := st.RunCleanup("keep-one", false)
	if err != nil {
		t.Fatal(err)
	}
	if run.Deleted != 2 {
		t.Fatalf("deleted = %d", run.Deleted)
	}
	if _, ok := st.GetAsset(repo.Name, "pkg/3.0.0/artifact.bin"); !ok {
		t.Fatal("newest asset should be retained")
	}
	if _, ok := st.GetAsset(repo.Name, "pkg/1.0.0/artifact.bin"); ok {
		t.Fatal("oldest asset should be deleted")
	}
}
