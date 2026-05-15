package copilot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

func postJSON(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return http.DefaultClient.Do(req)
}

// StartDeviceFlow initiates the GitHub OAuth device flow.
func StartDeviceFlow() (*deviceCodeResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id": GithubClientID,
		"scope":     GithubScopes,
	})

	resp, err := postJSON(GithubBaseURL+"/login/device/code", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed: %d", resp.StatusCode)
	}

	var result deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PollAccessToken polls GitHub until the user authorizes the device.
func PollAccessToken(deviceCode string, interval int) (string, error) {
	sleep := time.Duration(interval+1) * time.Second

	for {
		time.Sleep(sleep)

		body, _ := json.Marshal(map[string]string{
			"client_id":   GithubClientID,
			"device_code": deviceCode,
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		})

		resp, err := postJSON(GithubBaseURL+"/login/oauth/access_token", body)
		if err != nil {
			continue
		}

		var result accessTokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if result.AccessToken != "" {
			return result.AccessToken, nil
		}
	}
}

// RunAuthFlow performs the complete OAuth device flow, writes the token to
// disk, and prints instructions to the user.
func RunAuthFlow() error {
	device, err := StartDeviceFlow()
	if err != nil {
		return fmt.Errorf("failed to start device flow: %w", err)
	}

	fmt.Println("GitHub Copilot Authorization")
	fmt.Println()
	fmt.Printf("Please visit: %s\n", device.VerificationURI)
	fmt.Printf("And enter code: %s\n", device.UserCode)
	fmt.Println()
	fmt.Println("Waiting for authorization...")

	token, err := PollAccessToken(device.DeviceCode, device.Interval)
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	if err := WriteGitHubToken(token); err != nil {
		return fmt.Errorf("failed to write token: %w", err)
	}

	fmt.Println("Authorization successful!")
	fmt.Printf("Token saved to: %s\n", TokenFilePath())
	return nil
}
