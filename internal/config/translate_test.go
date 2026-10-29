package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/config"
)

func loadTranslateConfig(t *testing.T, body string) config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Translation off is the default, and a config with no translate section at
// all has to keep working: the gateway's job does not depend on it.
func TestTranslateIsOffByDefault(t *testing.T) {
	cfg := loadTranslateConfig(t, "driver: serial\n")
	if cfg.Translate.Enabled {
		t.Error("translation was on without being asked for")
	}
	if err := config.ValidateTranslate(cfg); err != nil {
		t.Errorf("a gateway that does not translate was refused: %v", err)
	}
}

func TestTranslateLoads(t *testing.T) {
	cfg := loadTranslateConfig(t, `
driver: serial
translate:
  enabled: true
  endpoint: http://192.0.2.10:5000
  api_key: shared-secret
  target: ru
  sources: [uz, en]
  timeout: 25s
  cache_size: 64
`)
	if !cfg.Translate.Enabled {
		t.Fatal("not enabled")
	}
	if cfg.Translate.Endpoint != "http://192.0.2.10:5000" {
		t.Errorf("endpoint = %q", cfg.Translate.Endpoint)
	}
	if cfg.Translate.APIKey != "shared-secret" {
		t.Errorf("api_key = %q", cfg.Translate.APIKey)
	}
	if cfg.TranslateTarget() != "ru" {
		t.Errorf("target = %q", cfg.TranslateTarget())
	}
	if got := cfg.TranslateSources(); len(got) != 2 || got[0] != "uz" || got[1] != "en" {
		t.Errorf("sources = %v", got)
	}
	if cfg.TranslateTimeout() != 25*time.Second {
		t.Errorf("timeout = %s", cfg.TranslateTimeout())
	}
	if cfg.TranslateCacheSize() != 64 {
		t.Errorf("cache_size = %d", cfg.TranslateCacheSize())
	}
}

func TestTranslateDefaults(t *testing.T) {
	cfg := loadTranslateConfig(t, "driver: serial\ntranslate:\n  enabled: true\n  endpoint: http://127.0.0.1:5000\n")
	if cfg.TranslateEngine() != "libretranslate" {
		t.Errorf("engine = %q", cfg.TranslateEngine())
	}
	if cfg.TranslateTarget() != "en" {
		t.Errorf("target = %q", cfg.TranslateTarget())
	}
	// Uzbek alone, because that is the language the recipient cannot read.
	if got := cfg.TranslateSources(); len(got) != 1 || got[0] != "uz" {
		t.Errorf("sources = %v", got)
	}
	if cfg.TranslateTimeout() != 10*time.Second {
		t.Errorf("timeout = %s", cfg.TranslateTimeout())
	}
	if cfg.TranslateCacheSize() != 256 {
		t.Errorf("cache_size = %d", cfg.TranslateCacheSize())
	}
}

func TestTranslateSourcesAreNormalised(t *testing.T) {
	cfg := loadTranslateConfig(t, "driver: serial\ntranslate:\n  enabled: true\n  endpoint: http://x:1\n  target: EN\n  sources: [\" UZ \", \"\", \"Ru\"]\n")
	if cfg.TranslateTarget() != "en" {
		t.Errorf("target = %q", cfg.TranslateTarget())
	}
	if got := cfg.TranslateSources(); len(got) != 2 || got[0] != "uz" || got[1] != "ru" {
		t.Errorf("sources = %v", got)
	}
}

// A systemd unit can point the daemon at a translation service without the
// address or key being committed to a file.
func TestTranslateEndpointFromEnvironment(t *testing.T) {
	t.Setenv("SMS_GATEWAY_TRANSLATE_ENDPOINT", "http://10.0.0.9:5000")
	t.Setenv("SMS_GATEWAY_TRANSLATE_API_KEY", "from-env")
	cfg := loadTranslateConfig(t, "driver: serial\ntranslate:\n  enabled: true\n  endpoint: http://127.0.0.1:5000\n  api_key: from-file\n")
	if cfg.Translate.Endpoint != "http://10.0.0.9:5000" {
		t.Errorf("endpoint = %q", cfg.Translate.Endpoint)
	}
	if cfg.Translate.APIKey != "from-env" {
		t.Errorf("api_key = %q", cfg.Translate.APIKey)
	}
}

