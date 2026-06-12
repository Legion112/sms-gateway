package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/legion/sms-gateway/internal/config"
)

// DriverName is the forward channel driver identifier.
const DriverName = "telegram_bot"

const (
	defaultAPIBase = "https://api.telegram.org"
	maxMessageLen  = 4096
)

// Client calls the Telegram Bot API.
type Client interface {
	Do(req *http.Request) (*http.Response, error)
}

// Channel forwards SMS notifications via Telegram Bot API private chat.
type Channel struct {
	name    string
	chatID  int64
	token   string
	client  Client
	baseURL string
	verbose bool
}

// New creates a telegram_bot forward channel.
func New(name string, cfg config.TelegramChannelConfig, token string, verbose bool) (*Channel, error) {
	if token == "" {
		return nil, fmt.Errorf("telegram_bot %q: bot_token is required (config or SMS_GATEWAY_CHANNEL_%s_BOT_TOKEN)", name, envChannelKey(name))
	}
	if cfg.ChatID == 0 {
		return nil, fmt.Errorf("telegram_bot %q: chat_id is required", name)
	}
	return &Channel{
		name:    name,
		chatID:  cfg.ChatID,
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: defaultAPIBase,
		verbose: verbose,
	}, nil
}

// NewWithClient creates a channel with a custom HTTP client (tests).
func NewWithClient(name string, cfg config.TelegramChannelConfig, token string, client Client, baseURL string) (*Channel, error) {
	ch, err := New(name, cfg, token, false)
	if err != nil {
		return nil, err
	}
	ch.client = client
	if baseURL != "" {
		ch.baseURL = baseURL
	}
	return ch, nil
}

func (c *Channel) Name() string   { return c.name }
func (c *Channel) Driver() string { return DriverName }

// Ping calls getMe to verify bot token.
func (c *Channel) Ping(ctx context.Context) error {
	var resp apiResponse
	if err := c.call(ctx, "getMe", nil, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram getMe: %s", resp.Description)
	}
	return nil
}

// Forward sends a formatted SMS notification.
func (c *Channel) Forward(ctx context.Context, msg SMSMessage) error {
	return c.SendText(ctx, formatMessage(msg))
}

// SendText sends a plain message as-is (used by channel test CLI).
func (c *Channel) SendText(ctx context.Context, text string) error {
	if len(text) > maxMessageLen {
		text = text[:maxMessageLen-3] + "..."
	}
	payload := map[string]any{
		"chat_id": c.chatID,
		"text":    text,
	}
	var resp apiResponse
	if err := c.call(ctx, "sendMessage", payload, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram sendMessage: %s", resp.Description)
	}
	return nil
}

func (c *Channel) Close() error { return nil }

// SMSMessage is the inbound SMS payload for formatting.
type SMSMessage struct {
	From string
	Text string
	Time string
}

func formatMessage(msg SMSMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SMS from %s\n", msg.From)
	if msg.Time != "" {
		fmt.Fprintf(&b, "%s\n", msg.Time)
	}
	if msg.Text != "" {
		b.WriteString("\n")
		b.WriteString(msg.Text)
	}
	return b.String()
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

func (c *Channel) call(ctx context.Context, method string, body any, out *apiResponse) error {
	url := fmt.Sprintf("%s/bot%s/%s", strings.TrimRight(c.baseURL, "/"), c.token, method)
	var req *http.Request
	var err error
	if body == nil {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
	} else {
		data, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return marshalErr
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if c.verbose {
		fmt.Printf("telegram %s: HTTP %d %s\n", method, resp.StatusCode, string(raw))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		time.Sleep(time.Second)
		return c.call(ctx, method, body, out)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("telegram %s: decode response: %w", method, err)
	}
	return nil
}

func envChannelKey(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}
