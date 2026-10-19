package gomod

import "testing"

func TestParseGoPath(t *testing.T) {
	latest, err := ParseGoPath("/example.com/acme/demo/@latest")
	if err != nil || latest.Kind != KindLatest || latest.Module != "example.com/acme/demo" {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	list, err := ParseGoPath("example.com/acme/demo/@v/list")
	if err != nil || list.Kind != KindList || list.HasComponent() || !list.IsMetadata() {
		t.Fatalf("list = %+v, %v", list, err)
	}
	info, err := ParseGoPath("example.com/acme/demo/@v/v1.2.3.info")
	if err != nil || info.Kind != KindInfo || info.Version != "v1.2.3" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	module, err := ParseGoPath("example.com/acme/demo/@v/v1.2.3.mod")
	if err != nil || module.Kind != KindModule {
		t.Fatalf("module = %+v, %v", module, err)
	}
	archive, err := ParseGoPath("example.com/acme/demo/@v/v1.2.3.zip")
	if err != nil || archive.FileName() != "v1.2.3.zip" || !archive.HasComponent() {
		t.Fatalf("archive = %+v, %v", archive, err)
	}

	if archive.ContentType() != "application/zip" {
		t.Errorf("zip content type = %s", archive.ContentType())
	}
	if list.ContentType() != "text/plain" || list.ProxyContentType() != "application/json" {
		t.Errorf("list content types = %s / %s", list.ContentType(), list.ProxyContentType())
	}
	if info.ContentType() != "application/json" || info.ProxyContentType() != "text/plain" {
		t.Errorf("info content types = %s / %s", info.ContentType(), info.ProxyContentType())
	}
	if latest.ContentType() != "text/plain" {
		t.Errorf("latest content type = %s", latest.ContentType())
	}
	if module.ContentType() != "text/plain" {
		t.Errorf("mod content type = %s", module.ContentType())
	}
}

func TestParseGoPathRejects(t *testing.T) {
	for _, raw := range []string{
		"",
		"example.com/demo/",
		"example.com/demo",
		"example.com/demo/@v/version",
		"example.com/demo/@v/v1.0.0.txt",
		"example.com/@v/demo/@v/v1.0.0.mod",
		"example.com/!A/demo/@v/list",
	} {
		if _, err := ParseGoPath(raw); err == nil {
			t.Errorf("ParseGoPath(%q) should fail", raw)
		}
	}
	if _, err := VersionedPath("example.com/demo", "v1.0.0", KindList); err == nil {
		t.Fatal("VersionedPath(KindList) should fail")
	}
}

func TestParseGoPathEscapedCoordinates(t *testing.T) {
	parsed, err := ParseGoPath("example.com/!acme/!demo/@v/v1.2.3-!r!c1.mod")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Module != "example.com/Acme/Demo" || parsed.Version != "v1.2.3-RC1" {
		t.Fatalf("parsed = %+v", parsed)
	}
	built, err := VersionedPath("example.com/Acme/Demo", "v1.2.3-RC1", KindPackage)
	if err != nil {
		t.Fatal(err)
	}
	if built.Path != "example.com/!acme/!demo/@v/v1.2.3-!r!c1.zip" {
		t.Fatalf("built path = %s", built.Path)
	}
}
