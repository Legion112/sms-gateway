package telegrambot_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward/telegrambot"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPingAndForward(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/getMe") {
			return jsonResp(`{"ok":true,"result":{"id":1}}`), nil
		}
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), `"chat_id":42`) {
			t.Fatalf("body %q missing chat_id", body)
		}
		return jsonResp(`{"ok":true,"result":{"message_id":1}}`), nil
	})}

	ch, err := telegrambot.NewWithClient("test", config.TelegramChannelConfig{ChatID: 42}, "token", client, "http://telegram.test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ch.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ch.Forward(ctx, telegrambot.SMSMessage{
		From: "+79162821457",
		Time: "2026-06-11 20:00:00",
		Text: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ch.SendText(ctx, "plain test"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls=%v", calls)
	}
}

func jsonResp(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestNewRequiresTokenAndChatID(t *testing.T) {
	if _, err := telegrambot.New("x", config.TelegramChannelConfig{}, "", false); err == nil {
		t.Fatal("expected error without token")
	}
	if _, err := telegrambot.New("x", config.TelegramChannelConfig{ChatID: 1}, "", false); err == nil {
		t.Fatal("expected error without token")
	}
}

func TestAPIError(t *testing.T) {
	client := &http.Client{Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
		return jsonResp(`{"ok":false,"description":"bad token"}`), nil
	})}
	ch, err := telegrambot.NewWithClient("test", config.TelegramChannelConfig{ChatID: 1}, "bad", client, "http://telegram.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("err=%v", err)
	}
}
