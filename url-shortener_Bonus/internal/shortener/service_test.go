package shortener

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"url-shortener/internal/store"
)

type fakeStore struct {
	urls  map[string]string
	links map[string]store.Link
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		urls:  make(map[string]string),
		links: make(map[string]store.Link),
	}
}

func (f *fakeStore) GetCode(url string) (string, bool) {
	code, ok := f.urls[url]
	return code, ok
}

func (f *fakeStore) GetURL(code string) (store.Link, error) {
	link, ok := f.links[code]
	if !ok {
		return store.Link{}, store.ErrNotFound
	}

	return link, nil
}

func (f *fakeStore) Save(url, code string) bool {
	if _, ok := f.urls[url]; ok {
		return false
	}

	if _, ok := f.links[code]; ok {
		return false
	}

	f.urls[url] = code
	f.links[code] = store.Link{
		URL:       url,
		CreatedAt: time.Now().UTC(),
	}

	return true
}

func TestShorten(t *testing.T) {
	s := NewService(store.New())

	first, err := s.Shorten(" https://Example.COM/test ")
	if err != nil {
		t.Fatal(err)
	}

	second, err := s.Shorten("https://example.com/test")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf("codes are different: %s and %s", first, second)
	}

	if len(first) != 6 {
		t.Fatalf("wrong code length: %s", first)
	}

	for _, c := range first {
		if !strings.ContainsRune(chars, c) {
			t.Fatalf("invalid character in code: %s", first)
		}
	}
}

func TestShortenWithFakeStore(t *testing.T) {
	db := newFakeStore()
	s := NewService(db)

	code, err := s.Shorten("https://example.com")
	if err != nil {
		t.Fatal(err)
	}

	if code == "" {
		t.Fatal("code is empty")
	}

	if saved, ok := db.GetCode("https://example.com"); !ok || saved != code {
		t.Fatalf("url was not saved correctly")
	}
}

func TestResolve(t *testing.T) {
	db := newFakeStore()
	s := NewService(db)

	now := time.Now().UTC()

	db.urls["https://example.com"] = "abc123"
	db.links["abc123"] = store.Link{
		URL:       "https://example.com",
		CreatedAt: now,
	}

	link, err := s.Resolve("abc123")
	if err != nil {
		t.Fatal(err)
	}

	if link.URL != "https://example.com" {
		t.Fatalf("wrong url: %s", link.URL)
	}

	if !link.CreatedAt.Equal(now) {
		t.Fatalf("wrong created time")
	}
}

func TestResolveNotFound(t *testing.T) {
	s := NewService(newFakeStore())

	_, err := s.Resolve("abcdef")

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBadURL(t *testing.T) {
	values := []string{
		"",
		"   ",
		"google.com",
		"ftp://example.com",
		"://bad",
		"http://",
	}

	s := NewService(store.New())

	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			_, err := s.Shorten(value)

			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("expected ErrInvalidURL for %q, got %v", value, err)
			}
		})
	}
}

func TestSameURLConcurrently(t *testing.T) {
	s := NewService(store.New())

	const count = 50

	codes := make(chan string, count)
	errors := make(chan error, count)

	var wg sync.WaitGroup
	wg.Add(count)

	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()

			code, err := s.Shorten("https://example.com/test")
			if err != nil {
				errors <- err
				return
			}

			codes <- code
		}()
	}

	wg.Wait()

	close(codes)
	close(errors)

	for err := range errors {
		t.Fatal(err)
	}

	var code string

	for value := range codes {
		if code == "" {
			code = value
			continue
		}

		if value != code {
			t.Fatalf("different codes: %s and %s", code, value)
		}
	}
}

func TestShortenRejectsDangerousDomains(t *testing.T) {
	service := NewService(store.New())

	tests := []string{
		"http://localhost",
		"http://localhost:8080/test",
		"http://127.0.0.1",
		"http://127.0.0.1:8080/test",
		"http://10.0.0.1",
		"http://192.168.1.1",
		"http://172.16.0.1",
		"http://169.254.1.1",
		"http://0.0.0.0",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			_, err := service.Shorten(value)

			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("expected ErrInvalidURL, got %v", err)
			}
		})
	}
}

func TestShortenRejectsUserInfo(t *testing.T) {
	service := NewService(store.New())

	_, err := service.Shorten("https://user:password@example.com")

	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("expected ErrInvalidURL, got %v", err)
	}
}

func TestShortenAllowsPublicURLs(t *testing.T) {
	service := NewService(store.New())

	tests := []string{
		"https://example.com",
		"http://example.com/path",
		"https://www.google.com/search?q=golang",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			_, err := service.Shorten(value)

			if err != nil {
				t.Fatalf("expected URL to be allowed, got %v", err)
			}
		})
	}
}

func TestShortenRejectsConfiguredBlockedDomains(t *testing.T) {
	service := NewServiceWithBlockedDomains(store.New(), []string{
		" bad.example ",
		"Phishing.Example.",
	})

	tests := []string{
		"https://bad.example/path",
		"http://BAD.EXAMPLE/path",
		"https://shop.bad.example/path",
		"https://PHISHING.EXAMPLE./login",
		"https://sub.phishing.example/path",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			_, err := service.Shorten(value)
			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("expected ErrInvalidURL for blocked domain %q, got %v", value, err)
			}
		})
	}
}

func TestShortenDoesNotBlockSimilarButDifferentDomains(t *testing.T) {
	service := NewServiceWithBlockedDomains(store.New(), []string{"bad.example"})

	tests := []string{
		"https://notbad.example/path",
		"https://bad.example.attacker.test/path",
		"https://good.example/path",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			if _, err := service.Shorten(value); err != nil {
				t.Fatalf("expected domain %q to be allowed, got %v", value, err)
			}
		})
	}
}

func TestNewServiceReadsDomainBlocklistFromEnvironment(t *testing.T) {
	t.Setenv("URL_BLOCKLIST_DOMAINS", "blocked.example, phishing.example")
	service := NewService(store.New())

	for _, value := range []string{
		"https://blocked.example",
		"https://sub.phishing.example",
	} {
		if _, err := service.Shorten(value); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("expected %q to be blocked, got %v", value, err)
		}
	}
}
