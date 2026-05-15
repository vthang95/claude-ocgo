package copilot

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type CopilotModel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Vendor string `json:"vendor"`
}

type modelsResponse struct {
	Data []CopilotModel `json:"data"`
}

var availableModels []CopilotModel

// FetchModels retrieves the list of models available from Copilot's API.
func FetchModels(token string) ([]CopilotModel, error) {
	req, err := http.NewRequest("GET", CopilotBaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "GitHubCopilotChat/"+CopilotVersion)
	req.Header.Set("editor-version", "vscode/1.99.3")
	req.Header.Set("editor-plugin-version", "copilot-chat/"+CopilotVersion)
	req.Header.Set("copilot-integration-id", "vscode-chat")
	req.Header.Set("x-github-api-version", APIVersion)
	req.Header.Set("x-request-id", newUUID())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models request failed: %d", resp.StatusCode)
	}

	var result modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	availableModels = result.Data
	return result.Data, nil
}

// AvailableModels returns the cached list of Copilot models.
func AvailableModels() []CopilotModel {
	return availableModels
}
