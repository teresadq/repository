package security

import "testing"

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("s3cret-value")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "s3cret-value") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("invalid password accepted")
	}
	if VerifyPassword("not-a-hash", "x") {
		t.Fatal("malformed hash accepted")
	}
}

func TestPrivilegeMatching(t *testing.T) {
	admin := Principal{Username: "admin", Privileges: []string{"*"}}
	if !admin.CanRead("maven", "releases") || !admin.CanSecurity("update") {
		t.Fatal("wildcard principal should hold every privilege")
	}
	dev := Principal{Privileges: []string{
		"nexus:repository-view:maven:*:read",
		"nexus:repository-view:*:npm-proxy:write",
	}}
	if !dev.CanRead("maven", "releases") {
		t.Fatal("maven read should match")
	}
	if dev.CanRead("npm", "npm-proxy") {
		t.Fatal("npm read should not match")
	}
	if !dev.CanWrite("npm", "npm-proxy") {
		t.Fatal("npm write should match")
	}
	if dev.CanWrite("maven", "releases") {
		t.Fatal("maven write should not match")
	}
	if dev.IsAdmin() {
		t.Fatal("developer should not be admin")
	}
}

func TestContentSelectorMatching(t *testing.T) {
	context := SelectorContext{Path: "public/demo/1.0.0/a.jar", Format: "maven", Repository: "releases"}
	cases := []struct {
		expression string
		want       bool
	}{
		{`path.startsWith("public/") && format == "maven"`, true},
		{`path.startsWith("private/") || repository == "releases"`, true},
		{`!path.endsWith(".tmp") && path.matches(".*/a\\.jar")`, true},
		{`path.contains("demo") && (format == "npm" || repo == "releases")`, true},
		{`format == "npm"`, false},
		{`path.endsWith(".zip")`, false},
	}
	for _, testCase := range cases {
		matched, err := MatchSelector(testCase.expression, context)
		if err != nil {
			t.Fatalf("%s: %v", testCase.expression, err)
		}
		if matched != testCase.want {
			t.Fatalf("%s = %v, want %v", testCase.expression, matched, testCase.want)
		}
	}
	for _, expression := range []string{`unknown == "x"`, `path.matches("[")`, `path ==`, `path.startsWith("x"`} {
		if _, err := MatchSelector(expression, context); err == nil {
			t.Fatalf("%s should fail", expression)
		}
	}
	if err := ValidateSelector(`path.startsWith("public/")`); err != nil {
		t.Fatal(err)
	}
}

func TestCanPathWithSelectors(t *testing.T) {
	principal := Principal{Privileges: []string{
		"nexus:repository-content-selector:maven:*:public-only:read",
	}}
	resolver := func(name string) (string, bool) {
		if name == "public-only" {
			return `path.startsWith("public/")`, true
		}
		return "", false
	}
	if !principal.CanPath("maven", "releases", "read", "public/demo/a.jar", resolver) {
		t.Fatal("public path should match")
	}
	if principal.CanPath("maven", "releases", "read", "private/demo/a.jar", resolver) {
		t.Fatal("private path should not match")
	}
	if principal.CanPath("maven", "releases", "write", "public/demo/a.jar", resolver) {
		t.Fatal("write should not match a read-only selector privilege")
	}
}
