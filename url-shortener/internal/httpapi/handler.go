package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"url-shortener/internal/shortener"
)

type Handler struct {
	service *shortener.Service
	base    string
}

type requestBody struct {
	URL string `json:"url"`
}

type responseBody struct {
	Code string `json:"code"`
	URL  string `json:"short_url"`
}

type linkResponse struct {
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

func NewHandler(service *shortener.Service, base string) http.Handler {
	h := &Handler{
		service: service,
		base:    strings.TrimRight(base, "/"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", h.shorten)
	mux.HandleFunc("GET /api/v1/links/{code}", h.getLink)
	mux.HandleFunc("GET /", h.redirect)

	return mux
}

func (h *Handler) shorten(w http.ResponseWriter, r *http.Request) {
	var data requestBody

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		sendError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	code, err := h.service.Shorten(data.URL)
	if err != nil {
		if errors.Is(err, shortener.ErrInvalidURL) {
			sendError(w, http.StatusBadRequest, "invalid URL")
			return
		}

		sendError(w, http.StatusInternalServerError, "server error")
		return
	}

	result := responseBody{
		Code: code,
		URL:  h.base + "/" + code,
	}

	sendJSON(w, http.StatusCreated, result)
}

func (h *Handler) getLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")

	link, err := h.service.Resolve(code)
	if err != nil {
		if errors.Is(err, shortener.ErrNotFound) {
			sendError(w, http.StatusNotFound, "link not found")
			return
		}

		sendError(w, http.StatusInternalServerError, "server error")
		return
	}

	result := linkResponse{
		URL:       link.URL,
		CreatedAt: link.CreatedAt.UTC().Format(time.RFC3339),
	}

	sendJSON(w, http.StatusOK, result)
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/")

	if code == "" || strings.Contains(code, "/") {
		http.NotFound(w, r)
		return
	}

	link, err := h.service.Resolve(code)
	if err != nil {
		if errors.Is(err, shortener.ErrNotFound) {
			http.NotFound(w, r)
			return
		}

		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, link.URL, http.StatusFound)
}

func sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, status int, message string) {
	sendJSON(w, status, map[string]string{
		"error": message,
	})
}
