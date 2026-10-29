package translate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newEngine(t *testing.T, srv *httptest.Server) *LibreTranslate {
	t.Helper()
	eng, err := NewLibreTranslate(srv.URL, "", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestEngineSendsTheLibreTranslateShape(t *testing.T) {
	var got translateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/translate" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %s", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(translateResponse{TranslatedText: "a safe home"})
	}))
	defer srv.Close()

	out, err := newEngine(t, srv).Translate(context.Background(), "xavfsiz xonadon", "uz", "en")
	if err != nil {
		t.Fatal(err)
	}
	if out != "a safe home" {
		t.Errorf("out = %q", out)
	}
	if got.Q != "xavfsiz xonadon" || got.Source != "uz" || got.Target != "en" {
		t.Errorf("request = %+v", got)
	}
	// Plain, not HTML: the recipient compares this against the original, and
	// an escaped body would not match.
	if got.Format != "text" {
		t.Errorf("format = %q, want text", got.Format)
	}
	if got.APIKey != "" {
		t.Errorf("sent an api_key that was not configured: %q", got.APIKey)
	}
}

func TestEngineSendsTheAPIKeyWhenSet(t *testing.T) {
	var got translateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		json.NewEncoder(w).Encode(translateResponse{TranslatedText: "ok"})
	}))
	defer srv.Close()

	eng, err := NewLibreTranslate(srv.URL, "s3cret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Translate(context.Background(), "salom dunyo", "uz", "en"); err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "s3cret" {
		t.Errorf("api_key = %q", got.APIKey)
	}
}

// The endpoint in config is a base URL, because that is how people write one.
func TestEndpointGetsTheTranslatePath(t *testing.T) {
	for _, given := range []string{
		"http://127.0.0.1:5000",
		"http://127.0.0.1:5000/",
		"http://127.0.0.1:5000/translate",
		"http://127.0.0.1:5000/translate/",
	} {
		eng, err := NewLibreTranslate(given, "", time.Second)
		if err != nil {
			t.Fatalf("%s: %v", given, err)
		}
		if eng.Endpoint() != "http://127.0.0.1:5000/translate" {
			t.Errorf("%s -> %s", given, eng.Endpoint())
		}
	}
}

func TestEndpointIsValidated(t *testing.T) {
	for _, bad := range []string{"", "   ", "127.0.0.1:5000", "ftp://host/translate", "::::"} {
		if _, err := NewLibreTranslate(bad, "", time.Second); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestEngineReportsAServiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(translateResponse{Error: "unsupported language pair de->en"})
	}))
	defer srv.Close()

	_, err := newEngine(t, srv).Translate(context.Background(), "guten tag freunde", "uz", "en")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "unsupported language pair") {
		t.Errorf("err = %v", err)
	}
}

// A failing service tends to echo its input, so a non-JSON failure body is
// never quoted: that body is the SMS.
func TestEngineDoesNotQuoteAFailureBody(t *testing.T) {
	const body = "upstream error while handling Kod podtverjdeniya: 100001"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, body)
	}))
	defer srv.Close()

	_, err := newEngine(t, srv).Translate(context.Background(), "x", "uz", "en")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "100001") {
		t.Errorf("echoed the body: %v", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("lost the status: %v", err)
	}
}

func TestEngineRejectsAnEmptyTranslation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(translateResponse{TranslatedText: ""})
	}))
	defer srv.Close()

	if _, err := newEngine(t, srv).Translate(context.Background(), "x", "uz", "en"); err == nil {
		t.Fatal("want an error for an answer with no translation in it")
	}
}

// A wrong endpoint -- someone's web UI on the same host -- must not stream a
// page into the daemon.
func TestEngineRefusesAHugeResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", 128<<10))
	}))
	defer srv.Close()

	_, err := newEngine(t, srv).Translate(context.Background(), "x", "uz", "en")
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineRespectsCancellation(t *testing.T) {
	// The handler is released by the test rather than by the request context:
	// a client-side timeout does not necessarily cancel the server's context,
	// and httptest.Server.Close waits for outstanding handlers.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := newEngine(t, srv).Translate(ctx, "x", "uz", "en"); err == nil {
		t.Fatal("want an error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s", elapsed)
	}
}

// The gateway reaches Telegram through a proxy and the host carries a split
// tunnel. Sending message bodies through either on the way to a machine on the
// same subnet would be both broken and a disclosure.
func TestEngineIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(translateResponse{TranslatedText: "reached"})
	}))
	defer srv.Close()

	out, err := newEngine(t, srv).Translate(context.Background(), "salom", "uz", "en")
	if err != nil {
		t.Fatalf("a proxy in the environment was consulted: %v", err)
	}
	if out != "reached" {
		t.Errorf("out = %q", out)
	}
}

func TestEngineName(t *testing.T) {
	eng, err := NewLibreTranslate("http://127.0.0.1:5000", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if eng.Name() != EngineLibreTranslate {
		t.Errorf("name = %q", eng.Name())
	}
}
