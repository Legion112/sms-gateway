package smspdu

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Deliver is a decoded mobile-terminated SMS TPDU.
type Deliver struct {
	From      string
	Timestamp string
	Text      string
	ConcatRef int
	Part      int
	PartTotal int
}

// ParseHexDeliver decodes a hex-encoded SMS PDU (SMSC + TPDU).
func ParseHexDeliver(hexPDU string) (Deliver, error) {
	hexPDU = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, hexPDU)
	pdu, err := hex.DecodeString(hexPDU)
	if err != nil {
		return Deliver{}, fmt.Errorf("hex decode: %w", err)
	}
	return ParseDeliver(pdu)
}

// ParseDeliver decodes SMS-DELIVER TPDU bytes including leading SMSC.
func ParseDeliver(pdu []byte) (Deliver, error) {
	if len(pdu) < 2 {
		return Deliver{}, fmt.Errorf("pdu too short")
	}
	smscLen := int(pdu[0])
	if len(pdu) < 1+smscLen+1 {
		return Deliver{}, fmt.Errorf("smsc truncated")
	}
	tpdu := pdu[1+smscLen:]
	return parseTPDU(tpdu)
}

func parseTPDU(tpdu []byte) (Deliver, error) {
	if len(tpdu) < 4 {
		return Deliver{}, fmt.Errorf("tpdu too short")
	}
	fo := tpdu[0]
	mti := fo & 0x03
	if mti != 0x00 {
		return Deliver{}, fmt.Errorf("unsupported mti %d", mti)
	}
	udhi := fo&0x40 != 0

	addr, addrUsed, err := parseAddress(tpdu[1:])
	if err != nil {
		return Deliver{}, fmt.Errorf("oa: %w", err)
	}
	off := 1 + addrUsed
	if len(tpdu) < off+8 {
		return Deliver{}, fmt.Errorf("tpdu missing fields")
	}
	// pid := tpdu[off]
	dcs := tpdu[off+1]
	scts := tpdu[off+2 : off+9]
	udl := int(tpdu[off+9])
	off += 10
	udBytes := udOctetLength(dcs, udl)
	if len(tpdu) < off+udBytes {
		return Deliver{}, fmt.Errorf("user data truncated")
	}
	ud := tpdu[off : off+udBytes]

	text, udh, err := decodeUserData(dcs, ud, udhi, udl)
	if err != nil {
		return Deliver{}, fmt.Errorf("ud: %w", err)
	}

	return Deliver{
		From:      addr,
		Timestamp: formatSCTS(scts),
		Text:      text,
		ConcatRef: udh.ConcatRef,
		Part:      udh.Part,
		PartTotal: udh.PartTotal,
	}, nil
}

func formatSCTS(scts []byte) string {
	if len(scts) != 7 {
		return ""
	}
	decodeDigit := func(b byte) int {
		lo := int(b & 0x0F)
		hi := int(b >> 4)
		if lo > 9 || hi > 9 {
			return -1
		}
		return lo*10 + hi
	}
	year := decodeDigit(scts[0])
	month := decodeDigit(scts[1])
	day := decodeDigit(scts[2])
	hour := decodeDigit(scts[3])
	min := decodeDigit(scts[4])
	sec := decodeDigit(scts[5])
	tz := decodeDigit(scts[6])
	if year < 0 || month < 0 {
		return fmt.Sprintf("%02X/%02X/%02X,%02X:%02X:%02X+%02X", scts[0], scts[1], scts[2], scts[3], scts[4], scts[5], scts[6])
	}
	year += 2000
	if year > 2099 {
		year -= 100
	}
	_ = tz
	t := time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC)
	return t.Format("06/01/02,15:04:05-07")
}

func udOctetLength(dcs byte, udl int) int {
	if udl <= 0 {
		return 0
	}
	if isGSM7(dcs) {
		return gsm7OctetLength(udl)
	}
	return udl
}
