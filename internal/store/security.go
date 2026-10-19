package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// User is a console/protocol principal.
type User struct {
	Username     string    `json:"username"`
	Name         string    `json:"name"`
	Email        string    `json:"email,omitempty"`
	PasswordHash string    `json:"passwordHash"`
	Roles        []string  `json:"roles,omitempty"`
	Groups       []string  `json:"groups,omitempty"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Role is a named set of Nexus-style privileges.
type Role struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Privileges  []string  `json:"privileges"`
	BuiltIn     bool      `json:"builtIn,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Group bundles roles for users.
type Group struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Roles       []string  `json:"roles"`
	BuiltIn     bool      `json:"builtIn,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Token is a hashed API token.
type Token struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Prefix     string     `json:"prefix"`
	Hash       string     `json:"hash"`
	Scopes     []string   `json:"scopes,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt time.Time  `json:"lastUsedAt,omitempty"`
}

// AuditEntry is one security/management audit record.
type AuditEntry struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	User       string    `json:"user"`
	IP         string    `json:"ip,omitempty"`
	Action     string    `json:"action"`
	Repository string    `json:"repository,omitempty"`
	Format     string    `json:"format,omitempty"`
	Path       string    `json:"path,omitempty"`
	Status     int       `json:"status,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// SecuritySettings controls anonymous access.
type SecuritySettings struct {
	AnonymousEnabled bool     `json:"anonymousEnabled"`
	AnonymousRoles   []string `json:"anonymousRoles"`
}

const maxAuditEntries = 5000

// EnsureAdmin creates or updates the built-in admin user and role.
func (s *Store) EnsureAdmin(username, displayName, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if s.state.Roles == nil {
		s.state.Roles = map[string]*Role{}
	}
	admin, ok := s.state.Roles["nx-admin"]
	if !ok {
		admin = &Role{
			Name:        "nx-admin",
			Description: "Full system administration",
			Privileges:  []string{"*"},
			BuiltIn:     true,
			CreatedAt:   now,
		}
	}
	admin.UpdatedAt = now
	s.state.Roles["nx-admin"] = admin

	if s.state.Users == nil {
		s.state.Users = map[string]*User{}
	}
	user, ok := s.state.Users[username]
	if !ok {
		user = &User{Username: username, CreatedAt: now, Active: true}
	}
	user.Name = displayName
	user.PasswordHash = passwordHash
	user.Active = true
	user.Roles = []string{"nx-admin"}
	user.UpdatedAt = now
	s.state.Users[username] = user
	return s.persistLocked()
}

// ListUsers returns users sorted by username.
func (s *Store) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.state.Users))
	for _, user := range s.state.Users {
		out = append(out, cloneUser(user))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// GetUser returns one user.
func (s *Store) GetUser(username string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.state.Users[username]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneUser(user), nil
}

// PutUser creates or replaces a user.
func (s *Store) PutUser(user *User) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if user.Username == "" || strings.ContainsAny(user.Username, "/ \t\n") {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	copied := cloneUser(user)
	if existing, ok := s.state.Users[user.Username]; ok {
		copied.CreatedAt = existing.CreatedAt
	} else if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	copied.UpdatedAt = now
	s.state.Users[copied.Username] = copied
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneUser(copied), nil
}

// DeleteUser removes a user and their tokens. The admin account is protected.
func (s *Store) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.state.Users[username]
	if !ok {
		return ErrNotFound
	}
	for _, role := range user.Roles {
		if role == "nx-admin" {
			return errors.New("cannot delete a user with nx-admin")
		}
	}
	delete(s.state.Users, username)
	for id, token := range s.state.Tokens {
		if token.Username == username {
			delete(s.state.Tokens, id)
		}
	}
	return s.persistLocked()
}

// ListRoles returns roles sorted by name.
func (s *Store) ListRoles() []*Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Role, 0, len(s.state.Roles))
	for _, role := range s.state.Roles {
		out = append(out, cloneRole(role))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetRole returns one role.
func (s *Store) GetRole(name string) (*Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	role, ok := s.state.Roles[name]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRole(role), nil
}

// PutRole creates or replaces a role.
func (s *Store) PutRole(role *Role) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role.Name == "" {
		return nil, ErrInvalid
	}
	if existing, ok := s.state.Roles[role.Name]; ok && existing.BuiltIn && role.Name == "nx-admin" {
		return nil, errors.New("cannot modify the built-in nx-admin role")
	}
	now := time.Now().UTC()
	copied := cloneRole(role)
	if existing, ok := s.state.Roles[role.Name]; ok {
		copied.CreatedAt = existing.CreatedAt
	} else if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	copied.UpdatedAt = now
	s.state.Roles[copied.Name] = copied
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneRole(copied), nil
}

// DeleteRole removes a custom role.
func (s *Store) DeleteRole(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.state.Roles[name]
	if !ok {
		return ErrNotFound
	}
	if role.BuiltIn {
		return errors.New("cannot delete a built-in role")
	}
	delete(s.state.Roles, name)
	for _, user := range s.state.Users {
		user.Roles = removeString(user.Roles, name)
	}
	for _, group := range s.state.Groups {
		group.Roles = removeString(group.Roles, name)
	}
	return s.persistLocked()
}

// ListGroups returns groups sorted by name.
func (s *Store) ListGroups() []*Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Group, 0, len(s.state.Groups))
	for _, group := range s.state.Groups {
		out = append(out, cloneGroup(group))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetGroup returns one group.
func (s *Store) GetGroup(name string) (*Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, ok := s.state.Groups[name]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneGroup(group), nil
}

// PutGroup creates or replaces a group.
func (s *Store) PutGroup(group *Group) (*Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if group.Name == "" {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	copied := cloneGroup(group)
	if existing, ok := s.state.Groups[group.Name]; ok {
		copied.CreatedAt = existing.CreatedAt
	} else if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	copied.UpdatedAt = now
	s.state.Groups[copied.Name] = copied
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneGroup(copied), nil
}

// DeleteGroup removes a group.
func (s *Store) DeleteGroup(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, ok := s.state.Groups[name]
	if !ok {
		return ErrNotFound
	}
	if group.BuiltIn {
		return errors.New("cannot delete a built-in group")
	}
	delete(s.state.Groups, name)
	for _, user := range s.state.Users {
		user.Groups = removeString(user.Groups, name)
	}
	return s.persistLocked()
}

// ListTokens returns tokens without their hashes.
func (s *Store) ListTokens() []*Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Token, 0, len(s.state.Tokens))
	for _, token := range s.state.Tokens {
		out = append(out, cloneToken(token, false))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// PutToken stores a token (the caller supplies the hash).
func (s *Store) PutToken(token *Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token.ID == "" || token.Username == "" || token.Hash == "" {
		return ErrInvalid
	}
	s.state.Tokens[token.ID] = cloneToken(token, true)
	return s.persistLocked()
}

// DeleteToken removes a token.
func (s *Store) DeleteToken(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Tokens[id]; !ok {
		return ErrNotFound
	}
	delete(s.state.Tokens, id)
	return s.persistLocked()
}

// FindTokenByHash returns the username for a token hash.
func (s *Store) FindTokenByHash(hash string, now time.Time) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, token := range s.state.Tokens {
		if token.Hash != hash {
			continue
		}
		if token.ExpiresAt != nil && token.ExpiresAt.Before(now) {
			return "", false
		}
		token.LastUsedAt = now.UTC()
		_ = s.persistLocked()
		return token.Username, true
	}
	return "", false
}

// EffectivePrivileges returns the union of privileges for a user.
func (s *Store) EffectivePrivileges(username string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.state.Users[username]
	if !ok || !user.Active {
		return nil, false
	}
	set := map[string]struct{}{}
	roleNames := append([]string(nil), user.Roles...)
	for _, groupName := range user.Groups {
		if group := s.state.Groups[groupName]; group != nil {
			roleNames = append(roleNames, group.Roles...)
		}
	}
	for _, roleName := range roleNames {
		role := s.state.Roles[roleName]
		if role == nil {
			continue
		}
		for _, privilege := range role.Privileges {
			set[privilege] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for privilege := range set {
		out = append(out, privilege)
	}
	sort.Strings(out)
	return out, true
}

// AnonymousPrivileges returns privileges for anonymous access, if enabled.
func (s *Store) AnonymousPrivileges() ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.state.Security.AnonymousEnabled {
		return nil, false
	}
	set := map[string]struct{}{}
	for _, roleName := range s.state.Security.AnonymousRoles {
		role := s.state.Roles[roleName]
		if role == nil {
			continue
		}
		for _, privilege := range role.Privileges {
			set[privilege] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for privilege := range set {
		out = append(out, privilege)
	}
	sort.Strings(out)
	return out, true
}

// SecuritySettings returns anonymous access settings.
func (s *Store) SecuritySettings() SecuritySettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSecurity(s.state.Security)
}

// UpdateSecuritySettings replaces anonymous access settings.
func (s *Store) UpdateSecuritySettings(settings SecuritySettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Security = cloneSecurity(settings)
	return s.persistLocked()
}

// AppendAudit records a management/security event.
func (s *Store) AppendAudit(entry AuditEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.AuditSeq++
	entry.ID = s.state.AuditSeq
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	s.state.Audit = append(s.state.Audit, entry)
	if len(s.state.Audit) > maxAuditEntries {
		s.state.Audit = s.state.Audit[len(s.state.Audit)-maxAuditEntries:]
	}
	_ = s.persistLocked()
}

// AuditFilter filters audit entries.
type AuditFilter struct {
	Query      string
	User       string
	Action     string
	Repository string
	Limit      int
	Offset     int
}

// ListAudit returns newest-first audit entries.
func (s *Store) ListAudit(filter AuditFilter) ([]AuditEntry, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	matched := make([]AuditEntry, 0, len(s.state.Audit))
	for index := len(s.state.Audit) - 1; index >= 0; index-- {
		entry := s.state.Audit[index]
		if filter.User != "" && entry.User != filter.User {
			continue
		}
		if filter.Action != "" && !strings.HasPrefix(entry.Action, filter.Action) {
			continue
		}
		if filter.Repository != "" && entry.Repository != filter.Repository {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(
			entry.User+" "+entry.Action+" "+entry.Repository+" "+entry.Path+" "+entry.Message), query) {
			continue
		}
		matched = append(matched, entry)
	}
	total := len(matched)
	if filter.Offset > 0 {
		if filter.Offset >= len(matched) {
			return nil, total
		}
		matched = matched[filter.Offset:]
	}
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}
	return matched, total
}

// RandomID returns a hex identifier of n bytes.
func RandomID(n int) string {
	buffer := make([]byte, n)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

// HashToken returns the SHA-256 hex digest of a bearer token.
func HashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cloneUser(user *User) *User {
	if user == nil {
		return nil
	}
	copied := *user
	copied.Roles = append([]string{}, user.Roles...)
	copied.Groups = append([]string{}, user.Groups...)
	return &copied
}

func cloneRole(role *Role) *Role {
	if role == nil {
		return nil
	}
	copied := *role
	copied.Privileges = append([]string{}, role.Privileges...)
	return &copied
}

func cloneGroup(group *Group) *Group {
	if group == nil {
		return nil
	}
	copied := *group
	copied.Roles = append([]string{}, group.Roles...)
	return &copied
}

func cloneToken(token *Token, includeHash bool) *Token {
	if token == nil {
		return nil
	}
	copied := *token
	copied.Scopes = append([]string{}, token.Scopes...)
	if !includeHash {
		copied.Hash = ""
	}
	if token.ExpiresAt != nil {
		expiry := *token.ExpiresAt
		copied.ExpiresAt = &expiry
	}
	return &copied
}

func cloneSecurity(settings SecuritySettings) SecuritySettings {
	settings.AnonymousRoles = append([]string(nil), settings.AnonymousRoles...)
	return settings
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}
