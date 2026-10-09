package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"url-shortener/internal/shortener"
	"url-shortener/internal/store"
)

func TestShortenPersistsBefore201(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")

	db, err := store.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}

	service := shortener.NewService(db)
	handler := NewHandler(service, "http://localhost:8080")

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/shorten",
		strings.NewReader(`{"url":"https://example.com/persisted"}`),
	)

	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.Code)
	}

	var response struct {
		Code string `json:"code"`
	}

	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}

	if response.Code == "" {
		t.Fatal("code is empty")
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := store.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()

	link, err := restarted.GetURL(response.Code)
	if err != nil {
		t.Fatal(err)
	}

	if link.URL != "https://example.com/persisted" {
		t.Fatalf("wrong persisted url: %s", link.URL)
	}
}