func TestTranslateRejectsAMissingEndpoint(t *testing.T) {
	cfg := config.Config{Translate: config.TranslateConfig{Enabled: true}}
	err := config.ValidateTranslate(cfg)
	if err == nil {
		t.Fatal("accepted translation with nowhere to send it")
	}
	if !strings.Contains(err.Error(), "endpoint is required") {
		t.Errorf("err = %v", err)
	}
}

func TestTranslateRejectsAnUnknownEngine(t *testing.T) {
	cfg := config.Config{Translate: config.TranslateConfig{
		Enabled:  true,
		Engine:   "google",
		Endpoint: "http://127.0.0.1:5000",
	}}
	if err := config.ValidateTranslate(cfg); err == nil {
		t.Fatal("accepted an engine that does not exist")
	}
}

// Translating a language into itself is a configuration mistake that would
// otherwise show up as the engine being asked nonsense once per message.
func TestTranslateRejectsATargetThatIsAlsoASource(t *testing.T) {
	cfg := config.Config{Translate: config.TranslateConfig{
		Enabled:  true,
		Endpoint: "http://127.0.0.1:5000",
		Target:   "en",
		Sources:  []string{"uz", "en"},
	}}
	if err := config.ValidateTranslate(cfg); err == nil {
		t.Fatal("accepted en as both source and target")
	}
}

func TestTranslateRejectsABadTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("translate:\n  enabled: true\n  timeout: soon\n"), 0o600)
	if _, err := config.Load(config.Overrides{ConfigPath: path}); err == nil {
		t.Fatal("accepted a timeout that is not a duration")
	}
}

// ValidateWatch is the gate the daemon actually passes through, so the
// translate checks have to be reachable from it.
func TestValidateWatchChecksTranslate(t *testing.T) {
	cfg := config.Config{
		Modems:       map[string]config.ModemEntry{"m1": {Driver: config.DriverMM}},
		Channels:     map[string]config.ChannelConfig{"tg": {Driver: "telegram_bot", Telegram: config.TelegramChannelConfig{ChatID: 1, BotToken: "x"}}},
		ForwardRules: []config.ForwardRule{{Name: "all", To: []string{"tg"}}},
		Storage:      config.StorageConfig{Path: "./data/sms.db"},
		Translate:    config.TranslateConfig{Enabled: true},
	}
	if err := config.ValidateWatch(cfg); err == nil {
		t.Fatal("the daemon would have started with translation enabled and no endpoint")
	}
}

// The shipped example has the section filled in but switched off. An example
// that arrived enabled would point every new install at an endpoint that is not
// there and log an outage per message.
func TestExampleConfigShipsTranslationOff(t *testing.T) {
	for _, env := range []string{
		"SMS_GATEWAY_CHANNEL_MY_TELEGRAM_BOT_TOKEN",
		"SMS_GATEWAY_CHANNEL_VIKA_TELEGRAM_BOT_TOKEN",
	} {
		t.Setenv(env, "123456:test-token")
	}
	cfg, err := config.Load(config.Overrides{ConfigPath: filepath.Join("..", "..", "config.example.yaml")})
	if err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
	if cfg.Translate.Enabled {
		t.Error("config.example.yaml ships with translation enabled")
	}
	if cfg.Translate.Endpoint == "" {
		t.Error("the example has no endpoint to copy")
	}
	if got := cfg.TranslateSources(); len(got) != 1 || got[0] != "uz" {
		t.Errorf("example sources = %v", got)
	}
}
