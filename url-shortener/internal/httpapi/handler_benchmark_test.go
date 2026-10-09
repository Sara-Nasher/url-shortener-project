package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"url-shortener/internal/shortener"
	"url-shortener/internal/store"
)

func BenchmarkRedirect(b *testing.B) {
	db := store.New()
	service := shortener.NewService(db)
	handler := NewHandler(service, "http://localhost:8080")

	code, err := service.Shorten("https://example.com/benchmark")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(
			http.MethodGet,
			"/"+code,
			nil,
		)

		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		if res.Code != http.StatusFound {
			b.Fatalf("expected 302, got %d", res.Code)
		}

		if location := res.Header().Get("Location"); location != "https://example.com/benchmark" {
			b.Fatalf("wrong location: %s", location)
		}
	}
}
