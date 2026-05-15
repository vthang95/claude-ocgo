package routes

import (
	"encoding/json"
	"net/http"

	"github.com/vthang95/claude-ocgo/internal/config"
	"github.com/vthang95/claude-ocgo/internal/copilot"
)

func RegisterUsage(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/usage", func(w http.ResponseWriter, r *http.Request) {
		githubToken := config.COPILOT_GITHUB_TOKEN
		if githubToken == "" {
			tok, err := copilot.ReadGitHubToken()
			if err != nil || tok == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{
					"error": "no GitHub token found; run 'ocgo copilot auth' to authenticate",
				})
				return
			}
			githubToken = tok
		}

		usage, err := copilot.GetUsage(githubToken)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]any{
				"error": err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usage)
	})
}
