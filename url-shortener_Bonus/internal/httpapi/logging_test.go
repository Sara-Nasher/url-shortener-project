package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingResponseWriterTracksFirstStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &loggingResponseWriter{ResponseWriter: recorder}
	writer.WriteHeader(http.StatusCreated)
	writer.WriteHeader(http.StatusInternalServerError)

	if writer.status != http.StatusCreated {
		t.Fatalf("logged status = %d, want first status %d", writer.status, http.StatusCreated)
	}
	if recorder.Code != http.StatusCreated {
		t.Fatalf("underlying status = %d, want %d", recorder.Code, http.StatusCreated)
	}
}

func TestLoggingResponseWriterImplicitOK(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &loggingResponseWriter{ResponseWriter: recorder}
	if _, err := writer.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if writer.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", writer.status, http.StatusOK)
	}
}

func TestLoggingMiddlewareDefaultStatus(t *testing.T) {
	handler := loggingMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
}
