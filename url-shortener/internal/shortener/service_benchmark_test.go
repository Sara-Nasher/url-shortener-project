package shortener

import (
	"fmt"
	"testing"

	"url-shortener/internal/store"
)

func BenchmarkShorten(b *testing.B) {
	service := NewService(store.New())

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		url := fmt.Sprintf("https://example.com/page/%d", i)

		if _, err := service.Shorten(url); err != nil {
			b.Fatal(err)
		}
	}
}
