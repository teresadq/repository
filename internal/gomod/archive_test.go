package gomod

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "module.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(out)
	for name, content := range files {
		entry, err := writer.Create(name)
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
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectModuleArchive(t *testing.T) {
	archive := writeZip(t, map[string]string{
		"example.com/demo@v1.2.3/go.mod":     "module example.com/demo\n\ngo 1.22\n",
		"example.com/demo@v1.2.3/demo.go":    "package demo\n",
		"example.com/demo@v1.2.3/sub/README": "hi\n",
	})
	inspected, err := InspectModuleArchive(archive, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Module != "example.com/demo" || inspected.Version != "v1.2.3" {
		t.Fatalf("inspected = %+v", inspected)
	}
	if string(inspected.GoMod) != "module example.com/demo\n\ngo 1.22\n" {
		t.Fatalf("go.mod = %q", inspected.GoMod)
	}
}

func TestInspectModuleArchiveSynthesizesGoMod(t *testing.T) {
	archive := writeZip(t, map[string]string{
		"example.com/demo@v1.2.3/demo.go": "package demo\n",
	})
	inspected, err := InspectModuleArchive(archive, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if string(inspected.GoMod) != "module example.com/demo\n" {
		t.Fatalf("go.mod = %q", inspected.GoMod)
	}
}

func TestInspectModuleArchiveRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"version mismatch": {
			"example.com/demo@v1.2.2/demo.go": "package demo\n",
		},
		"module directive mismatch": {
			"example.com/demo@v1.2.3/go.mod": "module example.com/other\n",
		},
		"two roots": {
			"example.com/demo@v1.2.3/go.mod":  "module example.com/demo\n",
			"example.com/other@v1.2.3/go.mod": "module example.com/other\n",
		},
		"unsafe path": {
			"example.com/demo@v1.2.3/../escape.go": "package demo\n",
		},
		"nested go.mod": {
			"example.com/demo@v1.2.3/go.mod":     "module example.com/demo\n",
			"example.com/demo@v1.2.3/sub/go.mod": "module example.com/sub\n",
		},
		"case conflict": {
			"example.com/demo@v1.2.3/go.mod":    "module example.com/demo\n",
			"example.com/demo@v1.2.3/Readme.md": "a\n",
			"example.com/demo@v1.2.3/readme.md": "b\n",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			archive := writeZip(t, files)
			if _, err := InspectModuleArchive(archive, "v1.2.3"); err == nil {
				t.Fatal("expected archive inspection to fail")
			}
		})
	}
}

func TestInspectModuleArchiveMajorSuffix(t *testing.T) {
	archive := writeZip(t, map[string]string{
		"example.com/demo/v2@v2.0.0/go.mod": "module example.com/demo/v2\n",
	})
	if _, err := InspectModuleArchive(archive, "v2.0.0"); err != nil {
		t.Fatalf("valid /v2 archive rejected: %v", err)
	}
	bad := writeZip(t, map[string]string{
		"example.com/demo@v2.0.0/go.mod": "module example.com/demo\n",
	})
	if _, err := InspectModuleArchive(bad, "v2.0.0"); err == nil {
		t.Fatal("v2 module without /v2 suffix should be rejected")
	}
}
