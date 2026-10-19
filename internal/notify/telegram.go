package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const telegramAPIBase = "https://api.telegram.org/bot"

var httpClient = &http.Client{Timeout: 10 * time.Second}

func sendTelegram(ctx context.Context, botToken, chatID, text string) error {
	if botToken == "" || chatID == "" {
		return fmt.Errorf("bot token or chat ID is empty")
	}

	endpoint := telegramAPIBase + botToken + "/sendMessage"
	params := url.Values{
		"chat_id":    {chatID},
		"text":       {text},
		"parse_mode": {"HTML"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("create telegram request: invalid endpoint")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		// Do not expose url.Error: its URL contains the bot credential, and
		// even its underlying transport error may repeat that URL.
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("send telegram: %w", context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("send telegram: %w", context.DeadlineExceeded)
		}
		return fmt.Errorf("send telegram: transport request failed")
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("telegram API error (status %d): invalid JSON response", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || !result.OK {
		return fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, strings.ReplaceAll(result.Description, botToken, "[REDACTED]"))
	}
	return nil
}
