package smspdu

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

type udhInfo struct {
	ConcatRef int
	Part      int
	PartTotal int
	HeaderLen int
}

func parseUDH(data []byte) (udhInfo, error) {
	info := udhInfo{}
	if len(data) == 0 {
		return info, nil
	}
	udhl := int(data[0])
	info.HeaderLen = 1 + udhl
	if len(data) < info.HeaderLen {
		return info, fmt.Errorf("udh truncated")
	}
	i := 1
	end := 1 + udhl
	for i < end {
		if i+1 >= end {
			break
		}
		iei := data[i]
		iedl := int(data[i+1])
		i += 2
		if i+iedl > end {
			break
		}
		switch iei {
		case 0x00:
			if iedl >= 3 {
				info.ConcatRef = int(data[i])
				info.PartTotal = int(data[i+1])
				info.Part = int(data[i+2])
			}
		case 0x08:
			if iedl >= 4 {
				info.ConcatRef = int(data[i])<<8 | int(data[i+1])
				info.PartTotal = int(data[i+2])
				info.Part = int(data[i+3])
			}
		}
		i += iedl
	}
	return info, nil
}

func udhSeptetLength(udhl int) int {
	if udhl <= 0 {
		return 0
	}
	headerBits := (1 + udhl) * 8
	return (headerBits + 6) / 7
}

func decodeUserData(dcs byte, ud []byte, udhi bool, udl int) (text string, udh udhInfo, err error) {
	if len(ud) == 0 || udl == 0 {
		return "", udh, nil
	}

	if udhi {
		udh, err = parseUDH(ud)
		if err != nil {
			return "", udh, err
		}
	}

	switch {
	case isUCS2(dcs):
		payload := ud
		if udhi {
			payload = ud[udh.HeaderLen:]
		}
		text, err = decodeUCS2(payload)
	case isGSM7(dcs):
		if udhi {
			text = unpackGSM7FromSeptet(ud, udhSeptetLength(udh.HeaderLen-1), udl-udhSeptetLength(udh.HeaderLen-1))
		} else {
			text = unpackGSM7(ud, udl)
		}
	default:
		payload := ud
		if udhi {
			payload = ud[udh.HeaderLen:]
		}
		text = decode8Bit(payload)
	}
	return trimText(text), udh, err
}

func isUCS2(dcs byte) bool {
	switch {
	case dcs == 0x08:
		return true
	case dcs&0xEC == 0x08:
		return true
	case dcs&0xF4 == 0xF4:
		return true
	default:
		return false
	}
}

func isGSM7(dcs byte) bool {
	if isUCS2(dcs) {
		return false
	}
	switch {
	case dcs == 0x00:
		return true
	case dcs == 0xF0:
		return true
	case dcs&0xEC == 0x00:
		return true
	case dcs&0xE0 == 0xC0:
		return true
	default:
		return false
	}
}

func decodeUCS2(payload []byte) (string, error) {
	if len(payload) < 2 || len(payload)%2 != 0 {
		return string(payload), nil
	}
	u16 := make([]uint16, len(payload)/2)
	for i := range u16 {
		u16[i] = uint16(payload[2*i])<<8 | uint16(payload[2*i+1])
	}
	return string(utf16.Decode(u16)), nil
}

func decode8Bit(payload []byte) string {
	return strings.TrimRight(string(payload), "\x00 ")
}

func trimText(s string) string {
	return strings.TrimRight(s, "\x00 \r\n")
}
