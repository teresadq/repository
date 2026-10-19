package store

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestExportImportReplace(t *testing.T) {
	source, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := source.CreateRepository(&Repository{
		Name: "backup-src", Recipe: "raw-hosted", Format: "raw", Type: "hosted", Online: true,
		Hosted: &HostedSettings{WritePolicy: "ALLOW"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum, size, _, err := source.SaveBlobBytes([]byte("backup-content"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := source.PutAsset(&Asset{
		Repository: repo.Name, Format: "raw", Path: "docs/a.txt", Module: "docs",
		Kind: "file", ContentType: "text/plain", Size: size, SHA256: sum, Source: "hosted",
		CreatedAt: now, UpdatedAt: now,
	}, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := source.PutUser(&User{Username: "backup-user", Name: "Backup", Active: true, Roles: []string{"nx-admin"}}); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	if err := source.Export(&archive); err != nil {
		t.Fatal(err)
	}

	destination, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := destination.ImportArchive(bytes.NewReader(archive.Bytes()), ImportOptions{Mode: "replace", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Repositories == 0 || result.Assets != 1 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := destination.GetRepository("backup-src"); err != nil {
		t.Fatal("imported repository missing")
	}
	if _, err := destination.GetUser("backup-user"); err != nil {
		t.Fatal("imported user missing")
	}
	asset, ok := destination.GetAsset("backup-src", "docs/a.txt")
	if !ok {
		t.Fatal("imported asset missing")
	}
	reader, err := destination.OpenBlob(asset)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(reader)
	reader.Close()
	if string(content) != "backup-content" {
		t.Fatalf("blob content = %q", content)
	}
}

func TestImportMergeSkipsExistingRepositories(t *testing.T) {
	source, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"merge-new", "raw-hosted"} {
		if _, err := source.CreateRepository(&Repository{
			Name: name, Recipe: "raw-hosted", Format: "raw", Type: "hosted", Online: true,
		}); err != nil && err != ErrAlreadyExists {
			t.Fatal(err)
		}
	}
	sum, size, _, _ := source.SaveBlobBytes([]byte("merge-content"))
	now := time.Now().UTC()
	_ = source.PutAsset(&Asset{
		Repository: "merge-new", Format: "raw", Path: "a.txt", Module: "a", Kind: "file",
		Size: size, SHA256: sum, Source: "hosted", CreatedAt: now, UpdatedAt: now,
	}, nil, true)
	var archive bytes.Buffer
	if err := source.Export(&archive); err != nil {
		t.Fatal(err)
	}

	destination, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := destination.ImportArchive(bytes.NewReader(archive.Bytes()), ImportOptions{Mode: "merge"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 8 || result.Repositories != 1 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := destination.GetRepository("merge-new"); err != nil {
		t.Fatal("new repository should be imported")
	}
	if _, ok := destination.GetAsset("raw-hosted", "a.txt"); ok {
		t.Fatal("existing repository should not receive imported assets")
	}
}

func TestImportRejectsUnsafeArchive(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.NewReader("not a gzip archive")
	if _, err := st.ImportArchive(bad, ImportOptions{Mode: "replace", Force: true}); err == nil {
		t.Fatal("invalid archive should fail")
	}
}
