package copilot

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const (
	GithubClientID     = "Iv1.b507a08c87ecfe98"
	GithubScopes       = "read:user"
	CopilotVersion     = "0.26.7"
	APIVersion         = "2025-04-01"
	GithubBaseURL      = "https://github.com"
	GithubAPIBaseURL   = "https://api.github.com"
	CopilotBaseURL     = "https://api.githubcopilot.com"
)

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// BaseHeaders returns the common headers for Copilot API requests.
// The caller should add Authorization: Bearer <copilot-token>.
func BaseHeaders() map[string]string {
	return map[string]string{
		"Content-Type":                         "application/json",
		"Accept":                               "application/json",
		"copilot-integration-id":               "vscode-chat",
		"editor-version":                       "vscode/1.99.3",
		"editor-plugin-version":                "copilot-chat/" + CopilotVersion,
		"user-agent":                           "GitHubCopilotChat/" + CopilotVersion,
		"openai-intent":                        "conversation-panel",
		"x-github-api-version":                 APIVersion,
		"x-request-id":                         newUUID(),
		"x-vscode-user-agent-library-version":  "electron-fetch",
	}
}
