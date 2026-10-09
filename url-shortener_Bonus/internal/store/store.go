package store

import (
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("link not found")

type Link struct {
	URL       string
	CreatedAt time.Time
}

type Store struct {
	mu    sync.RWMutex
	urls  map[string]string
	links map[string]Link
}

func New() *Store {
	return &Store{
		urls:  make(map[string]string),
		links: make(map[string]Link),
	}
}

func (s *Store) GetCode(url string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	code, ok := s.urls[url]
	return code, ok
}

func (s *Store) GetURL(code string) (Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	link, ok := s.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}

	return link, nil
}

func (s *Store) Save(url, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.urls[url]; ok {
		return false
	}

	if _, ok := s.links[code]; ok {
		return false
	}

	s.urls[url] = code
	s.links[code] = Link{
		URL:       url,
		CreatedAt: time.Now().UTC(),
	}

	return true
}
