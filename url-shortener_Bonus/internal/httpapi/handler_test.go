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

func TestShortenRejectsMalformedAndAmbiguousJSON(t *testing.T) {
	values := []string{
		`{"url":"https://example.com"} {"url":"https://second.example"}`,
		`{"url":"https://example.com"} trailing`,
		`{"url":"https://example.com","unexpected":true}`,
	}

	for _, body := range values {
		t.Run(body, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(body))
			res := httptest.NewRecorder()
			testHandler().ServeHTTP(res, req)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d; body=%s", res.Code, res.Body.String())
			}
		})
	}
}

func TestShortenRejectsOversizedBody(t *testing.T) {
	body := `{"url":"https://example.com","padding":"` + strings.Repeat("x", maxShortenBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(body))
	res := httptest.NewRecorder()
	testHandler().ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d; body=%s", res.Code, res.Body.String())
	}
}

func TestRouteMethodsAndMalformedPaths(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   int
		allow  string
	}{
		{name: "shorten method", method: http.MethodGet, path: "/api/shorten", want: http.StatusMethodNotAllowed, allow: http.MethodPost},
		{name: "metadata method", method: http.MethodPost, path: "/api/v1/links/abc123", want: http.StatusMethodNotAllowed, allow: http.MethodGet},
		{name: "metadata empty code", method: http.MethodGet, path: "/api/v1/links/", want: http.StatusNotFound},
		{name: "metadata nested path", method: http.MethodGet, path: "/api/v1/links/abc/def", want: http.StatusNotFound},
		{name: "redirect method", method: http.MethodPost, path: "/abc123", want: http.StatusMethodNotAllowed, allow: http.MethodGet},
		{name: "redirect empty code", method: http.MethodGet, path: "/", want: http.StatusNotFound},
		{name: "redirect nested path", method: http.MethodGet, path: "/abc/def", want: http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			res := httptest.NewRecorder()
			testHandler().ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("expected status %d, got %d; body=%s", tc.want, res.Code, res.Body.String())
			}
			if tc.allow != "" && res.Header().Get("Allow") != tc.allow {
				t.Fatalf("expected Allow=%q, got %q", tc.allow, res.Header().Get("Allow"))
			}
		})
	}
}

func TestResponseHelpersAndClientIP(t *testing.T) {
	if got := formatRetryAfter(0); got != "1" {
		t.Fatalf("formatRetryAfter(0) = %q, want 1", got)
	}
	if got := formatRetryAfter(-3); got != "1" {
		t.Fatalf("formatRetryAfter(-3) = %q, want 1", got)
	}
	if got := formatRetryAfter(12); got != "12" {
		t.Fatalf("formatRetryAfter(12) = %q, want 12", got)
	}

	for _, tc := range []struct {
		remote string
		want   string
	}{
		{remote: "192.0.2.10:4321", want: "192.0.2.10"},
		{remote: "[2001:db8::1]:4321", want: "2001:db8::1"},
		{remote: "not-a-host-port", want: "not-a-host-port"},
		{remote: "", want: "unknown"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tc.remote
		if got := clientIP(req); got != tc.want {
			t.Errorf("clientIP(%q) = %q, want %q", tc.remote, got, tc.want)
		}
	}
}
