package store

import "testing"

func TestStore(t *testing.T) {
	s := New()

	if _, ok := s.GetCode("https://example.com"); ok {
		t.Fatal("url should not exist")
	}

	if !s.Save("https://example.com", "abc123") {
		t.Fatal("save failed")
	}

	code, ok := s.GetCode("https://example.com")
	if !ok || code != "abc123" {
		t.Fatalf("wrong code: %q", code)
	}

	link, err := s.GetURL("abc123")
	if err != nil {
		t.Fatal(err)
	}

	if link.URL != "https://example.com" {
		t.Fatalf("wrong url: %s", link.URL)
	}

	if link.CreatedAt.IsZero() {
		t.Fatal("created time was not saved")
	}
}

func TestDuplicate(t *testing.T) {
	s := New()

	if !s.Save("https://one.com", "abc123") {
		t.Fatal("first save failed")
	}

	if s.Save("https://one.com", "xyz789") {
		t.Fatal("duplicate url was saved")
	}

	if s.Save("https://two.com", "abc123") {
		t.Fatal("duplicate code was saved")
	}
}

func TestGetURLNotFound(t *testing.T) {
	s := New()

	_, err := s.GetURL("abcdef")

	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
