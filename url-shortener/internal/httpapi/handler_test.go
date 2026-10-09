package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"url-shortener/internal/shortener"
	"url-shortener/internal/store"
)

func testHandler() http.Handler {
	db := store.New()
	service := shortener.NewService(db)

	return NewHandler(service, "http://localhost:8080")
}

func TestShortenRedirect(t *testing.T) {
	handler := testHandler()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/shorten",
		strings.NewReader(`{"url":"https://go.dev/doc/"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.Code)
	}

	var data responseBody

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}

	if data.Code == "" || data.URL == "" {
		t.Fatalf("invalid response: %+v", data)
	}

	req2 := httptest.NewRequest(
		http.MethodGet,
		"/"+data.Code,
		nil,
	)

	res2 := httptest.NewRecorder()
	handler.ServeHTTP(res2, req2)

	if res2.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", res2.Code)
	}

	if location := res2.Header().Get("Location"); location != "https://go.dev/doc/" {
		t.Fatalf("wrong location: %s", location)
	}
}

func TestLinkMetadata(t *testing.T) {
	handler := testHandler()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/shorten",
		strings.NewReader(`{"url":"https://example.com/test"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.Code)
	}

	var created responseBody

	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	req2 := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/links/"+created.Code,
		nil,
	)

	res2 := httptest.NewRecorder()
	handler.ServeHTTP(res2, req2)

	if res2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res2.Code)
	}

	var result linkResponse

	if err := json.NewDecoder(res2.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	if result.URL != "https://example.com/test" {
		t.Fatalf("wrong url: %s", result.URL)
	}

	if result.CreatedAt == "" {
		t.Fatal("created_at is empty")
	}

	if _, err := time.Parse(time.RFC3339, result.CreatedAt); err != nil {
		t.Fatalf("invalid created_at: %s", result.CreatedAt)
	}
}

func TestLinkMetadataNotFound(t *testing.T) {
	handler := testHandler()

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/links/abcdef",
		nil,
	)

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	var result map[string]string

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	if result["error"] != "link not found" {
		t.Fatalf("wrong error: %q", result["error"])
	}
}

func TestBadRequests(t *testing.T) {
	values := []string{
		`{}`,
		`{"url":""}`,
		`{"url":"example.com"}`,
		`{"url":"ftp://example.com"}`,
		`not-json`,
	}

	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			handler := testHandler()

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/shorten",
				strings.NewReader(value),
			)

			req.Header.Set("Content-Type", "application/json")

			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", res.Code)
			}
		})
	}
}

func TestUnknownCode(t *testing.T) {
	handler := testHandler()

	req := httptest.NewRequest(
		http.MethodGet,
		"/abcdef",
		nil,
	)

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestIdempotency(t *testing.T) {
	handler := testHandler()

	body := `{"url":"https://example.com/test"}`

	req1 := httptest.NewRequest(
		http.MethodPost,
		"/api/shorten",
		strings.NewReader(body),
	)
	req1.Header.Set("Content-Type", "application/json")

	res1 := httptest.NewRecorder()
	handler.ServeHTTP(res1, req1)

	var first responseBody
	if err := json.NewDecoder(res1.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}

	req2 := httptest.NewRequest(
		http.MethodPost,
		"/api/shorten",
		strings.NewReader(body),
	)
	req2.Header.Set("Content-Type", "application/json")

	res2 := httptest.NewRecorder()
	handler.ServeHTTP(res2, req2)

	var second responseBody
	if err := json.NewDecoder(res2.Body).Decode(&second); err != nil {
		t.Fatal(err)
	}

	if first.Code != second.Code {
		t.Fatalf("different codes: %s and %s", first.Code, second.Code)
	}

	if first.URL != second.URL {
		t.Fatalf("different short urls: %s and %s", first.URL, second.URL)
	}
}
