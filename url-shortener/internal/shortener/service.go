package shortener

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"url-shortener/internal/store"
)

const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var ErrInvalidURL = errors.New("invalid url")
var ErrNotFound = errors.New("link not found")

type linkStore interface {
	GetCode(url string) (string, bool)
	GetURL(code string) (store.Link, error)
	Save(url, code string) bool
}

type Service struct {
	store linkStore
}

func NewService(s linkStore) *Service {
	return &Service{store: s}
}

func (s *Service) Shorten(value string) (string, error) {
	value = strings.TrimSpace(value)

	normalized, err := normalizeURL(value)
	if err != nil {
		return "", fmt.Errorf("cannot shorten url: %w", err)
	}

	if code, ok := s.store.GetCode(normalized); ok {
		return code, nil
	}

	for i := 0; i < 100; i++ {
		code, err := makeCode()
		if err != nil {
			return "", fmt.Errorf("could not create code: %w", err)
		}

		if s.store.Save(normalized, code) {
			return code, nil
		}

		if oldCode, ok := s.store.GetCode(normalized); ok {
			return oldCode, nil
		}
	}

	return "", errors.New("could not create a unique code")
}

func (s *Service) Resolve(code string) (store.Link, error) {
	link, err := s.store.GetURL(code)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Link{}, fmt.Errorf("code %q: %w", code, ErrNotFound)
		}

		return store.Link{}, fmt.Errorf("get link: %w", err)
	}

	return link, nil
}

func normalizeURL(value string) (string, error) {
	if value == "" {
		return "", ErrInvalidURL
	}

	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return "", ErrInvalidURL
	}

	u.Scheme = strings.ToLower(u.Scheme)

	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrInvalidURL
	}

	u.Host = strings.ToLower(u.Host)

	return u.String(), nil
}

func makeCode() (string, error) {
	data := make([]byte, 6)

	if _, err := rand.Read(data); err != nil {
		return "", err
	}

	result := make([]byte, 6)

	for i := range data {
		result[i] = chars[int(data[i])%len(chars)]
	}

	return string(result), nil
}
