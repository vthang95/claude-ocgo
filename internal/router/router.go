package router

import (
	"regexp"
)

var (
	qwenRe    = regexp.MustCompile(`(?i)^qwen`)
	minimaxRe = regexp.MustCompile(`(?i)^minimax`)
	kimiRe    = regexp.MustCompile(`(?i)^kimi`)
)

func RouteModel(model string) string {
	if qwenRe.MatchString(model) {
		return "qwen"
	}
	if minimaxRe.MatchString(model) {
		return "minimax"
	}
	if kimiRe.MatchString(model) {
		return "qwen"
	}
	return "qwen"
}
