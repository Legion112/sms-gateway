package cmdutil

import (
	"testing"

	"github.com/legion/sms-gateway/internal/config"
)

func TestResolveLineFromSimNumbers(t *testing.T) {
	cfg := config.Config{
		SimNumbers: map[string]string{
			"8999801234567890123": "+998901234567",
		},
	}
	line := ResolveLine(cfg, "8999801234567890123F")
	if line.Number != "+998901234567" || line.Source != LineSourceSimNumbers {
		t.Fatalf("got %+v", line)
	}
}

func TestResolveLineUnmapped(t *testing.T) {
	line := ResolveLine(config.Config{}, "8999809999999999999")
	if line.Source != LineSourceUnmapped || line.ICCID != "8999809999999999999" {
		t.Fatalf("got %+v", line)
	}
}

func TestResolveLineNoICCID(t *testing.T) {
	line := ResolveLine(config.Config{}, "")
	if line.Source != LineSourceNoICCID {
		t.Fatalf("got %+v", line)
	}
}

func TestFormatPhoneStatus(t *testing.T) {
	if got := FormatPhoneStatus(ResolvedLine{Number: "+998901234567", Source: LineSourceSimNumbers}); got != "+998901234567 (sim_numbers)" {
		t.Fatalf("got %q", got)
	}
	if got := FormatPhoneStatus(ResolvedLine{ICCID: "8999801234567890123", Source: LineSourceUnmapped}); got != "unknown (iccid 8999801234567890123 not in sim_numbers)" {
		t.Fatalf("got %q", got)
	}
}
