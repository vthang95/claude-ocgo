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

func AnthropicToOpenAI(body map[string]interface{}, defaultModel string) map[string]interface{} {
	messages := make([]map[string]interface{}, 0)

	// system
	if sys, ok := body["system"]; ok {
		if s, ok := sys.(string); ok {
			messages = append(messages, map[string]interface{}{"role": "system", "content": s})
		} else if blocks, ok := sys.([]interface{}); ok {
			parts := make([]string, 0, len(blocks))
			for _, b := range blocks {
				if bm, ok := b.(map[string]interface{}); ok && bm["type"] == "text" {
					parts = append(parts, fmt.Sprint(bm["text"]))
				}
			}
			messages = append(messages, map[string]interface{}{"role": "system", "content": joinStrings(parts, "\n")})
		}
	}

	// messages
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, rawMsg := range msgs {
			msg, _ := rawMsg.(map[string]interface{})
			role, _ := msg["role"].(string)

			if role == "user" {
				content := msg["content"]
				if blocks, ok := content.([]interface{}); ok {
					toolResults := filterBlocks(blocks, "tool_result")
					otherBlocks := filterBlocksNegate(blocks, "tool_result")

					for _, tr := range toolResults {
						tm, _ := tr.(map[string]interface{})
						contentStr := extractToolResultContent(tm)
						messages = append(messages, map[string]interface{}{
							"role":         "tool",
							"tool_call_id": tm["tool_use_id"],
							"content":      contentStr,
						})
					}

					if len(otherBlocks) > 0 {
						messages = append(messages, map[string]interface{}{
							"role":    "user",
							"content": ConvertContent(otherBlocks),
						})
					}
				} else {
					messages = append(messages, map[string]interface{}{
						"role":    "user",
						"content": ConvertContent(content),
					})
				}
			} else if role == "assistant" {
				c := msg["content"]
				if s, ok := c.(string); ok {
					messages = append(messages, map[string]interface{}{"role": "assistant", "content": s})
				} else if blocks, ok := c.([]interface{}); ok {
					text := ""
					toolCalls := make([]map[string]interface{}, 0)
					for _, block := range blocks {
						bm, _ := block.(map[string]interface{})
						switch bm["type"] {
						case "text":
							text += fmt.Sprint(bm["text"])
						case "tool_use":
							input := bm["input"]
							if input == nil {
								input = map[string]interface{}{}
							}
							inputJSON, _ := json.Marshal(input)
							toolCalls = append(toolCalls, map[string]interface{}{
								"id":   bm["id"],
								"type": "function",
								"function": map[string]interface{}{
									"name":      bm["name"],
									"arguments": string(inputJSON),
								},
							})
						}
					}
					oai := map[string]interface{}{"role": "assistant"}
					if text != "" {
						oai["content"] = text
					}
					if len(toolCalls) > 0 {
						oai["tool_calls"] = toolCalls
					}
					messages = append(messages, oai)
				}
			}
		}
	}

	oai := map[string]interface{}{
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

	if tools, ok := body["tools"].([]interface{}); ok && len(tools) > 0 {
		oaiTools := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			tm, _ := t.(map[string]interface{})
			oaiTools = append(oaiTools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
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

func ConvertContent(content interface{}) interface{} {
	if s, ok := content.(string); ok {
		return s
	}
	blocks, ok := content.([]interface{})
	if !ok {
		return ""
	}

	textParts := make([]string, 0)
	imageParts := make([]map[string]interface{}, 0)

	for _, b := range blocks {
		bm, _ := b.(map[string]interface{})
		switch bm["type"] {
		case "text":
			if t, ok := bm["text"].(string); ok && t != "" {
				textParts = append(textParts, t)
			}
		case "image":
			src, _ := bm["source"].(map[string]interface{})
			imageParts = append(imageParts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
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

	result := make([]map[string]interface{}, 0)
	if mergedText != "" {
		result = append(result, map[string]interface{}{"type": "text", "text": mergedText})
	}
	for _, img := range imageParts {
		result = append(result, img)
	}
	return result
}

// OpenAI → Anthropic response conversion

func OpenAIToAnthropic(oaiResp map[string]interface{}, origBody map[string]interface{}, defaultModel string) map[string]interface{} {
	choices, _ := oaiResp["choices"].([]interface{})
	choice := map[string]interface{}{}
	msg := map[string]interface{}{}
	if len(choices) > 0 {
		choice, _ = choices[0].(map[string]interface{})
		msg, _ = choice["message"].(map[string]interface{})
	}

	content := make([]map[string]interface{}, 0)

	if text, ok := msg["content"].(string); ok && text != "" {
		content = append(content, map[string]interface{}{"type": "text", "text": text})
	}
	if reasoning, ok := msg["reasoning_content"].(string); ok && reasoning != "" {
		content = append([]map[string]interface{}{{"type": "thinking", "thinking": reasoning}}, content...)
	}
	if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
		for _, tc := range toolCalls {
			tcm, _ := tc.(map[string]interface{})
			fn, _ := tcm["function"].(map[string]interface{})
			args := "{}"
			if a, ok := fn["arguments"].(string); ok {
				args = a
			}
			var parsedInput interface{}
			json.Unmarshal([]byte(args), &parsedInput)
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    fmt.Sprint(tcm["id"]),
				"name":  fn["name"],
				"input": parsedInput,
			})
		}
	}
	if len(content) == 0 {
		content = append(content, map[string]interface{}{"type": "text", "text": ""})
	}

	msgID, _ := oaiResp["id"].(string)
	if msgID == "" {
		msgID = "msg_" + randomHex(4)
	}

	resp := map[string]interface{}{
		"id":    msgID,
		"type":  "message",
		"role":  "assistant",
		"content": content,
		"model":   getModel(oaiResp, getModel(origBody, defaultModel)),
			"stop_reason": MapFinishReason(choice["finish_reason"]),
		"stop_sequence": nil,
		"usage": map[string]interface{}{
			"input_tokens":  toInt(oaiResp, "usage", "prompt_tokens"),
			"output_tokens": toInt(oaiResp, "usage", "completion_tokens"),
		},
	}

	return resp
}

func MapFinishReason(reason interface{}) string {
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

func filterBlocks(blocks []interface{}, t string) []interface{} {
	var out []interface{}
	for _, b := range blocks {
		if bm, ok := b.(map[string]interface{}); ok && bm["type"] == t {
			out = append(out, b)
		}
	}
	return out
}

func filterBlocksNegate(blocks []interface{}, t string) []interface{} {
	var out []interface{}
	for _, b := range blocks {
		if bm, ok := b.(map[string]interface{}); ok && bm["type"] != t {
			out = append(out, b)
		}
	}
	return out
}

func extractToolResultContent(tm map[string]interface{}) string {
	c := tm["content"]
	if s, ok := c.(string); ok {
		return s
	}
	if blocks, ok := c.([]interface{}); ok {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if bm, ok := b.(map[string]interface{}); ok && bm["type"] == "text" {
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

func getModel(body map[string]interface{}, defaultModel string) string {
	if m, ok := body["model"].(string); ok && m != "" {
		return m
	}
	return defaultModel
}

func joinStrings(parts []string, sep string) string {
	return strings.Join(parts, sep)
}

func toInt(m map[string]interface{}, keys ...string) int {
	current := m
	for i, k := range keys {
		if i == len(keys)-1 {
			if v, ok := current[k].(float64); ok {
				return int(v)
			}
			return 0
		}
		next, _ := current[k].(map[string]interface{})
		current = next
	}
	return 0
}
