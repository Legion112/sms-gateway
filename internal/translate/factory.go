package translate

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/config"
)

// FromConfig builds the configured translator, or nil when translation is
// switched off. A nil Translator is fully usable: every method on it reports
// that there is nothing to add.
//
// It does not reach the engine. A gateway must start and receive SMS whether or
// not the translation service happens to be up, so the first contact is made by
// the first message that needs translating, and a service that is down shows up
// as one journal line rather than as a daemon that will not start.
func FromConfig(cfg config.Config, logf func(format string, args ...any)) (*Translator, error) {
	if !cfg.Translate.Enabled {
		return nil, nil
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		return nil, err
	}
	return New(engine, Options{
		Sources:   cfg.TranslateSources(),
		Target:    cfg.TranslateTarget(),
		Timeout:   cfg.TranslateTimeout(),
		CacheSize: cfg.TranslateCacheSize(),
		Log:       logf,
	}), nil
}

// NewEngine builds the configured engine driver.
func NewEngine(cfg config.Config) (Engine, error) {
	switch name := cfg.TranslateEngine(); name {
	case EngineLibreTranslate:
		return NewLibreTranslate(cfg.Translate.Endpoint, cfg.Translate.APIKey, cfg.TranslateTimeout())
	default:
		return nil, fmt.Errorf("translate: unknown engine %q", name)
	}
}
