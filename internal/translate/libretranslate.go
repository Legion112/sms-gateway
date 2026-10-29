package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EngineLibreTranslate is the driver name for a LibreTranslate-shaped service.
const EngineLibreTranslate = "libretranslate"

// LibreTranslate talks to a translation service on the operator's own network
// over the LibreTranslate HTTP API: POST /translate with the text and the two
// language codes, back a JSON object holding translatedText.
//
// That API was chosen over inventing one because it is the published interface
// of the self-hosted translation world, so the operator can put anything that
// speaks it behind this endpoint and swap engines without touching the gateway.
// LibreTranslate's own Argos models cannot read Uzbek at all -- its package
// index has no uz in it -- so in practice the thing behind the endpoint is an
// NLLB-200 server wearing this API, which is what deploy/translate serves.
type LibreTranslate struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewLibreTranslate builds the engine. endpoint is the service's base URL; the
// /translate path is appended if it is not already there.
func NewLibreTranslate(endpoint, apiKey string, timeout time.Duration) (*LibreTranslate, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("translate: endpoint is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("translate: endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("translate: endpoint %q: want an http or https URL", endpoint)
	}
	// The endpoint is written as a base URL, because that is how people write
	// one; the path is normalised rather than tested, so that a trailing slash
	// cannot leave it alone.
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/translate") {
		u.Path += "/translate"
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &LibreTranslate{
		endpoint: u.String(),
		apiKey:   strings.TrimSpace(apiKey),
		client:   &http.Client{Timeout: timeout, Transport: localTransport()},
	}, nil
}

// localTransport is deliberately proxy-blind.
//
// This gateway reaches Telegram through a proxy, and the host it runs on
// carries a split tunnel. Inheriting that configuration here would send message
// bodies -- one-time codes, card numbers -- out through a tunnel on their way
// to a machine on the same subnet, which is both a needless failure and exactly
// the thing the operator asked not to happen. The engine is local by
// definition, so this transport never consults a proxy.
func localTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        4,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
}

func (l *LibreTranslate) Name() string { return EngineLibreTranslate }

// Endpoint reports where this engine sends text, for the setup command.
func (l *LibreTranslate) Endpoint() string { return l.endpoint }

type translateRequest struct {
	Q      string `json:"q"`
	Source string `json:"source"`
	Target string `json:"target"`
	Format string `json:"format"`
	APIKey string `json:"api_key,omitempty"`
}

type translateResponse struct {
	TranslatedText string `json:"translatedText"`
	Error          string `json:"error"`
}

// Translate sends one message body to the service.
func (l *LibreTranslate) Translate(ctx context.Context, text, source, target string) (string, error) {
	body, err := json.Marshal(translateRequest{
		Q:      text,
		Source: source,
		Target: target,
		// "text" and not "html": an SMS body is plain, and asking for HTML
		// would invite the service to escape or re-encode characters that the
		// recipient is going to compare against the original.
		Format: "text",
		APIKey: l.apiKey,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Bounded because this is an unauthenticated service on the local network
	// and a wrong endpoint -- someone's web UI, say -- would otherwise stream
	// a page into the daemon's memory.
	raw, err := readLimited(resp.Body, 64<<10)
	if err != nil {
		return "", err
	}

	var decoded translateResponse
	// Decoding is attempted whatever the status, because this API reports its
	// own failures in the body and that message is more use than the code.
	jsonErr := json.Unmarshal(raw, &decoded)
	if decoded.Error != "" {
		return "", fmt.Errorf("translate: %s: %s", resp.Status, decoded.Error)
	}
	if resp.StatusCode != http.StatusOK {
		// The body is not quoted here. A failing service tends to echo its
		// input, and the input is the SMS.
		return "", fmt.Errorf("translate: %s from %s", resp.Status, l.endpoint)
	}
	if jsonErr != nil {
		return "", fmt.Errorf("translate: decode response from %s: %w", l.endpoint, jsonErr)
	}
	if decoded.TranslatedText == "" {
		return "", fmt.Errorf("translate: %s returned no translation", l.endpoint)
	}
	return decoded.TranslatedText, nil
}
