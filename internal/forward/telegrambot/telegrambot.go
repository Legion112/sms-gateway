package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/legion/sms-gateway/internal/config"
)

// DriverName is the forward channel driver identifier.
const DriverName = "telegram_bot"

const (
	defaultAPIBase = "https://api.telegram.org"
	maxMessageLen  = 4096

	// minTranslationLen is the least room worth spending on a translation. A
	// handful of characters of a sentence says nothing the original did not.
	minTranslationLen = 24
	ellipsis          = "..."
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
	client, err := newHTTPClient(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("telegram_bot %q: %w", name, err)
	}
	return &Channel{
		name:    name,
		chatID:  cfg.ChatID,
		token:   token,
		client:  client,
		baseURL: defaultAPIBase,
		verbose: verbose,
	}, nil
}

func newHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("proxy_url: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport}, nil
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
	Modem       string
	From        string
	ToLine      string
	ToICCID     string
	Text        string
	Time        string
	Translation Translation
}

// Translation is machine-translated text shown under the original body.
type Translation struct {
	Text   string
	Source string
	Target string
}

// Present reports whether there is a translation to show.
func (t Translation) Present() bool { return t.Text != "" }

func formatMessage(msg SMSMessage) string {
	var b strings.Builder
	switch {
	case msg.ToLine != "":
		fmt.Fprintf(&b, "SMS to %s (%s)\n", msg.ToLine, msg.Modem)
	case msg.ToICCID != "":
		fmt.Fprintf(&b, "SMS to unknown line iccid=%s (%s)\n", msg.ToICCID, msg.Modem)
	case msg.Modem != "":
		fmt.Fprintf(&b, "SMS to unknown line iccid=unavailable (%s)\n", msg.Modem)
	default:
		b.WriteString("SMS from ")
		fmt.Fprintf(&b, "%s\n", msg.From)
		return b.String()
	}
	fmt.Fprintf(&b, "from %s\n", msg.From)
	if msg.Time != "" {
		fmt.Fprintf(&b, "%s\n", msg.Time)
	}
	if msg.Text != "" {
		b.WriteString("\n")
		b.WriteString(msg.Text)
	}
	return appendTranslation(b.String(), msg.Translation)
}

// appendTranslation puts the translation below the original, inside whatever
// room the original has left.
//
// The order and the budget are both deliberate. The original is what the
// recipient checks a one-time code or an amount against, and a machine
// translation of a bank SMS can get a currency wrong, so the original goes
// first and keeps every byte it needs. If the translation does not fit, it is
// cut, and if there is no room to cut it to, it is dropped: a truncated
// translation is a nuisance, a truncated original is a lost message.
func appendTranslation(body string, t Translation) string {
	if !t.Present() {
		return body
	}
	header := fmt.Sprintf("\n\ntranslation (%s -> %s):\n", t.Source, t.Target)
	room := maxMessageLen - len(body) - len(header)
	if room < minTranslationLen {
		return body
	}
	text := t.Text
	if len(text) > room {
		text = truncateRunes(text, room-len(ellipsis)) + ellipsis
	}
	return body + header + text
}

// truncateRunes cuts to at most limit bytes without splitting a character.
// Uzbek, Russian and the engine's own output are all multi-byte, and half a
// rune reaches Telegram as a replacement character.
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// FormatMessageForTest exposes message formatting for unit tests.
func FormatMessageForTest(msg SMSMessage) string {
	return formatMessage(msg)
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
