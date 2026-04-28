package convert

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
)

func randomHex(n int) string {
	bytes := make([]byte, n)
	rand.Read(bytes)
	return fmt.Sprintf("%x", bytes)
}

// Anthropic → OpenAI conversion

func AnthropicToOpenAI(body map[string]any, defaultModel string) map[string]any {
	messages := make([]map[string]any, 0)

	// system
	if sys, ok := body["system"]; ok {
		if s, ok := sys.(string); ok {
			messages = append(messages, map[string]any{"role": "system", "content": s})
		} else if blocks, ok := sys.([]any); ok {
			parts := make([]string, 0, len(blocks))
			for _, b := range blocks {
				if bm, ok := b.(map[string]any); ok && bm["type"] == "text" {
					parts = append(parts, fmt.Sprint(bm["text"]))
				}
			}
			messages = append(messages, map[string]any{"role": "system", "content": joinStrings(parts, "\n")})
		}
	}

	// messages
	if msgs, ok := body["messages"].([]any); ok {
		for _, rawMsg := range msgs {
			msg, _ := rawMsg.(map[string]any)
			role, _ := msg["role"].(string)

			if role == "user" {
				content := msg["content"]
				if blocks, ok := content.([]any); ok {
					toolResults := filterBlocks(blocks, "tool_result")
					otherBlocks := filterBlocksNegate(blocks, "tool_result")

					for _, tr := range toolResults {
						tm, _ := tr.(map[string]any)
						contentStr := extractToolResultContent(tm)
						messages = append(messages, map[string]any{
							"role":         "tool",
							"tool_call_id": tm["tool_use_id"],
							"content":      contentStr,
						})
					}

					if len(otherBlocks) > 0 {
						messages = append(messages, map[string]any{
							"role":    "user",
							"content": ConvertContent(otherBlocks),
						})
					}
				} else {
					messages = append(messages, map[string]any{
						"role":    "user",
						"content": ConvertContent(content),
					})
				}
			} else if role == "assistant" {
				c := msg["content"]
				if s, ok := c.(string); ok {
					messages = append(messages, map[string]any{"role": "assistant", "content": s})
				} else if blocks, ok := c.([]any); ok {
					text := ""
					reasoning := ""
					toolCalls := make([]map[string]any, 0)
					for _, block := range blocks {
						bm, _ := block.(map[string]any)
						switch bm["type"] {
						case "text":
							text += fmt.Sprint(bm["text"])
						case "thinking":
							if t, ok := bm["thinking"].(string); ok {
								reasoning += t
							}
						case "tool_use":
							input := bm["input"]
							if input == nil {
								input = map[string]any{}
							}
							inputJSON, _ := json.Marshal(input)
							toolCalls = append(toolCalls, map[string]any{
								"id":   bm["id"],
								"type": "function",
								"function": map[string]any{
									"name":      bm["name"],
									"arguments": string(inputJSON),
								},
							})
						}
					}
					oai := map[string]any{"role": "assistant"}
					if text != "" {
						oai["content"] = text
					}
					if reasoning != "" {
						oai["reasoning_content"] = reasoning
					}
					if len(toolCalls) > 0 {
						oai["tool_calls"] = toolCalls
						// Some thinking-enabled upstreams reject assistant tool calls
						// without a reasoning_content field. Set an empty placeholder
						// when the prior turn didn't include a thinking block.
						if _, has := oai["reasoning_content"]; !has {
							oai["reasoning_content"] = ""
						}
					}
					messages = append(messages, oai)
				}
			}
		}
	}

	oai := map[string]any{
		"model":  getModel(body, defaultModel),
		"stream": false,
	}
	if len(messages) > 0 {
		oai["messages"] = messages
	}
	if v := body["max_tokens"]; v != nil {
		oai["max_tokens"] = v
	}
	if v := body["temperature"]; v != nil {
		oai["temperature"] = v
	}
	if v := body["top_p"]; v != nil {
		oai["top_p"] = v
	}
	if v := body["stop_sequences"]; v != nil {
		oai["stop"] = v
	}

	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		oaiTools := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			oaiTools = append(oaiTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tm["name"],
					"description": tm["description"],
					"parameters":  tm["input_schema"],
				},
			})
		}
		oai["tools"] = oaiTools
	}

	return oai
}

