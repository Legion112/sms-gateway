package cmdutil

import (
	"github.com/legion/sms-gateway/internal/config"
)

// LineSource identifies how the subscriber line number was resolved.
type LineSource string

const (
	LineSourceSimNumbers LineSource = "sim_numbers"
	LineSourceUnmapped   LineSource = "unmapped"
	LineSourceNoICCID    LineSource = "no_iccid"
)

// ResolvedLine is the SIM-centric line number for a modem's inserted SIM.
type ResolvedLine struct {
	Number string
	Source LineSource
	ICCID  string
}

// ResolveLine returns the MSISDN for the inserted SIM via sim_numbers only.
func ResolveLine(cfg config.Config, iccid string) ResolvedLine {
	normalized := config.NormalizeICCID(iccid)
	if normalized == "" {
		return ResolvedLine{Source: LineSourceNoICCID}
	}
	if number := cfg.LookupSimNumber(normalized); number != "" {
		return ResolvedLine{
			Number: number,
			Source: LineSourceSimNumbers,
			ICCID:  normalized,
		}
	}
	return ResolvedLine{
		Source: LineSourceUnmapped,
		ICCID:  normalized,
	}
}

// FormatPhoneStatus formats phone line for CLI status output.
func FormatPhoneStatus(line ResolvedLine) string {
	switch line.Source {
	case LineSourceSimNumbers:
		return line.Number + " (sim_numbers)"
	case LineSourceUnmapped:
		return "unknown (iccid " + line.ICCID + " not in sim_numbers)"
	default:
		return "unknown"
	}
}
