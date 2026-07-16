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

func TestRouterToLine(t *testing.T) {
	rules := []config.ForwardRule{
		{Name: "line-a", ToLine: "+15550001001", From: "*", To: []string{"my-telegram"}},
		{Name: "line-b", ToLine: "+15550001002", From: "*", To: []string{"vika-telegram"}},
		{Name: "default", ToLine: "*", From: "*", To: []string{"my-telegram"}},
	}
	r := forward.NewRouter(rules)

	tests := []struct {
		name     string
		msg      forward.InboundSMS
		wantRule string
		wantCh   []string
	}{
		{
			name:     "mapped line a",
			msg:      forward.InboundSMS{Modem: "m1", From: "+15559999999", ToLine: "+15550001001"},
			wantRule: "line-a",
			wantCh:   []string{"my-telegram"},
		},
		{
			name:     "mapped line b",
			msg:      forward.InboundSMS{Modem: "m2", From: "+15559999999", ToLine: "+15550001002"},
			wantRule: "line-b",
			wantCh:   []string{"vika-telegram"},
		},
		{
			name:     "unmapped empty ToLine catch-all",
			msg:      forward.InboundSMS{Modem: "m1", From: "+15559999999", ToLine: ""},
			wantRule: "default",
			wantCh:   []string{"my-telegram"},
		},
		{
			name:     "other mapped line catch-all",
			msg:      forward.InboundSMS{Modem: "m1", From: "+15559999999", ToLine: "+15550001999"},
			wantRule: "default",
			wantCh:   []string{"my-telegram"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rule, ch, ok := r.Resolve(tc.msg)
			if !ok {
				t.Fatalf("expected match")
			}
			if rule != tc.wantRule {
				t.Fatalf("rule=%q want %q", rule, tc.wantRule)
			}
			if len(ch) != len(tc.wantCh) {
				t.Fatalf("channels=%v want %v", ch, tc.wantCh)
			}
			for i := range ch {
				if ch[i] != tc.wantCh[i] {
					t.Fatalf("channels=%v want %v", ch, tc.wantCh)
				}
			}
		})
	}
}

func TestRouterToLineWrongLineNoMatchBeforeCatchAll(t *testing.T) {
	r := forward.NewRouter([]config.ForwardRule{
		{Name: "line-a", ToLine: "+15550001001", From: "*", To: []string{"my-telegram"}},
	})
	_, _, ok := r.Resolve(forward.InboundSMS{Modem: "m1", From: "+15559999999", ToLine: "+15550001002"})
	if ok {
		t.Fatal("specific to_line must not match wrong recipient")
	}
}

func TestRouterToLinePrefix(t *testing.T) {
	r := forward.NewRouter([]config.ForwardRule{
		{Name: "prefix", ToLine: "+1555000*", From: "*", To: []string{"tg"}},
	})
	rule, ch, ok := r.Resolve(forward.InboundSMS{ToLine: "+15550001001", From: "+1"})
	if !ok || rule != "prefix" || len(ch) != 1 {
		t.Fatalf("ok=%v rule=%q ch=%v", ok, rule, ch)
	}
}
