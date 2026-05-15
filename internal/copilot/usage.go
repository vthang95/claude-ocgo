package copilot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// FlexInt accepts both integer and float JSON numbers (e.g., 0 and 0.0).
type FlexInt int

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || b[0] == 'n' {
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = FlexInt(v)
	return nil
}

// MarshalJSON encodes FlexInt as an integer.
func (f FlexInt) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(int(f))), nil
}

type QuotaDetail struct {
	Entitlement      FlexInt `json:"entitlement"`
	OverageCount     FlexInt `json:"overage_count"`
	OveragePermitted bool    `json:"overage_permitted"`
	PercentRemaining float64 `json:"percent_remaining"`
	QuotaID          string  `json:"quota_id"`
	QuotaRemaining   FlexInt `json:"quota_remaining"`
	Remaining        FlexInt `json:"remaining"`
	Unlimited        bool    `json:"unlimited"`
}

type UsageResponse struct {
	AccessTypeSKU        string `json:"access_type_sku"`
	AnalyticsTrackingID  string `json:"analytics_tracking_id"`
	AssignedDate         string `json:"assigned_date"`
	CopilotPlan          string `json:"copilot_plan"`
	QuotaResetDate       string `json:"quota_reset_date"`
	QuotaSnapshots       struct {
		Chat              QuotaDetail `json:"chat"`
		Completions       QuotaDetail `json:"completions"`
		PremiumInteractions QuotaDetail `json:"premium_interactions"`
	} `json:"quota_snapshots"`
}

// GetUsage fetches the current Copilot usage/quota from GitHub.
func GetUsage(githubToken string) (*UsageResponse, error) {
	req, err := http.NewRequest("GET", GithubAPIBaseURL+"/copilot_internal/user", nil)
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
		return nil, fmt.Errorf("usage request failed: %d", resp.StatusCode)
	}

	var result UsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// FormatUsageSummary returns a human-readable summary of the usage data.
func FormatUsageSummary(u *UsageResponse) string {
	premium := u.QuotaSnapshots.PremiumInteractions
	premiumTotal := int(premium.Entitlement)
	premiumUsed := premiumTotal - int(premium.Remaining)
	premiumPct := 0.0
	if premiumTotal > 0 {
		premiumPct = float64(premiumUsed) / float64(premiumTotal) * 100
	}

	chat := u.QuotaSnapshots.Chat
	chatTotal := int(chat.Entitlement)
	chatUsed := chatTotal - int(chat.Remaining)
	chatPct := 0.0
	if chatTotal > 0 {
		chatPct = float64(chatUsed) / float64(chatTotal) * 100
	}

	completions := u.QuotaSnapshots.Completions
	compTotal := int(completions.Entitlement)
	compUsed := compTotal - int(completions.Remaining)
	compPct := 0.0
	if compTotal > 0 {
		compPct = float64(compUsed) / float64(compTotal) * 100
	}

	return fmt.Sprintf(`Copilot Usage (plan: %s)
Quota resets: %s

Premium Requests: %d/%d used (%.1f%%)
Chat:            %d/%d used (%.1f%%)
Completions:     %d/%d used (%.1f%%)`,
		u.CopilotPlan, u.QuotaResetDate,
		premiumUsed, premiumTotal, premiumPct,
		chatUsed, chatTotal, chatPct,
		compUsed, compTotal, compPct,
	)
}
