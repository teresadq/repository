package store

import (
	"sort"
	"time"
)

// ContentSelector is a reusable path expression referenced by privileges.
type ContentSelector struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Expression  string    `json:"expression"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ListContentSelectors returns selectors sorted by name.
func (s *Store) ListContentSelectors() []*ContentSelector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ContentSelector, 0, len(s.state.ContentSelectors))
	for _, selector := range s.state.ContentSelectors {
		copied := *selector
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetContentSelector returns one selector.
func (s *Store) GetContentSelector(name string) (*ContentSelector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	selector, ok := s.state.ContentSelectors[name]
	if !ok {
		return nil, ErrNotFound
	}
	copied := *selector
	return &copied, nil
}

// PutContentSelector creates or replaces a selector.
func (s *Store) PutContentSelector(selector *ContentSelector) (*ContentSelector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if selector.Name == "" || selector.Expression == "" {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	copied := *selector
	if existing, ok := s.state.ContentSelectors[selector.Name]; ok {
		copied.CreatedAt = existing.CreatedAt
	} else if copied.CreatedAt.IsZero() {
		copied.CreatedAt = now
	}
	copied.UpdatedAt = now
	s.state.ContentSelectors[copied.Name] = &copied
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	result := copied
	return &result, nil
}

// DeleteContentSelector removes a selector.
func (s *Store) DeleteContentSelector(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.ContentSelectors[name]; !ok {
		return ErrNotFound
	}
	delete(s.state.ContentSelectors, name)
	return s.persistLocked()
}
