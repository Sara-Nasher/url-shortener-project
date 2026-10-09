package shortener

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
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
	store          linkStore
	blockedDomains map[string]struct{}
}

func NewService(s linkStore) *Service {
	configuredDomains := strings.Split(os.Getenv("URL_BLOCKLIST_DOMAINS"), ",")
	return NewServiceWithBlockedDomains(s, configuredDomains)
}

func NewServiceWithBlockedDomains(s linkStore, domains []string) *Service {
	blocked := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = normalizeDomain(domain)
		if domain != "" {
			blocked[domain] = struct{}{}
		}
	}

	return &Service{
		store:          s,
		blockedDomains: blocked,
	}
}

func (s *Service) Shorten(value string) (string, error) {
	value = strings.TrimSpace(value)

	normalized, err := normalizeURL(value, s.blockedDomains)
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

func normalizeURL(value string, blockedDomains map[string]struct{}) (string, error) {
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

	if u.User != nil {
		return "", ErrInvalidURL
	}

	host := normalizeDomain(u.Hostname())

	if host == "" || host == "localhost" || isBlockedDomain(host, blockedDomains) {
		return "", ErrInvalidURL
	}

	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() ||
			ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() ||
			ip.IsUnspecified() {
			return "", ErrInvalidURL
		}
	}

	u.Host = strings.ToLower(u.Host)

	return u.String(), nil
}

func normalizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimSuffix(domain, ".")

	if strings.ContainsAny(domain, "/:@?#[]") {
		return ""
	}
	return domain
}

func isBlockedDomain(host string, blockedDomains map[string]struct{}) bool {
	for blocked := range blockedDomains {
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}

func makeCode() (string, error) {
	const acceptedByteLimit = 256 - (256 % len(chars))
	result := make([]byte, 6)
	var samples [32]byte
	index := len(samples)

	for i := 0; i < len(result); {
		if index >= len(samples) {
			if _, err := rand.Read(samples[:]); err != nil {
				return "", err
			}
			index = 0
		}

		sample := samples[index]
		index++
		if int(sample) >= acceptedByteLimit {
			continue
		}

		result[i] = chars[int(sample)%len(chars)]
		i++
	}

	return string(result), nil
}
