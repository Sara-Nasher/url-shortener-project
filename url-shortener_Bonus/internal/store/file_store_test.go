package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")

	first, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !first.Save("https://example.com", "abc123") {
		t.Fatal("save failed")
	}

	link, err := first.GetURL("abc123")
	if err != nil {
		t.Fatal(err)
	}

	if link.URL != "https://example.com" {
		t.Fatalf("wrong url: %s", link.URL)
	}

	if link.CreatedAt.IsZero() {
		t.Fatal("created time was not saved")
	}

	createdAt := link.CreatedAt

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	code, ok := second.GetCode("https://example.com")
	if !ok {
		t.Fatal("url was not loaded after restart")
	}

	if code != "abc123" {
		t.Fatalf("wrong code after restart: %s", code)
	}

	link, err = second.GetURL("abc123")
	if err != nil {
		t.Fatal(err)
	}

	if !link.CreatedAt.Equal(createdAt) {
		t.Fatalf("created time changed after restart")
	}
}

func TestFileStoreDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")

	s, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

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

func TestFileStoreCreatedAtIsUTC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")

	s, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if !s.Save("https://example.com", "abc123") {
		t.Fatal("save failed")
	}

	link, err := s.GetURL("abc123")
	if err != nil {
		t.Fatal(err)
	}

	if link.CreatedAt.Location() != time.UTC {
		t.Fatalf("expected UTC, got %v", link.CreatedAt.Location())
	}
}

func TestFileStoreIdempotencyAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")

	first, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !first.Save("https://example.com/test", "abc123") {
		t.Fatal("save failed")
	}

	first.Close()

	second, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	code, ok := second.GetCode("https://example.com/test")
	if !ok {
		t.Fatal("url was not found after restart")
	}

	if code != "abc123" {
		t.Fatalf("expected abc123, got %s", code)
	}
}

func TestFileStoreRepairsIncompleteFinalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")
	validRecord := `{"url":"https://valid.example","code":"abc123","created_at":"2026-10-07T10:20:30Z"}` + "\n"
	incompleteRecord := `{"url":"https://unfinished.example","code":"xyz789"`
	if err := os.WriteFile(path, []byte(validRecord+incompleteRecord), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code, ok := s.GetCode("https://valid.example"); !ok || code != "abc123" {
		t.Fatalf("valid record was not loaded: code=%q ok=%v", code, ok)
	}
	if _, ok := s.GetCode("https://unfinished.example"); ok {
		t.Fatal("incomplete final record should not be loaded")
	}
	if !s.Save("https://new.example", "new123") {
		t.Fatal("save after recovery failed")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if code, ok := reopened.GetCode("https://new.example"); !ok || code != "new123" {
		t.Fatalf("new record was not preserved after restart: code=%q ok=%v", code, ok)
	}
}

func TestFileStoreRejectsMalformedCompleteFinalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")
	data := []byte(`{"url":"https://valid.example","code":"abc123","created_at":"2026-10-07T10:20:30Z"}` + "\n" + `{"url":` + "\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	s, err := NewFile(path)
	if err == nil {
		_ = s.Close()
		t.Fatal("expected malformed complete record to return an error")
	}
}

func TestFileStoreRejectsInvalidAndDuplicateRecordsOnLoad(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "missing url",
			data: `{"url":"","code":"abc123","created_at":"2026-10-07T10:20:30Z"}` + "\n",
		},
		{
			name: "duplicate url",
			data: `{"url":"https://same.example","code":"abc123","created_at":"2026-10-07T10:20:30Z"}` + "\n" + `{"url":"https://same.example","code":"xyz789","created_at":"2026-10-07T10:20:30Z"}` + "\n",
		},
		{
			name: "duplicate code",
			data: `{"url":"https://one.example","code":"abc123","created_at":"2026-10-07T10:20:30Z"}` + "\n" + `{"url":"https://two.example","code":"abc123","created_at":"2026-10-07T10:20:30Z"}` + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "links.jsonl")
			if err := os.WriteFile(path, []byte(tc.data), 0644); err != nil {
				t.Fatal(err)
			}
			if s, err := NewFile(path); err == nil {
				_ = s.Close()
				t.Fatal("expected invalid store data to fail loading")
			}
		})
	}
}

func TestFileStoreSaveAfterCloseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")
	s, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Save("https://example.com", "abc123") {
		t.Fatal("save should fail after the file is closed")
	}
}

func TestFileStoreGetURLNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")
	s, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.GetURL("missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
