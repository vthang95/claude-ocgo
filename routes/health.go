package routes

import (
	"encoding/json"
	"net/http"

	"github.com/vthang95/claude-ocgo/internal/copilot"
)

var opencodeModels = []map[string]any{
	{"id": "qwen3.6-plus", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "qwen3.5-plus", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "glm-5.1", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "glm-5", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "kimi-k2.5", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "kimi-k2.6", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "minimax-m2.7", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "minimax-m2.5", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "mimo-v2-pro", "object": "model", "created": 0, "owned_by": "opencode-go"},
	{"id": "mimo-v2-omni", "object": "model", "created": 0, "owned_by": "opencode-go"},
}

func RegisterHealth(mux *http.ServeMux) {
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		models := opencodeModels

		// Add dynamically discovered Copilot models if available.
		if copilot.GetCurrentToken() != "" {
			for _, m := range copilot.AvailableModels() {
				models = append(models, map[string]any{
					"id":        m.ID,
					"object":    "model",
					"created":   0,
					"owned_by":  m.Vendor,
				})
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   models,
		})
	})
}
