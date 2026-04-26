package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"chatgpt2api/internal/outboundproxy"
)

func (s *Server) handleProxyTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	proxyURL := firstNonEmpty(body.URL, s.cfg.Proxy.URL)
	if err := outboundproxy.Validate(proxyURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"status":  0,
			"latency": 0,
			"error":   err.Error(),
		})
		return
	}

	transport, err := outboundproxy.NewHTTPTransport(proxyURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"status":  0,
			"latency": 0,
			"error":   err.Error(),
		})
		return
	}

	client := &http.Client{Timeout: 15 * time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://chatgpt.com/api/auth/csrf", nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("User-Agent", "chatgpt2api-studio proxy test")

	startedAt := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      false,
			"status":  0,
			"latency": latency,
			"error":   err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	errorMessage := ""
	if resp.StatusCode >= http.StatusInternalServerError {
		errorMessage = strings.TrimSpace(http.StatusText(resp.StatusCode))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      resp.StatusCode < http.StatusInternalServerError,
		"status":  resp.StatusCode,
		"latency": latency,
		"error":   errorMessage,
	})
}
