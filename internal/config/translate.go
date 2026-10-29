package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// TranslateConfig holds settings for translating inbound SMS before they are
// forwarded.
type TranslateConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Engine    string        `yaml:"engine"`
	Endpoint  string        `yaml:"endpoint"`
	APIKey    string        `yaml:"api_key"`
	Target    string        `yaml:"target"`
	Sources   []string      `yaml:"sources"`
	Timeout   time.Duration `yaml:"-"`
	CacheSize int           `yaml:"cache_size"`
}

type translateFile struct {
	Translate struct {
		Enabled   bool     `yaml:"enabled"`
		Engine    string   `yaml:"engine"`
		Endpoint  string   `yaml:"endpoint"`
		APIKey    string   `yaml:"api_key"`
		Target    string   `yaml:"target"`
		Sources   []string `yaml:"sources"`
		Timeout   string   `yaml:"timeout"`
		CacheSize int      `yaml:"cache_size"`
	} `yaml:"translate"`
}

const (
	envTranslateEndpoint = "SMS_GATEWAY_TRANSLATE_ENDPOINT"
	envTranslateAPIKey   = "SMS_GATEWAY_TRANSLATE_API_KEY"
)

func applyTranslateFile(file *translateFile, cfg *Config) error {
	t := &file.Translate
	cfg.Translate.Enabled = t.Enabled
	if t.Engine != "" {
		cfg.Translate.Engine = t.Engine
	}
	if t.Endpoint != "" {
		cfg.Translate.Endpoint = t.Endpoint
	}
	if t.APIKey != "" {
		cfg.Translate.APIKey = t.APIKey
	}
	if t.Target != "" {
		cfg.Translate.Target = t.Target
	}
	if len(t.Sources) > 0 {
		cfg.Translate.Sources = append([]string(nil), t.Sources...)
	}
	if t.CacheSize != 0 {
		cfg.Translate.CacheSize = t.CacheSize
	}
	if t.Timeout != "" {
		parsed, err := time.ParseDuration(t.Timeout)
		if err != nil {
			return fmt.Errorf("translate.timeout: %w", err)
		}
		if parsed < 0 {
			return fmt.Errorf("translate.timeout: must not be negative")
		}
		cfg.Translate.Timeout = parsed
	}
	// The endpoint and key are also readable from the environment so that a
	// systemd unit can point the daemon at a translation service without the
	// address being committed to a config file.
	if v := os.Getenv(envTranslateEndpoint); v != "" {
		cfg.Translate.Endpoint = v
	}
	if v := os.Getenv(envTranslateAPIKey); v != "" {
		cfg.Translate.APIKey = v
	}
	return nil
}

// TranslateEngine returns the configured engine driver, defaulting to the
// LibreTranslate HTTP API.
func (cfg Config) TranslateEngine() string {
	if e := strings.TrimSpace(cfg.Translate.Engine); e != "" {
		return e
	}
	return "libretranslate"
}

// TranslateTarget returns the language to translate into, defaulting to
// English.
func (cfg Config) TranslateTarget() string {
	if t := strings.TrimSpace(cfg.Translate.Target); t != "" {
		return strings.ToLower(t)
	}
	return "en"
}

// TranslateSources returns the languages worth translating -- the ones the
// recipient cannot read -- defaulting to Uzbek alone.
//
// It is a list rather than a switch because the decision is per language: these
// SIMs receive Russian and English as well, and translating those would add a
// paragraph of noise to every message the recipient could already read.
func (cfg Config) TranslateSources() []string {
	out := make([]string, 0, len(cfg.Translate.Sources))
	for _, s := range cfg.Translate.Sources {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"uz"}
	}
	return out
}

// TranslateTimeout bounds one translation, with default 10s.
//
// It is short because it is spent on the delivery path: the message is already
// received and stored, and every second here is a second the recipient does not
// have it. A translation that misses this budget is dropped, not waited for.
func (cfg Config) TranslateTimeout() time.Duration {
	if cfg.Translate.Timeout > 0 {
		return cfg.Translate.Timeout
	}
	return 10 * time.Second
}

// TranslateCacheSize is how many recent translations to remember, with default
// 256. Operator broadcasts repeat verbatim and both modems receive them, so the
// cache spares the engine most of its work.
func (cfg Config) TranslateCacheSize() int {
	if cfg.Translate.CacheSize > 0 {
		return cfg.Translate.CacheSize
	}
	return 256
}

// ValidateTranslate checks translation settings. It only has anything to say
// when the feature is switched on: a gateway that does not translate must not
// be refused a start over a section it never uses.
func ValidateTranslate(cfg Config) error {
	if !cfg.Translate.Enabled {
		return nil
	}
	if engine := cfg.TranslateEngine(); engine != "libretranslate" {
		return fmt.Errorf("translate: unknown engine %q", engine)
	}
	if strings.TrimSpace(cfg.Translate.Endpoint) == "" {
		return fmt.Errorf("translate: endpoint is required when translation is enabled (config or %s)", envTranslateEndpoint)
	}
	target := cfg.TranslateTarget()
	for _, src := range cfg.TranslateSources() {
		if src == target {
			return fmt.Errorf("translate: %q is both a source and the target", src)
		}
	}
	return nil
}
