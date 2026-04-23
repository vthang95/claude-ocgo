package routes

import (
	"encoding/json"
	"net/http"
)

func RegisterHealth(mux *http.ServeMux) {
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	})

	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
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
			},
		})
	})
}
