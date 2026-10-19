// Package security implements password hashing and Nexus-style privilege
// matching used by the console and artifact protocols.
package security

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	passwordIterations = 210_000
	passwordKeyLength  = 32
)

// HashPassword encodes a password as pbkdf2-sha256$iterations$salt$hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against an encoded hash.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(key, expected) == 1
}

// Principal is an authenticated (or anonymous) caller.
type Principal struct {
	Username   string   `json:"username"`
	Name       string   `json:"name,omitempty"`
	Anonymous  bool     `json:"anonymous,omitempty"`
	Privileges []string `json:"privileges,omitempty"`
}

// Can reports whether the principal holds a privilege. Privileges are
// colon-separated and support '*' segments, e.g.
// nexus:repository-view:*:*:read.
func (p Principal) Can(privilege string) bool {
	for _, granted := range p.Privileges {
		if matchPrivilege(granted, privilege) {
			return true
		}
	}
	return false
}

// CanAny reports whether the principal holds one of the privileges.
func (p Principal) CanAny(privileges ...string) bool {
	for _, privilege := range privileges {
		if p.Can(privilege) {
			return true
		}
	}
	return false
}

// CanBrowse reports browse access (listing/search) to a repository.
func (p Principal) CanBrowse(format, repository string) bool {
	return p.CanAny(
		fmt.Sprintf("nexus:repository-view:%s:%s:browse", format, repository),
		fmt.Sprintf("nexus:repository-view:*:%s:browse", repository),
	)
}

// CanRead reports download access to a repository.
func (p Principal) CanRead(format, repository string) bool {
	return p.CanAny(
		fmt.Sprintf("nexus:repository-view:%s:%s:read", format, repository),
		fmt.Sprintf("nexus:repository-view:*:%s:read", repository),
	)
}

// CanWrite reports upload/delete access to a repository.
func (p Principal) CanWrite(format, repository string) bool {
	return p.CanAny(
		fmt.Sprintf("nexus:repository-view:%s:%s:write", format, repository),
		fmt.Sprintf("nexus:repository-view:*:%s:write", repository),
	)
}

// CanAdminRepository reports repository configuration access.
func (p Principal) CanAdminRepository(format, repository string) bool {
	for _, action := range []string{"read", "add", "edit", "delete"} {
		if !p.Can(fmt.Sprintf("nexus:repository-admin:%s:%s:%s", format, repository, action)) &&
			!p.Can(fmt.Sprintf("nexus:repository-admin:*:%s:%s", repository, action)) {
			return false
		}
	}
	return true
}

// CanSecurity reports access to users, roles, groups and tokens.
func (p Principal) CanSecurity(action string) bool {
	return p.Can("nexus:security:"+action) || p.Can("nexus:security:*")
}

// IsAdmin reports whether the principal has the global wildcard privilege.
func (p Principal) IsAdmin() bool {
	return p.Can("*")
}

func matchPrivilege(granted, required string) bool {
	if granted == "*" || granted == required {
		return true
	}
	grantedParts := strings.Split(granted, ":")
	requiredParts := strings.Split(required, ":")
	if len(grantedParts) != len(requiredParts) {
		return false
	}
	for index := range grantedParts {
		if grantedParts[index] != "*" && grantedParts[index] != requiredParts[index] {
			return false
		}
	}
	return true
}
