// Package ldapauth provides simple-bind LDAP authentication with a user search.
package ldapauth

import (
	"fmt"
	"strings"

	ldap "github.com/go-ldap/ldap/v3"
)

// Config configures LDAP authentication.
type Config struct {
	URL          string
	BaseDN       string
	BindDN       string
	BindPassword string
	UserFilter   string
	DefaultRoles []string
}

// User is an authenticated LDAP identity.
type User struct {
	Username string
	Name     string
	Email    string
	Groups   []string
}

// Authenticator performs LDAP bind authentication.
type Authenticator struct{ cfg Config }

// New returns nil when LDAP is not configured.
func New(cfg Config) *Authenticator {
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.BaseDN) == "" {
		return nil
	}
	if strings.TrimSpace(cfg.UserFilter) == "" {
		cfg.UserFilter = "(uid=%s)"
	}
	return &Authenticator{cfg: cfg}
}

// DefaultRoles returns roles assigned to provisioned LDAP users.
func (a *Authenticator) DefaultRoles() []string {
	if a == nil {
		return nil
	}
	return append([]string{}, a.cfg.DefaultRoles...)
}

// Authenticate searches for the user and verifies their password by binding.
func (a *Authenticator) Authenticate(username, password string) (*User, bool) {
	if a == nil || username == "" || password == "" {
		return nil, false
	}
	connection, err := ldap.DialURL(a.cfg.URL)
	if err != nil {
		return nil, false
	}
	defer connection.Close()
	if a.cfg.BindDN != "" {
		if err := connection.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			return nil, false
		}
	}
	filter := fmt.Sprintf(a.cfg.UserFilter, ldap.EscapeFilter(username))
	result, err := connection.Search(&ldap.SearchRequest{
		BaseDN: a.cfg.BaseDN, Scope: ldap.ScopeWholeSubtree, Filter: filter,
		Attributes: []string{"uid", "sAMAccountName", "cn", "displayName", "mail", "memberOf"},
	})
	if err != nil || len(result.Entries) == 0 {
		return nil, false
	}
	entry := result.Entries[0]
	if err := connection.Bind(entry.DN, password); err != nil {
		return nil, false
	}
	user := &User{
		Username: first(entry, "uid", "sAMAccountName", "mail", "cn"),
		Name:     first(entry, "displayName", "cn", "uid"),
		Email:    first(entry, "mail"),
	}
	if user.Username == "" {
		user.Username = username
	}
	for _, group := range entry.GetAttributeValues("memberOf") {
		if cn := commonName(group); cn != "" {
			user.Groups = append(user.Groups, cn)
		}
	}
	return user, true
}

func first(entry *ldap.Entry, attributes ...string) string {
	for _, attribute := range attributes {
		if value := entry.GetAttributeValue(attribute); value != "" {
			return value
		}
	}
	return ""
}

func commonName(dn string) string {
	for _, part := range strings.Split(dn, ",") {
		part = strings.TrimSpace(part)
		if len(part) > 3 && strings.EqualFold(part[:3], "cn=") {
			return part[3:]
		}
	}
	return ""
}
