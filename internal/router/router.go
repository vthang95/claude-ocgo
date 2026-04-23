package router

import (
	"regexp"
)

var (
	qwenRe    = regexp.MustCompile(`(?i)^qwen`)
	minimaxRe = regexp.MustCompile(`(?i)^minimax`)
)

func RouteModel(model string) string {
	if qwenRe.MatchString(model) {
		return "qwen"
	}
	if minimaxRe.MatchString(model) {
		return "minimax"
	}
	return "qwen"
}
