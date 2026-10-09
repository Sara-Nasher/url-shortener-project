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

	first, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !first.Save("https://valid.example", "valid1") {
		t.Fatal("could not save initial valid record")
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.WriteString(
		`{"url":"https://partial.example","code":`,
	); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := NewFile(path)
	if err != nil {
		t.Fatalf("store should recover incomplete final record: %v", err)
	}

	if _, ok := second.GetCode("https://valid.example"); !ok {
		t.Fatal("valid record was not recovered")
	}

	if _, ok := second.GetCode("https://partial.example"); ok {
		t.Fatal("incomplete record must not be loaded")
	}

	if !second.Save("https://new.example", "new123") {
		t.Fatal("save after recovery failed")
	}

	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	third, err := NewFile(path)
	if err != nil {
		t.Fatalf("store should reopen after recovery and append: %v", err)
	}
	defer third.Close()

	if _, ok := third.GetCode("https://valid.example"); !ok {
		t.Fatal("original valid record was lost")
	}

	if code, ok := third.GetCode("https://new.example"); !ok || code != "new123" {
		t.Fatalf("new record was not recovered: code=%q ok=%v", code, ok)
	}
}
