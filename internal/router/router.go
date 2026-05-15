package router

import (
	"regexp"

	"github.com/vthang95/claude-ocgo/internal/config"
)

var (
	openaiRe    = regexp.MustCompile(`(?i)^qwen|^kimi|^glm-5|^deepseek-|^opencode-go/qwen|^opencode-go/kimi|^opencode-go/glm`)
	anthropicRe = regexp.MustCompile(`(?i)^minimax|^opencode-go/minimax`)
	copilotRe   = regexp.MustCompile(`(?i)^claude-`)
)

func RouteModel(model string) string {
	if openaiRe.MatchString(model) {
		return "openai"
	}
	if anthropicRe.MatchString(model) {
		return "anthropic"
	}
	if copilotRe.MatchString(model) {
		return "copilot"
	}
	// Unrecognized models: fallback to the configured default provider.
	if config.PROVIDER == "copilot" {
		return "copilot"
	}
	return "openai"
}
