package smspdu

import (
	"fmt"
	"strings"
)

const (
	tonUnknown       = 0
	tonInternational = 1
	tonNational      = 2
	tonAlphanumeric  = 5
)

func parseAddress(data []byte) (addr string, used int, err error) {
	if len(data) < 2 {
		return "", 0, fmt.Errorf("address too short")
	}
	length := int(data[0])
	toa := data[1]
	ton := (toa >> 4) & 0x07

	switch ton {
	case tonAlphanumeric:
		octets := (length + 1) / 2
		if len(data) < 2+octets {
			return "", 0, fmt.Errorf("alphanumeric address truncated")
		}
		raw := data[2 : 2+octets]
		addr = strings.TrimRight(unpackGSM7All(raw), "\x00 ")
		return addr, 2 + octets, nil
	case tonInternational, tonNational, tonUnknown:
		octets := (length + 1) / 2
		if len(data) < 2+octets {
			return "", 0, fmt.Errorf("numeric address truncated")
		}
		digits := semiOctetDigits(data[2:2+octets], length)
		if digits == "" {
			return "unknown", 2 + octets, nil
		}
		if ton == tonInternational || strings.HasPrefix(digits, "+") {
			if !strings.HasPrefix(digits, "+") {
				digits = "+" + digits
			}
			return digits, 2 + octets, nil
		}
		return digits, 2 + octets, nil
	default:
		octets := (length + 1) / 2
		if len(data) < 2+octets {
			return "", 0, fmt.Errorf("address truncated")
		}
		return fmt.Sprintf("addr:%x", data[2:2+octets]), 2 + octets, nil
	}
}

func semiOctetDigits(data []byte, length int) string {
	var b strings.Builder
	for i := 0; i < length; i++ {
		byteIndex := i / 2
		if byteIndex >= len(data) {
			break
		}
		var d byte
		if i%2 == 0 {
			d = data[byteIndex] & 0x0F
		} else {
			d = data[byteIndex] >> 4
		}
		if d == 0x0F {
			break
		}
		if d > 9 {
			continue
		}
		b.WriteByte('0' + d)
	}
	return b.String()
}
