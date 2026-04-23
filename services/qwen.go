package services

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/vthang95/claude-ocgo/internal/config"
	"github.com/vthang95/claude-ocgo/internal/convert"
	"github.com/vthang95/claude-ocgo/internal/logger"
)

var httpClient = &http.Client{}

func ForwardQwen(body map[string]interface{}, w http.ResponseWriter, r *http.Request) error {
	stream := body["stream"] == true

	logger.WriteLogVerbose("QWEN_RAW_REQUEST", map[string]interface{}{
		"model":    body["model"],
		"stream":   stream,
		"msgCount": len(toSlice(body["messages"])),
	})

	openaiBody := convert.AnthropicToOpenAI(body, config.DEFAULT_MODEL)
	openaiBody["model"] = config.DEFAULT_MODEL

	if stream {
		return forwardStreaming(openaiBody, body, w, r)
	}
	return forwardNonStreaming(openaiBody, body, w, r)
}

// newUpstreamRequest builds a POST request to the upstream API at the given path.
// The caller must set auth headers and call newUpstreamRequestLog afterward.
func newUpstreamRequest(path string, bodyBytes []byte) (*http.Request, error) {
	reqURL := config.UPSTREAM_BASE + path
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func newUpstreamRequestLog(req *http.Request, bodyBytes []byte) {
	logger.WriteLogVerbose("QWEN_UPSTREAM_REQUEST", map[string]interface{}{
		"url":     req.URL.String(),
		"bodyLen": len(bodyBytes),
	})
}

func forwardNonStreaming(openaiBody, origBody map[string]interface{}, w http.ResponseWriter, r *http.Request) error {
	bodyBytes, _ := json.Marshal(openaiBody)
	req, err := newUpstreamRequest("/chat/completions", bodyBytes)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.UPSTREAM_KEY)
	newUpstreamRequestLog(req, bodyBytes)

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.WriteLog("QWEN_REQUEST_ERROR", map[string]interface{}{"message": err.Error()})
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.WriteLogVerbose("QWEN_UPSTREAM_ERROR_RESPONSE", map[string]interface{}{
			"status": resp.StatusCode,
			"body":   string(respBody[:minInt(500, len(respBody))]),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return nil
	}

	var upstreamJSON map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamJSON); err != nil {
		logger.WriteLog("QWEN_UPSTREAM_NON_JSON_200", map[string]interface{}{
			"status":      resp.StatusCode,
			"dataPreview": string(respBody[:minInt(500, len(respBody))]),
		})
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "upstream_error",
				"message": fmt.Sprintf("Upstream returned non-JSON: %s", string(respBody[:minInt(200, len(respBody))])),
			},
		})
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	result := convert.OpenAIToAnthropic(upstreamJSON, origBody, config.DEFAULT_MODEL)
	json.NewEncoder(w).Encode(result)
	return nil
}

func forwardStreaming(openaiBody, origBody map[string]interface{}, w http.ResponseWriter, r *http.Request) error {
	openaiBody["stream"] = true

	bodyBytes, _ := json.Marshal(openaiBody)
	req, err := newUpstreamRequest("/chat/completions", bodyBytes)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.UPSTREAM_KEY)
	newUpstreamRequestLog(req, bodyBytes)

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.WriteLog("QWEN_STREAM_REQUEST_ERROR", map[string]interface{}{"message": err.Error()})
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errData, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": fmt.Sprintf("Upstream %d: %s", resp.StatusCode, string(errData[:minInt(200, len(errData))])),
			},
		})
		return nil
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	streamState := &streamState{
		writer:         w,
		flusher:        flusher,
		isFirst:        true,
		nextBlockIndex: 0,
		textBlockIndex: -1,
		toolCallSlots:  make(map[int]*toolSlot),
	}

	return processStream(resp.Body, streamState, origBody)
}

type toolSlot struct {
	contentBlockIndex int
	id                string
	name              string
	started           bool
}

type streamState struct {
	writer         http.ResponseWriter
	flusher        http.Flusher
	isFirst        bool
	nextBlockIndex int
	textBlockIndex int
	toolCallSlots  map[int]*toolSlot
}

