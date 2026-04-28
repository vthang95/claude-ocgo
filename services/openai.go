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

func ForwardOpenAI(body map[string]any, w http.ResponseWriter, r *http.Request) error {
	stream := body["stream"] == true
	requestedModel := ""
	if m, ok := body["model"].(string); ok {
		requestedModel = m
	}

	logger.WriteLog("OPENAI_RAW_REQUEST", map[string]any{
		"model":    requestedModel,
		"stream":   stream,
		"msgCount": len(toSlice(body["messages"])),
	})

	modelToUse := requestedModel
	if modelToUse == "" {
		modelToUse = config.DEFAULT_MODEL
	}

	openaiBody := convert.AnthropicToOpenAI(body, modelToUse)
	openaiBody["model"] = modelToUse

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
	logger.WriteLogVerbose("OPENAI_UPSTREAM_REQUEST", map[string]any{
		"url":     req.URL.String(),
		"bodyLen": len(bodyBytes),
	})
}

func forwardNonStreaming(openaiBody, origBody map[string]any, w http.ResponseWriter, r *http.Request) error {
	bodyBytes, _ := json.Marshal(openaiBody)
	req, err := newUpstreamRequest("/chat/completions", bodyBytes)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.UPSTREAM_KEY)
	newUpstreamRequestLog(req, bodyBytes)

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.WriteError("OPENAI_REQUEST_ERROR", map[string]any{"message": err.Error()})
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.WriteError("OPENAI_UPSTREAM_ERROR_RESPONSE", map[string]any{
			"status": resp.StatusCode,
			"body":   string(respBody[:minInt(500, len(respBody))]),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return nil
	}

	var upstreamJSON map[string]any
	if err := json.Unmarshal(respBody, &upstreamJSON); err != nil {
		logger.WriteError("OPENAI_UPSTREAM_NON_JSON_200", map[string]any{
			"status":      resp.StatusCode,
			"dataPreview": string(respBody[:minInt(500, len(respBody))]),
		})
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
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

func forwardStreaming(openaiBody, origBody map[string]any, w http.ResponseWriter, r *http.Request) error {
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
		logger.WriteError("OPENAI_STREAM_REQUEST_ERROR", map[string]any{"message": err.Error()})
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errData, _ := io.ReadAll(resp.Body)
		logger.WriteError("OPENAI_STREAM_UPSTREAM_ERROR", map[string]any{
			"status": resp.StatusCode,
			"body":   string(errData[:minInt(500, len(errData))]),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
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
		writer:             w,
		flusher:            flusher,
		isFirst:            true,
		nextBlockIndex:     0,
		textBlockIndex:     -1,
		thinkingBlockIndex: -1,
		toolCallSlots:      make(map[int]*toolSlot),
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
	writer             http.ResponseWriter
	flusher            http.Flusher
	isFirst            bool
	nextBlockIndex     int
	textBlockIndex     int
	thinkingBlockIndex int
	thinkingClosed     bool
	toolCallSlots      map[int]*toolSlot
}

func (s *streamState) closeThinkingIfOpen() {
	if s.thinkingBlockIndex >= 0 && !s.thinkingClosed {
		s.emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": s.thinkingBlockIndex,
		})
		s.thinkingClosed = true
	}
}

func (s *streamState) emit(event string, data any) {
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
		s.emit("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": s.textBlockIndex,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
	}
}

func (s *streamState) startThinkingBlock() {
	if s.thinkingBlockIndex < 0 {
		s.thinkingBlockIndex = s.nextBlockIndex
		s.nextBlockIndex++
		s.emit("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": s.thinkingBlockIndex,
			"content_block": map[string]any{
				"type":     "thinking",
				"thinking": "",
			},
		})
	}
}

func (s *streamState) handleToolCalls(toolCallDeltas []map[string]any) {
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
		if fn, ok := tc["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				slot.name = name
			}
		}
		if !slot.started && slot.id != "" && slot.name != "" {
			s.emit("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": slot.contentBlockIndex,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    slot.id,
					"name":  slot.name,
					"input": map[string]any{},
				},
			})
			slot.started = true
		}
		if slot.started {
			if fn, ok := tc["function"].(map[string]any); ok {
				if args, ok := fn["arguments"].(string); ok {
					s.emit("content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": slot.contentBlockIndex,
						"delta": map[string]any{
							"type":         "input_json_delta",
							"partial_json": args,
						},
					})
				}
			}
		}
	}
}

func processStream(body io.ReadCloser, s *streamState, origBody map[string]any) error {
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

		var eventChunk map[string]any
		if err := json.Unmarshal([]byte(raw), &eventChunk); err != nil {
			continue
		}

		if s.isFirst {
			logger.WriteLog("OPENAI_STREAM_FIRST_CHUNK", map[string]any{"preview": line[:minInt(500, len(line))]})
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

			s.emit("message_start", map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id":            msgID,
					"type":          "message",
					"role":          "assistant",
					"content":       []any{},
					"model":         model,
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]any{
						"input_tokens":  0,
						"output_tokens": 0,
					},
				},
			})
		}

		choices, _ := eventChunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}

		reasoning := ""
		if r, ok := delta["reasoning_content"].(string); ok {
			reasoning = r
		} else if r, ok := delta["reasoning"].(string); ok {
			reasoning = r
		}
		if reasoning != "" && !s.thinkingClosed {
			s.startThinkingBlock()
			s.emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": s.thinkingBlockIndex,
				"delta": map[string]any{
					"type":     "thinking_delta",
					"thinking": reasoning,
				},
			})
		}

		if content, ok := delta["content"].(string); ok && content != "" {
			s.closeThinkingIfOpen()
			s.startTextBlock()
			s.emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": s.textBlockIndex,
				"delta": map[string]any{
					"type": "text_delta",
					"text": content,
				},
			})
		}

		if toolCalls, ok := delta["tool_calls"].([]any); ok && len(toolCalls) > 0 {
			s.closeThinkingIfOpen()
			deltas := make([]map[string]any, 0, len(toolCalls))
			for _, tc := range toolCalls {
				tcm, _ := tc.(map[string]any)
				deltas = append(deltas, tcm)
			}
			s.handleToolCalls(deltas)
		}

		if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
			s.closeThinkingIfOpen()
			if s.textBlockIndex >= 0 {
				s.emit("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": s.textBlockIndex,
				})
			}
			for _, slot := range s.toolCallSlots {
				if slot.started {
					s.emit("content_block_stop", map[string]any{
						"type":  "content_block_stop",
						"index": slot.contentBlockIndex,
					})
				}
			}
			outputTokens := 0
			if usage, ok := choice["usage"].(map[string]any); ok {
				if ct, ok := usage["completion_tokens"].(float64); ok {
					outputTokens = int(ct)
				}
			}
			s.emit("message_delta", map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason":   convert.MapFinishReason(finishReason),
					"stop_sequence": nil,
				},
				"usage": map[string]any{
					"output_tokens": outputTokens,
				},
			})
			s.emit("message_stop", map[string]any{"type": "message_stop"})
			return nil
		}
	}

	return scanner.Err()
}

func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	s, _ := v.([]any)
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
