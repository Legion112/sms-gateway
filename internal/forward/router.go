package forward

import (
	"strings"

	"github.com/legion/sms-gateway/internal/config"
)

// Router selects forward channels for an inbound SMS using config rules.
type Router struct {
	rules []config.ForwardRule
}

// NewRouter builds a router from forward rules (first match wins).
func NewRouter(rules []config.ForwardRule) *Router {
	return &Router{rules: rules}
}

// Resolve returns the first matching rule name and channel names, if any.
func (r *Router) Resolve(msg InboundSMS) (ruleName string, channels []string, ok bool) {
	for _, rule := range r.rules {
		if !matchModem(rule.Modem, msg.Modem) {
			continue
		}
		if !matchFrom(rule.From, msg.From) {
			continue
		}
		return rule.Name, append([]string(nil), rule.To...), true
	}
	return "", nil, false
}

func matchModem(ruleModem, msgModem string) bool {
	if ruleModem == "" {
		return true
	}
	return ruleModem == msgModem
}

func matchFrom(pattern, from string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(from, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == from
}
