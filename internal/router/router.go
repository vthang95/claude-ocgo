package router

import (
	"regexp"
)

var (
	openaiRe    = regexp.MustCompile(`(?i)^qwen|^kimi|^opencode-go/qwen|^opencode-go/kimi`)
	anthropicRe = regexp.MustCompile(`(?i)^minimax|^opencode-go/minimax`)
)

func RouteModel(model string) string {
	if openaiRe.MatchString(model) {
		return "openai"
	}
	if anthropicRe.MatchString(model) {
		return "anthropic"
	}
	return "openai"
}
