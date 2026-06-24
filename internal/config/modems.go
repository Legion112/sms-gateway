package config

import (
	"fmt"
	"sort"
)

// ResolveModemTargets returns modem names to operate on.
// An empty name means legacy top-level config (single modem).
func (cfg Config) ResolveModemTargets(modemFlag string) ([]string, error) {
	if modemFlag != "" {
		if _, ok := cfg.Modems[modemFlag]; !ok {
			return nil, fmt.Errorf("modem %q not found in modems", modemFlag)
		}
		return []string{modemFlag}, nil
	}
	if len(cfg.Modems) == 0 {
		return []string{""}, nil
	}
	names := make([]string, 0, len(cfg.Modems))
	for name := range cfg.Modems {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// SendModemTarget picks the modem name for outbound send.
func (cfg Config) SendModemTarget(modemFlag string) (string, error) {
	if len(cfg.Modems) == 0 {
		if modemFlag != "" {
			return "", fmt.Errorf("modem %q not found (no modems configured)", modemFlag)
		}
		return "", nil
	}
	if modemFlag != "" {
		if _, ok := cfg.Modems[modemFlag]; !ok {
			return "", fmt.Errorf("modem %q not found in modems", modemFlag)
		}
		return modemFlag, nil
	}
	if len(cfg.Modems) == 1 {
		for name := range cfg.Modems {
			return name, nil
		}
	}
	if cfg.DefaultModem != "" {
		if _, ok := cfg.Modems[cfg.DefaultModem]; !ok {
			return "", fmt.Errorf("default_modem %q not found in modems", cfg.DefaultModem)
		}
		return cfg.DefaultModem, nil
	}
	return "", fmt.Errorf("--modem is required when multiple modems are configured")
}
