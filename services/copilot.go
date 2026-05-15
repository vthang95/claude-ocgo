package services

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/vthang95/claude-ocgo/internal/config"
	"github.com/vthang95/claude-ocgo/internal/copilot"
)

func ForwardCopilot(body map[string]any, w http.ResponseWriter, r *http.Request) error {
	token := copilot.GetCurrentToken()
	if token == "" {
		return fmt.Errorf("no copilot token available; run 'ocgo copilot auth' to authenticate")
	}

	// Translate model name before passing to generic forwarder.
	if m, ok := body["model"].(string); ok {
		body["model"] = translateCopilotModel(m)
	}

	// Determine X-Initiator based on message roles.
	initiator := "user"
	if msgs, ok := body["messages"].([]any); ok {
		for _, raw := range msgs {
			msg, _ := raw.(map[string]any)
			role, _ := msg["role"].(string)
			if role == "assistant" || role == "tool" {
				initiator = "agent"
				break
			}
		}
	}

	extraHeaders := copilot.BaseHeaders()
	extraHeaders["X-Initiator"] = initiator

	return ForwardGeneric(body, w, r, config.COPILOT_BASE_URL, token, extraHeaders, "COPILOT", config.COPILOT_MODEL)
}

func translateCopilotModel(model string) string {
	// First, try to find an exact match in available models.
	available := copilot.AvailableModels()
	for _, m := range available {
		if strings.EqualFold(m.ID, model) {
			return m.ID
		}
	}

	// For Claude Code's versioned model names like claude-sonnet-4-6,
	// strip the trailing version and try to find a match.
	shortened := model
	if strings.HasPrefix(model, "claude-sonnet-4-") {
		shortened = "claude-sonnet-4"
	} else if strings.HasPrefix(model, "claude-opus-4-") {
		shortened = "claude-opus-4"
	}

	// Check if the shortened name is in the available list.
	if shortened != model {
		for _, m := range available {
			if strings.EqualFold(m.ID, shortened) {
				return m.ID
			}
		}
		// Not found. Look for a model that starts with the same prefix
		// (e.g., claude-sonnet-4-20250506 for claude-sonnet-4).
		for _, m := range available {
			if strings.HasPrefix(strings.ToLower(m.ID), strings.ToLower(shortened)) {
				return m.ID
			}
		}
	}

	return model
}
