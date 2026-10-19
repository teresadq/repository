package gomod

import (
	"testing"
	"time"
)

func TestVersions(t *testing.T) {
	if CompareVersions("v1.10.0", "v1.9.9") <= 0 {
		t.Fatal("v1.10.0 should be greater than v1.9.9")
	}
	if CompareVersions("v1.0.0", "v1.0.0-rc.1") <= 0 {
		t.Fatal("release should be greater than prerelease")
	}
	for _, pseudo := range []string{
		"v0.0.0-20250824120000-abcdef123456",
		"v1.2.4-0.20250824120000-abcdef1",
		"v1.2.3-rc.0.20250824120000-abcdef1",
	} {
		if !IsPseudoVersion(pseudo) {
			t.Errorf("%s should be a pseudo-version", pseudo)
		}
	}
	for _, tagged := range []string{
		"v1.2.3-rc.1",
		"v1.2.3-rc.20250824120000-abcdef1",
	} {
		if IsPseudoVersion(tagged) {
			t.Errorf("%s should not be a pseudo-version", tagged)
		}
	}

	got := ListVersions([]string{"v1.10.0", "v0.0.0-20250824120000-abcdef123456", "v1.2.3", "v1.2.3-rc.1"})
	want := []string{"v1.2.3-rc.1", "v1.2.3", "v1.10.0"}
	if len(got) != len(want) {
		t.Fatalf("ListVersions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListVersions = %v, want %v", got, want)
		}
	}
}

func TestLatest(t *testing.T) {
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	candidate, ok := Latest([]Candidate{
		{Version: "v2.0.0-rc.1", Time: now},
		{Version: "v1.9.0", Time: now.Add(-time.Minute)},
	})
	if !ok || candidate.Version != "v1.9.0" {
		t.Fatalf("latest = %v, want v1.9.0", candidate)
	}
	candidate, ok = Latest([]Candidate{
		{Version: "v2.0.0-rc.1", Time: now},
		{Version: "v2.0.0-rc.2", Time: now.Add(-time.Minute)},
	})
	if !ok || candidate.Version != "v2.0.0-rc.2" {
		t.Fatalf("latest = %v, want v2.0.0-rc.2", candidate)
	}
	candidate, ok = Latest([]Candidate{
		{Version: "v0.0.0-20250824120000-aaaaaaa", Time: now.Add(time.Minute)},
		{Version: "v0.0.0-20250825120000-bbbbbbb", Time: now},
	})
	if !ok || candidate.Version != "v0.0.0-20250825120000-bbbbbbb" {
		t.Fatalf("latest = %v, want newest pseudo-version", candidate)
	}
	if _, ok := Latest(nil); ok {
		t.Fatal("Latest(nil) should be empty")
	}
}

func TestEscapeVersions(t *testing.T) {
	escaped, err := EscapeVersion("v1.2.3-RC1")
	if err != nil || escaped != "v1.2.3-!r!c1" {
		t.Fatalf("EscapeVersion = %q, %v", escaped, err)
	}
	version, err := UnescapeVersion("v1.2.3-!r!c1")
	if err != nil || version != "v1.2.3-RC1" {
		t.Fatalf("UnescapeVersion = %q, %v", version, err)
	}
	for _, bad := range []string{"", "v1.2.3-!", "v1.2.3-!1", "v1.2.3-RC1"} {
		if _, err := UnescapeVersion(bad); err == nil {
			t.Errorf("UnescapeVersion(%q) should fail", bad)
		}
	}
	for _, bad := range []string{"1.2.3", "v01.2.3", "v1.2"} {
		if _, err := RequireCanonicalVersion(bad); err == nil {
			t.Errorf("RequireCanonicalVersion(%q) should fail", bad)
		}
	}
	if _, err := Major("v2147483648.0.0"); err == nil {
		t.Fatal("major overflow should fail")
	}
	if _, ok := PseudoTimestamp("v0.0.0-20251301120000-abcdef1"); ok {
		t.Fatal("invalid timestamp should not parse")
	}
}

func TestModulePaths(t *testing.T) {
	escaped, err := EscapeModulePath("example.com/Acme/Demo")
	if err != nil || escaped != "example.com/!acme/!demo" {
		t.Fatalf("EscapeModulePath = %q, %v", escaped, err)
	}
	module, err := UnescapeModulePath("example.com/!acme/!demo")
	if err != nil || module != "example.com/Acme/Demo" {
		t.Fatalf("UnescapeModulePath = %q, %v", module, err)
	}
}

func TestVersionSuffix(t *testing.T) {
	valid := [][2]string{
		{"example.com/acme/demo/v2", "v2.1.0"},
		{"example.com/acme/demo", "v2.1.0+incompatible"},
		{"gopkg.in/yaml.v3", "v3.0.1"},
		{"gopkg.in/check.v1", "v0.0.0-20161208181325-20d25e280405"},
		{"gopkg.in/yaml.v2-unstable", "v2.0.0"},
	}
	for _, pair := range valid {
		if err := RequireVersionSuffix(pair[0], pair[1]); err != nil {
			t.Errorf("RequireVersionSuffix(%q, %q): %v", pair[0], pair[1], err)
		}
	}
	invalid := [][2]string{
		{"example.com/acme/demo", "v2.1.0"},
		{"example.com/acme/demo/v3", "v2.1.0"},
		{"gopkg.in/yaml.v1", "v0.1.0"},
		{"example.com/acme/demo/v2", "v1.0.0"},
		{"example.com/acme/demo/v999999999999999999999999999999", "v2.0.0"},
	}
	for _, pair := range invalid {
		if err := RequireVersionSuffix(pair[0], pair[1]); err == nil {
			t.Errorf("RequireVersionSuffix(%q, %q) should fail", pair[0], pair[1])
		}
	}
}

func TestInvalidModulePaths(t *testing.T) {
	for _, path := range []string{
		"../demo",
		"localhost/demo",
		"Example.com/demo",
		"example.com//demo",
		"example.com/.demo",
		"example.com/demo~1",
		"example.com/demo/v1",
		"example.com/demo/v02",
		"gopkg.in/yaml",
		"example.com/dem%c",
		"example.com/con",
		"gopkg.in/yaml.v01",
	} {
		if _, err := RequireModulePath(path); err == nil {
			t.Errorf("RequireModulePath(%q) should fail", path)
		}
	}
	for _, escaped := range []string{"", "example.com/Acme", "example.com/!A", "example.com/demo!", "example.com/!1demo"} {
		if _, err := UnescapeModulePath(escaped); err == nil {
			t.Errorf("UnescapeModulePath(%q) should fail", escaped)
		}
	}
}
