package router

import (
	"regexp"
)

var (
	openaiRe    = regexp.MustCompile(`(?i)^qwen|^kimi|^glm-5|^opencode-go/qwen|^opencode-go/kimi|^opencode-go/glm`)
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
