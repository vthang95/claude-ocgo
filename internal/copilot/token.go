package copilot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vthang95/claude-ocgo/internal/config"
)

var (
	currentToken   string
	currentTokenMu sync.RWMutex
)

// SetCurrentToken stores the active Copilot JWT.
func SetCurrentToken(token string) {
	currentTokenMu.Lock()
	currentToken = token
	currentTokenMu.Unlock()
}

// GetCurrentToken returns the active Copilot JWT.
func GetCurrentToken() string {
	currentTokenMu.RLock()
	defer currentTokenMu.RUnlock()
	return currentToken
}

// TokenFilePath returns the path to the persisted GitHub token.
func TokenFilePath() string {
	return filepath.Join(config.ConfigDir(), "github_token")
}

// ReadGitHubToken reads the GitHub token from disk.
func ReadGitHubToken() (string, error) {
	data, err := os.ReadFile(TokenFilePath())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteGitHubToken writes the GitHub token to disk with restricted permissions.
func WriteGitHubToken(token string) error {
	path := TokenFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0600)
}

// CopilotTokenResponse is the response from GitHub's Copilot token endpoint.
type CopilotTokenResponse struct {
	Token     string `json:"token"`
	RefreshIn int    `json:"refresh_in"`
	ExpiresAt int64  `json:"expires_at"`
}

// GetCopilotToken exchanges a GitHub token for a Copilot JWT.
func GetCopilotToken(githubToken string) (*CopilotTokenResponse, error) {
	req, err := http.NewRequest("GET", GithubAPIBaseURL+"/copilot_internal/v2/token", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "token "+githubToken)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "GitHubCopilotChat/"+CopilotVersion)
	req.Header.Set("editor-version", "vscode/1.99.3")
	req.Header.Set("editor-plugin-version", "copilot-chat/"+CopilotVersion)
	req.Header.Set("x-github-api-version", APIVersion)
	req.Header.Set("x-vscode-user-agent-library-version", "electron-fetch")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("copilot token request failed: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result CopilotTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RefreshLoop runs a background goroutine that periodically refreshes the
// Copilot token. It calls onNewToken with each fresh token. The loop exits
// if fetching a token fails critically.
func RefreshLoop(githubToken string, onNewToken func(string)) {
	refresh := func() bool {
		result, err := GetCopilotToken(githubToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "copilot token refresh failed: %v\n", err)
			return false
		}
		onNewToken(result.Token)

		sleepSec := result.RefreshIn - 60
		if sleepSec < 30 {
			sleepSec = 30
		}
		time.Sleep(time.Duration(sleepSec) * time.Second)
		return true
	}

	// Run once immediately, then loop.
	if !refresh() {
		return
	}
	for {
		if !refresh() {
			return
		}
	}
}
