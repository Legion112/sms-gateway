package forward_test

import (
	"testing"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
)

func TestRouterResolve(t *testing.T) {
	rules := []config.ForwardRule{
		{Name: "bank", Modem: "sim1", From: "+79001234567", To: []string{"tg"}},
		{Name: "uk", Modem: "sim2", From: "+44*", To: []string{"tg", "sms"}},
		{Name: "default", Modem: "sim1", From: "*", To: []string{"tg"}},
	}
	r := forward.NewRouter(rules)

	tests := []struct {
		msg      forward.InboundSMS
		wantRule string
		wantCh   []string
		wantOK   bool
	}{
		{
			msg:      forward.InboundSMS{Modem: "sim1", From: "+79001234567"},
			wantRule: "bank", wantCh: []string{"tg"}, wantOK: true,
		},
		{
			msg:      forward.InboundSMS{Modem: "sim2", From: "+44123456789"},
			wantRule: "uk", wantCh: []string{"tg", "sms"}, wantOK: true,
		},
		{
			msg:      forward.InboundSMS{Modem: "sim1", From: "+79999999999"},
			wantRule: "default", wantCh: []string{"tg"}, wantOK: true,
		},
		{
			msg:    forward.InboundSMS{Modem: "sim9", From: "+79999999999"},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		rule, ch, ok := r.Resolve(tc.msg)
		if ok != tc.wantOK {
			t.Fatalf("msg %+v: ok=%v want %v", tc.msg, ok, tc.wantOK)
		}
		if !ok {
			continue
		}
		if rule != tc.wantRule {
			t.Fatalf("msg %+v: rule=%q want %q", tc.msg, rule, tc.wantRule)
		}
		if len(ch) != len(tc.wantCh) {
			t.Fatalf("msg %+v: channels=%v want %v", tc.msg, ch, tc.wantCh)
		}
		for i := range ch {
			if ch[i] != tc.wantCh[i] {
				t.Fatalf("msg %+v: channels=%v want %v", tc.msg, ch, tc.wantCh)
			}
		}
	}
}

func TestRouterAnyModem(t *testing.T) {
	r := forward.NewRouter([]config.ForwardRule{
		{Name: "any", From: "*", To: []string{"tg"}},
	})
	_, ch, ok := r.Resolve(forward.InboundSMS{Modem: "anything", From: "+1"})
	if !ok || len(ch) != 1 {
		t.Fatalf("expected match for empty rule modem, got ok=%v ch=%v", ok, ch)
	}
}
