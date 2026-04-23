package services

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/vthang95/claude-ocgo/internal/config"
	"github.com/vthang95/claude-ocgo/internal/logger"
)

func ForwardMiniMax(body map[string]interface{}, w http.ResponseWriter, r *http.Request) error {
	stream := body["stream"] == true

	body["model"] = config.DEFAULT_MODEL
	bodyBytes, _ := json.Marshal(body)
	logger.WriteLog("MINIMAX_REQUEST", map[string]interface{}{
		"model":   body["model"],
		"stream":  stream,
		"bodyLen": len(bodyBytes),
	})

	if stream {
		return forwardMiniMaxStream(w, bodyBytes)
	}
	return forwardMiniMaxNonStream(w, bodyBytes)
}

func upstreamPath() string {
	u, _ := url.Parse(config.UPSTREAM_BASE)
	return u.Path + "/messages"
}

func upstreamHost() string {
	u, _ := url.Parse(config.UPSTREAM_BASE)
	return u.Host
}

func upstreamScheme() string {
	u, _ := url.Parse(config.UPSTREAM_BASE)
	return u.Scheme
}

// newMiniMaxRequest builds a POST request to the upstream MiniMax API.
// The caller must add any additional headers (e.g. Content-Length).
func newMiniMaxRequest(bodyBytes []byte) (*http.Request, error) {
	reqURL := config.UPSTREAM_BASE + "/messages"
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", config.UPSTREAM_KEY)
	req.Header.Set("anthropic-version", "2023-06-01")
	return req, nil
}

func forwardMiniMaxNonStream(w http.ResponseWriter, bodyBytes []byte) error {
	req, err := newMiniMaxRequest(bodyBytes)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.WriteLog("MINIMAX_NONSTREAM_ERROR", map[string]interface{}{"message": err.Error()})
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": err.Error(),
			},
		})
		return nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	logger.WriteLog("MINIMAX_NONSTREAM_UPSTREAM", map[string]interface{}{
		"status": resp.StatusCode,
		"bytes":  len(respBody),
	})

	if resp.StatusCode >= 400 {
		var parsed map[string]interface{}
		if json.Unmarshal(respBody, &parsed) == nil {
			if _, ok := parsed["error"]; ok {
				logger.WriteLog("MINIMAX_ERROR_RESPONSE", map[string]interface{}{"error": parsed["error"]})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
	return nil
}

func forwardMiniMaxStream(w http.ResponseWriter, bodyBytes []byte) error {
	req, err := newMiniMaxRequest(bodyBytes)
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.WriteLog("MINIMAX_STREAM_ERROR", map[string]interface{}{"message": err.Error()})
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", mustMarshal(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": err.Error(),
			},
		}))
		return nil
	}
	defer resp.Body.Close()

	logger.WriteLog("MINIMAX_STREAM_UPSTREAM", map[string]interface{}{"status": resp.StatusCode})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(w, line)
		if flusher != nil {
			flusher.Flush()
		}
	}

	logger.WriteLog("MINIMAX_STREAM_DONE", map[string]interface{}{})
	return scanner.Err()
}

func mustMarshal(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
