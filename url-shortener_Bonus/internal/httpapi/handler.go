package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"url-shortener/internal/shortener"
	"url-shortener/internal/store"
)

type Handler struct {
	service     *shortener.Service
	base        string
	rateLimiter *rateLimiter
}

type responseBody struct {
	Code string `json:"code"`
	URL  string `json:"short_url"`
}

type linkResponse struct {
	Code      string `json:"code"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

type errorResponse struct {
	Error string `json:"error"`
}

const maxShortenBodyBytes = 1 << 20 

func NewHandler(service *shortener.Service, base string) http.Handler {
	mux := http.NewServeMux()

	h := &Handler{
		service:     service,
		base:        strings.TrimRight(base, "/"),
		rateLimiter: newRateLimiter(10, time.Minute),
	}

	mux.HandleFunc("/api/shorten", h.shorten)
	mux.HandleFunc("/api/v1/links/", h.getLink)
	mux.HandleFunc("/", h.redirect)

	return loggingMiddleware(mux)
}

func (h *Handler) shorten(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ip := clientIP(r)

	if !h.rateLimiter.allow(ip) {
		retryAfter := h.rateLimiter.retryAfter(ip)
		w.Header().Set("Retry-After", formatRetryAfter(retryAfter))
		sendError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	defer r.Body.Close()

	r.Body = http.MaxBytesReader(w, r.Body, maxShortenBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req struct {
		URL string `json:"url"`
	}

	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			sendError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		sendError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}

	if req.URL == "" {
		sendError(w, http.StatusBadRequest, "url is required")
		return
	}

	code, err := h.service.Shorten(req.URL)
	if err != nil {
		if errors.Is(err, shortener.ErrInvalidURL) {
			sendError(w, http.StatusBadRequest, "invalid url")
			return
		}

		sendError(w, http.StatusInternalServerError, "could not shorten url")
		return
	}

	sendJSON(w, http.StatusCreated, responseBody{
		Code: code,
		URL:  h.base + "/" + code,
	})
}

func (h *Handler) getLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	code := strings.TrimPrefix(r.URL.Path, "/api/v1/links/")

	if code == "" || strings.Contains(code, "/") {
		sendError(w, http.StatusNotFound, "link not found")
		return
	}

	link, err := h.service.Resolve(code)
	if err != nil {
		if errors.Is(err, shortener.ErrNotFound) ||
			errors.Is(err, store.ErrNotFound) {
			sendError(w, http.StatusNotFound, "link not found")
			return
		}

		sendError(w, http.StatusInternalServerError, "could not get link")
		return
	}

	sendJSON(w, http.StatusOK, linkResponse{
		Code:      code,
		URL:       link.URL,
		CreatedAt: link.CreatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	code := strings.TrimPrefix(r.URL.Path, "/")

	if code == "" || strings.Contains(code, "/") {
		sendError(w, http.StatusNotFound, "link not found")
		return
	}

	link, err := h.service.Resolve(code)
	if err != nil {
		if errors.Is(err, shortener.ErrNotFound) ||
			errors.Is(err, store.ErrNotFound) {
			sendError(w, http.StatusNotFound, "link not found")
			return
		}

		sendError(w, http.StatusInternalServerError, "could not resolve link")
		return
	}

	http.Redirect(w, r, link.URL, http.StatusFound)
}

func sendJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}

func sendError(w http.ResponseWriter, status int, message string) {
	sendJSON(w, status, errorResponse{
		Error: message,
	})
}

func formatRetryAfter(seconds int) string {
	if seconds < 1 {
		return "1"
	}

	return strconv.Itoa(seconds)
}