func ConvertContent(content any) any {
	if s, ok := content.(string); ok {
		return s
	}
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}

	textParts := make([]string, 0)
	imageParts := make([]map[string]any, 0)

	for _, b := range blocks {
		bm, _ := b.(map[string]any)
		switch bm["type"] {
		case "text":
			if t, ok := bm["text"].(string); ok && t != "" {
				textParts = append(textParts, t)
			}
		case "image":
			src, _ := bm["source"].(map[string]any)
			imageParts = append(imageParts, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": fmt.Sprintf("data:%s;base64,%s", src["media_type"], src["data"]),
				},
			})
		}
	}

	mergedText := joinStrings(textParts, "\n")
	if len(imageParts) == 0 {
		if mergedText == "" {
			return ""
		}
		return mergedText
	}

	result := make([]map[string]any, 0)
	if mergedText != "" {
		result = append(result, map[string]any{"type": "text", "text": mergedText})
	}
	for _, img := range imageParts {
		result = append(result, img)
	}
	return result
}

// OpenAI → Anthropic response conversion

func OpenAIToAnthropic(oaiResp map[string]any, origBody map[string]any, defaultModel string) map[string]any {
	choices, _ := oaiResp["choices"].([]any)
	choice := map[string]any{}
	msg := map[string]any{}
	if len(choices) > 0 {
		choice, _ = choices[0].(map[string]any)
		msg, _ = choice["message"].(map[string]any)
	}

	content := make([]map[string]any, 0)

	if text, ok := msg["content"].(string); ok && text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	if reasoning, ok := msg["reasoning_content"].(string); ok && reasoning != "" {
		content = append([]map[string]any{{"type": "thinking", "thinking": reasoning}}, content...)
	}
	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tc := range toolCalls {
			tcm, _ := tc.(map[string]any)
			fn, _ := tcm["function"].(map[string]any)
			args := "{}"
			if a, ok := fn["arguments"].(string); ok {
				args = a
			}
			var parsedInput any
			json.Unmarshal([]byte(args), &parsedInput)
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    fmt.Sprint(tcm["id"]),
				"name":  fn["name"],
				"input": parsedInput,
			})
		}
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}

	msgID, _ := oaiResp["id"].(string)
	if msgID == "" {
		msgID = "msg_" + randomHex(4)
	}

	resp := map[string]any{
		"id":            msgID,
		"type":          "message",
		"role":          "assistant",
		"content":       content,
		"model":         getModel(oaiResp, getModel(origBody, defaultModel)),
		"stop_reason":   MapFinishReason(choice["finish_reason"]),
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  toInt(oaiResp, "usage", "prompt_tokens"),
			"output_tokens": toInt(oaiResp, "usage", "completion_tokens"),
		},
	}

	return resp
}

func MapFinishReason(reason any) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}

// Helpers

func filterBlocks(blocks []any, t string) []any {
	var out []any
	for _, b := range blocks {
		if bm, ok := b.(map[string]any); ok && bm["type"] == t {
			out = append(out, b)
		}
	}
	return out
}

func filterBlocksNegate(blocks []any, t string) []any {
	var out []any
	for _, b := range blocks {
		if bm, ok := b.(map[string]any); ok && bm["type"] != t {
			out = append(out, b)
		}
	}
	return out
}

func extractToolResultContent(tm map[string]any) string {
	c := tm["content"]
	if s, ok := c.(string); ok {
		return s
	}
	if blocks, ok := c.([]any); ok {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if bm, ok := b.(map[string]any); ok && bm["type"] == "text" {
				parts = append(parts, fmt.Sprint(bm["text"]))
			}
		}
		return joinStrings(parts, "\n")
	}
	if c == nil {
		return ""
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func getModel(body map[string]any, defaultModel string) string {
	if m, ok := body["model"].(string); ok && m != "" {
		return m
	}
	return defaultModel
}

func joinStrings(parts []string, sep string) string {
	return strings.Join(parts, sep)
}

func toInt(m map[string]any, keys ...string) int {
	current := m
	for i, k := range keys {
		if i == len(keys)-1 {
			if v, ok := current[k].(float64); ok {
				return int(v)
			}
			return 0
		}
		next, _ := current[k].(map[string]any)
		current = next
	}
	return 0
}
