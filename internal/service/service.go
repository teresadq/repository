// Package service implements the Go module proxy protocol for hosted, proxy
// and group repositories. The behavior follows kkRepo's Java GoHostedService,
// GoProxyService and GoGroupService.
package service

import (
	"fmt"
	"io"
	"sync"
	"time"

	"kkrepo-go/internal/gomod"
	"kkrepo-go/internal/signing"
	"kkrepo-go/internal/store"
	"kkrepo-go/internal/upstream"
)

// Service serves Go module proxy requests.
type Service struct {
	store   *store.Store
	fetcher *upstream.Fetcher
	now     func() time.Time

	sessionMu      sync.Mutex
	pubSessions    map[string]*pubUpload
	dockerSessions map[string]*dockerUpload

	aptSigner    *signing.APT
	alpineSigner *signing.Alpine
}

// New creates a Go protocol service.
func New(st *store.Store) *Service {
	return &Service{
		store:          st,
		fetcher:        upstream.New(),
		now:            func() time.Time { return time.Now().UTC() },
		pubSessions:    map[string]*pubUpload{},
		dockerSessions: map[string]*dockerUpload{},
	}
}

// Store exposes the underlying metadata store for management APIs.
func (s *Service) Store() *store.Store { return s.store }

// SetSigningKeys configures APT OpenPGP and Alpine RSA signing keys.
func (s *Service) SetSigningKeys(apt *signing.APT, alpine *signing.Alpine) {
	s.aptSigner = apt
	s.alpineSigner = alpine
}

// Serve dispatches a GET/HEAD request under /repository/<repo>/.
func (s *Service) Serve(repositoryName, rawPath string, headOnly bool) (*Response, error) {
	repository, err := s.store.GetRepository(repositoryName)
	if err != nil {
		return nil, NotFound("Repository not found: " + repositoryName)
	}
	if !repository.Online {
		return nil, NotFound("Repository is offline: " + repositoryName)
	}
	switch repository.Type {
	case "hosted":
		return s.serveHosted(repository, rawPath, headOnly)
	case "proxy":
		return s.serveProxy(repository, rawPath, headOnly)
	case "group":
		return s.serveGroup(repository, rawPath, headOnly)
	default:
		return nil, MethodNotAllowed("Unsupported repository type: " + repository.Type)
	}
}

// Publish handles a hosted module upload at <version>.zip.
func (s *Service) Publish(repositoryName, rawPath string, body io.Reader) (*Published, error) {
	repository, err := s.store.GetRepository(repositoryName)
	if err != nil {
		return nil, NotFound("Repository not found: " + repositoryName)
	}
	return s.publishHosted(repository, rawPath, body)
}

func parsePath(rawPath string) (gomod.GoPath, error) {
	path, err := gomod.ParseGoPath(rawPath)
	if err != nil {
		return gomod.GoPath{}, NotFound(err.Error())
	}
	return path, nil
}

func asServiceError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Error); ok {
		return err
	}
	return fmt.Errorf("%w", err)
}