func (s *streamState) emit(event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(s.writer, "event: %s\ndata: %s\n\n", event, string(jsonData))
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *streamState) startTextBlock() {
	if s.textBlockIndex < 0 {
		s.textBlockIndex = s.nextBlockIndex
		s.nextBlockIndex++
		s.emit("content_block_start", map[string]interface{}{
			"type":  "content_block_start",
			"index": s.textBlockIndex,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		})
	}
}

func (s *streamState) handleToolCalls(toolCallDeltas []map[string]interface{}) {
	for _, tc := range toolCallDeltas {
		idx := int(tc["index"].(float64))
		slot, exists := s.toolCallSlots[idx]
		if !exists {
			slot = &toolSlot{contentBlockIndex: s.nextBlockIndex}
			s.nextBlockIndex++
			s.toolCallSlots[idx] = slot
		}
		if id, ok := tc["id"].(string); ok {
			slot.id = id
		}
		if fn, ok := tc["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				slot.name = name
			}
		}
		if !slot.started && slot.id != "" && slot.name != "" {
			s.emit("content_block_start", map[string]interface{}{
				"type":  "content_block_start",
				"index": slot.contentBlockIndex,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    slot.id,
					"name":  slot.name,
					"input": map[string]interface{}{},
				},
			})
			slot.started = true
		}
		if slot.started {
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				if args, ok := fn["arguments"].(string); ok {
					s.emit("content_block_delta", map[string]interface{}{
						"type":  "content_block_delta",
						"index": slot.contentBlockIndex,
						"delta": map[string]interface{}{
							"type":         "input_json_delta",
							"partial_json": args,
						},
					})
				}
			}
		}
	}
}

func processStream(body io.ReadCloser, s *streamState, origBody map[string]interface{}) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		if raw == "[DONE]" {
			return nil
		}

		var eventChunk map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &eventChunk); err != nil {
			continue
		}

		if s.isFirst {
			logger.WriteLog("QWEN_STREAM_FIRST_CHUNK", map[string]interface{}{"preview": line[:minInt(500, len(line))]})
			s.isFirst = false

			msgID := "msg_" + randomHex(4)
			if id, ok := eventChunk["id"].(string); ok && id != "" {
				msgID = id
			}

			model := config.DEFAULT_MODEL
			if m, ok := eventChunk["model"].(string); ok && m != "" {
				model = m
			} else if m, ok := origBody["model"].(string); ok && m != "" {
				model = m
			}

			s.emit("message_start", map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":            msgID,
					"type":          "message",
					"role":          "assistant",
					"content":       []interface{}{},
					"model":         model,
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]interface{}{
						"input_tokens":  0,
						"output_tokens": 0,
					},
				},
			})
		}

		choices, _ := eventChunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})
		if delta == nil {
			continue
		}

		if content, ok := delta["content"].(string); ok && content != "" {
			s.startTextBlock()
			s.emit("content_block_delta", map[string]interface{}{
				"type":  "content_block_delta",
				"index": s.textBlockIndex,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": content,
				},
			})
		}

		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			deltas := make([]map[string]interface{}, 0, len(toolCalls))
			for _, tc := range toolCalls {
				tcm, _ := tc.(map[string]interface{})
				deltas = append(deltas, tcm)
			}
			s.handleToolCalls(deltas)
		}

		if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
			if s.textBlockIndex >= 0 {
				s.emit("content_block_stop", map[string]interface{}{
					"type":  "content_block_stop",
					"index": s.textBlockIndex,
				})
			}
			for _, slot := range s.toolCallSlots {
				if slot.started {
					s.emit("content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": slot.contentBlockIndex,
					})
				}
			}
			outputTokens := 0
			if usage, ok := choice["usage"].(map[string]interface{}); ok {
				if ct, ok := usage["completion_tokens"].(float64); ok {
					outputTokens = int(ct)
				}
			}
			s.emit("message_delta", map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason":   convert.MapFinishReason(finishReason),
					"stop_sequence": nil,
				},
				"usage": map[string]interface{}{
					"output_tokens": outputTokens,
				},
			})
			s.emit("message_stop", map[string]interface{}{"type": "message_stop"})
			return nil
		}
	}

	return scanner.Err()
}

func toSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	s, _ := v.([]interface{})
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func randomHex(n int) string {
	bytes := make([]byte, n)
	rand.Read(bytes)
	return fmt.Sprintf("%x", bytes)
}
