package smsconcat

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/legion/sms-gateway/internal/modem"
)

// Part is one SMS segment, optionally part of a concatenated message.
type Part = modem.Part

// GroupKey identifies a concatenated SMS group.
type GroupKey struct {
	From      string
	ConcatRef int
	PartTotal int
}

// GroupKeyFromPart returns the concat group key for a part.
func GroupKeyFromPart(p Part) GroupKey {
	return GroupKey{From: p.From, ConcatRef: p.ConcatRef, PartTotal: p.PartTotal}
}

// IsConcat reports whether part is a segment of a multi-part SMS.
func IsConcat(p Part) bool {
	return p.PartTotal >= 2 && p.Part >= 1
}

// PartToMessage converts a non-concat part to modem.Message.
func PartToMessage(p Part) modem.Message {
	return partToMessage(p)
}

// MergeGroup merges an ordered set of parts from the same concat group.
func MergeGroup(segs []Part) modem.Message {
	if len(segs) == 0 {
		return modem.Message{}
	}
	msgs := Merge(segs)
	if len(msgs) == 1 {
		return msgs[0]
	}
	var b strings.Builder
	for _, p := range segs {
		b.WriteString(p.Text)
	}
	first := segs[0]
	return modem.Message{
		ID:        lowestID(segs),
		From:      first.From,
		State:     first.State,
		Timestamp: first.Timestamp,
		Text:      trimMergedText(b.String()),
		Storage:   first.Storage,
		SMSC:      first.SMSC,
	}
}

// MergeIncompleteGroup merges available parts and prefixes when the group timed out.
func MergeIncompleteGroup(segs []Part) modem.Message {
	msg := MergeGroup(segs)
	if len(segs) == 0 {
		return msg
	}
	total := segs[0].PartTotal
	if total < 2 {
		return msg
	}
	msg.Text = fmt.Sprintf("[incomplete %d/%d]\n%s", len(segs), total, msg.Text)
	return msg
}

// Merge joins concatenated segments into single modem.Message values.
func Merge(parts []Part) []modem.Message {
	if len(parts) == 0 {
		return nil
	}

	type groupKey struct {
		from      string
		concatRef int
		partTotal int
	}

	groups := make(map[groupKey][]Part)
	var singles []Part

	for _, p := range parts {
		if p.PartTotal >= 2 && p.Part >= 1 {
			key := groupKey{from: p.From, concatRef: p.ConcatRef, partTotal: p.PartTotal}
			groups[key] = append(groups[key], p)
			continue
		}
		singles = append(singles, p)
	}

	consumed := make(map[string]struct{})
	var out []modem.Message

	for key, segs := range groups {
		if len(segs) != key.partTotal {
			for _, p := range segs {
				if _, ok := consumed[p.ID]; ok {
					continue
				}
				out = append(out, partToMessage(p))
				consumed[p.ID] = struct{}{}
			}
			continue
		}

		sort.Slice(segs, func(i, j int) bool {
			return segs[i].Part < segs[j].Part
		})

		var b strings.Builder
		for _, p := range segs {
			b.WriteString(p.Text)
			consumed[p.ID] = struct{}{}
		}

		first := segs[0]
		out = append(out, modem.Message{
			ID:        lowestID(segs),
			From:      first.From,
			State:     first.State,
			Timestamp: first.Timestamp,
			Text:      trimMergedText(b.String()),
			Storage:   first.Storage,
			SMSC:      first.SMSC,
		})
	}

	for _, p := range singles {
		if _, ok := consumed[p.ID]; ok {
			continue
		}
		out = append(out, partToMessage(p))
	}

	sort.Slice(out, func(i, j int) bool {
		return messageIndex(out[i].ID) < messageIndex(out[j].ID)
	})
	return out
}

func partToMessage(p Part) modem.Message {
	return modem.Message{
		ID:        p.ID,
		From:      p.From,
		State:     p.State,
		Timestamp: p.Timestamp,
		Text:      trimMergedText(p.Text),
		Storage:   p.Storage,
		SMSC:      p.SMSC,
	}
}

func lowestID(segs []Part) string {
	low := segs[0].ID
	for _, p := range segs[1:] {
		if messageIndex(p.ID) < messageIndex(low) {
			low = p.ID
		}
	}
	return low
}

func messageIndex(id string) int {
	n, err := strconv.Atoi(strings.TrimSpace(id))
	if err != nil {
		return 1 << 30
	}
	return n
}

func trimMergedText(s string) string {
	return strings.TrimRight(s, "\x00 \r\n")
}
