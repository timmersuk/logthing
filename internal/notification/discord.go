package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const discordContentLimit = 2000

type DiscordNotifier struct {
	webhook *url.URL
	client  *http.Client
}

func NewDiscord(webhookURL string, timeout time.Duration) (*DiscordNotifier, error) {
	return newDiscord(webhookURL, timeout, http.DefaultClient)
}

func newDiscord(webhookURL string, timeout time.Duration, client *http.Client) (*DiscordNotifier, error) {
	parsed, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("Discord webhook URL is invalid")
	}
	if parsed.Scheme != "https" {
		return nil, errors.New("Discord webhook URL must use HTTPS")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	configuredClient := *client
	configuredClient.Timeout = timeout
	return &DiscordNotifier{webhook: parsed, client: &configuredClient}, nil
}

func (n *DiscordNotifier) Send(ctx context.Context, message Notification) (Receipt, error) {
	content := strings.TrimSpace(message.Title)
	if body := strings.TrimSpace(message.Body); body != "" {
		if content != "" {
			content += "\n"
		}
		content += body
	}
	if link := strings.TrimSpace(message.Link); link != "" {
		content += "\n" + link
	}
	content = truncateRunes(content, discordContentLimit)
	payload, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return Receipt{}, &SendError{Permanent: true, message: "encode Discord notification"}
	}

	endpoint := *n.webhook
	query := endpoint.Query()
	query.Set("wait", "true")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return Receipt{}, &SendError{Permanent: true, message: "create Discord notification request"}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return Receipt{}, &SendError{message: "send Discord notification: " + n.safeTransportCause(err)}
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Receipt{}, &SendError{
			StatusCode: response.StatusCode,
			Permanent:  response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests,
			RetryAfter: retryAfter(response.Header.Get("Retry-After")),
			message:    fmt.Sprintf("Discord notification returned HTTP %d", response.StatusCode),
		}
	}

	externalID := response.Header.Get("X-Discord-Message-ID")
	if response.StatusCode != http.StatusNoContent {
		var result struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(responseBody, &result) == nil {
			externalID = result.ID
		}
	}
	return Receipt{Adapter: "discord", ExternalID: externalID}, nil
}

func (n *DiscordNotifier) safeTransportCause(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "request timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "request canceled"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	cause := err.Error()
	secrets := []string{n.webhook.String(), n.webhook.EscapedPath(), n.webhook.Path, n.webhook.RawQuery}
	secrets = append(secrets, strings.Split(strings.Trim(n.webhook.Path, "/"), "/")...)
	for _, secret := range secrets {
		if len(secret) >= 8 {
			cause = strings.ReplaceAll(cause, secret, "[redacted]")
		}
	}
	return cause
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
