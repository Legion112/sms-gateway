package serial

import (
	"context"
	"testing"

	"github.com/legion/sms-gateway/internal/smsconcat"
	"github.com/legion/sms-gateway/internal/smspdu"
)

func TestParseCMGL(t *testing.T) {
	resp := "+CMGL: 1,\"REC UNREAD\",\"+79162821457\",\"\",\"25/06/11,20:08:59+12\"\nHello world\n+CMGL: 2,\"STO SENT\",\"+15551212\",\"\",\"25/06/10,10:00:00+00\"\nSent msg\nOK"

	msgs := parseCMGL(resp)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].ID != "1" || msgs[0].From != "+79162821457" || msgs[0].Text != "Hello world" {
		t.Fatalf("msg0: %+v", msgs[0])
	}
	if msgs[0].State != "received (unread)" {
		t.Fatalf("state = %q", msgs[0].State)
	}
	if msgs[1].Text != "Sent msg" {
		t.Fatalf("msg1 text = %q", msgs[1].Text)
	}
}

func TestParseCMGLQuectelFormat(t *testing.T) {
	resp := "+CMGL: 0,\"REC READ\",\"+79956009754\",,26/07/04,21:21:50+20\nHello this sms\n+CMGL: 36,\"REC READ\",\"48545552\",,26/07/04,21:24:14+20\nUzbek text\nOK"
	msgs := parseCMGL(resp)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].ID != "0" || msgs[0].From != "+79956009754" || msgs[0].Text != "Hello this sms" {
		t.Fatalf("msg0: %+v", msgs[0])
	}
	if msgs[0].Timestamp != "26/07/04,21:21:50+20" {
		t.Fatalf("timestamp = %q", msgs[0].Timestamp)
	}
	if msgs[1].From != "48545552" {
		t.Fatalf("msg1 from = %q", msgs[1].From)
	}
}

func TestParseCMGLEmpty(t *testing.T) {
	if msgs := parseCMGL("OK"); len(msgs) != 0 {
		t.Fatalf("got %v", msgs)
	}
}

func TestParseCMGLIndices(t *testing.T) {
	resp := "+CMGL: 25,\"REC READ\",\"sender\",,26/07/04,21:23:24+20\npart1\n+CMGL: 26,\"REC READ\",\"sender\",,26/07/04,21:23:27+20\npart2\nOK"
	got := parseCMGLIndices(resp)
	if len(got) != 2 || got[0] != "25" || got[1] != "26" {
		t.Fatalf("got %v", got)
	}
}

func TestParseCMGLPDU(t *testing.T) {
	hexPDU := "0791999810584059000B919759069057F400006270401212050233C8329BFD06D1D1E93968DE9E83D273D0BCEC2683E86F503BED2E83AA7A71799D9ED3C36ED034D9048DC3723208"
	resp := "+CMGL: 0,1,,176\n" + hexPDU + "\nOK"
	entries := parseCMGLPDU(resp)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].index != "0" || entries[0].stat != "1" {
		t.Fatalf("entry: %+v", entries[0])
	}
	if entries[0].hexPDU != hexPDU {
		t.Fatalf("hex mismatch")
	}
}

func TestParseCMGLPDUMulti(t *testing.T) {
	resp := "+CMGL: 25,1,,162\n4012d0c27259faaeb7d3ee330008627040121215028c0500035209020020\n+CMGL: 26,1,,80\n079199981058405900\nOK"
	entries := parseCMGLPDU(resp)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].index != "25" || entries[1].index != "26" {
		t.Fatalf("indices: %q %q", entries[0].index, entries[1].index)
	}
}

func TestParseCMGLPDUToPart(t *testing.T) {
	hexPDU := "0791999810584059000B919759069057F400006270401212050233C8329BFD06D1D1E93968DE9E83D273D0BCEC2683E86F503BED2E83AA7A71799D9ED3C36ED034D9048DC3723208"
	entries := parseCMGLPDU("+CMGL: 0,1,,176\n" + hexPDU + "\nOK")
	if len(entries) != 1 {
		t.Fatal("expected one entry")
	}
	deliver, err := smspdu.ParseHexDeliver(entries[0].hexPDU)
	if err != nil {
		t.Fatalf("parse deliver: %v", err)
	}
	part := smsconcat.Part{
		ID:        entries[0].index,
		From:      deliver.From,
		State:     "received (read)",
		Timestamp: deliver.Timestamp,
		Text:      deliver.Text,
		Storage:   "modem",
		ConcatRef: deliver.ConcatRef,
		Part:      deliver.Part,
		PartTotal: deliver.PartTotal,
	}
	if part.From != "+79956009754" {
		t.Fatalf("from = %q", part.From)
	}
}

func TestParseQCMGRConcat(t *testing.T) {
	resp := "+QCMGR: \"REC READ\",\"+8615056913384\",,\"13/07/30,14:44:37+32\",120,1,2\nABCD\nOK"
	part, ok := parseQCMGR("3", resp)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if part.ID != "3" || part.From != "+8615056913384" || part.Text != "ABCD" {
		t.Fatalf("part: %+v", part)
	}
	if part.ConcatRef != 120 || part.Part != 1 || part.PartTotal != 2 {
		t.Fatalf("concat meta: ref=%d part=%d total=%d", part.ConcatRef, part.Part, part.PartTotal)
	}
}

func TestParseQCMGRNonConcat(t *testing.T) {
	resp := "+QCMGR: \"REC READ\",\"+79956009754\",,26/07/04,21:21:50+20\nHello\nOK"
	part, ok := parseQCMGR("0", resp)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if part.PartTotal != 0 || part.Part != 0 || part.ConcatRef != 0 {
		t.Fatalf("expected non-concat, got ref=%d part=%d total=%d", part.ConcatRef, part.Part, part.PartTotal)
	}
	if part.Text != "Hello" {
		t.Fatalf("text = %q", part.Text)
	}
}

func TestMergeQCMGRPartsIntegration(t *testing.T) {
	parts := []smsconcat.Part{
		parsePart(t, "25", "+QCMGR: \"REC READ\",\"sender\",,\"26/07/04,21:23:24+20\",120,1,3\npart one\nOK"),
		parsePart(t, "26", "+QCMGR: \"REC READ\",\"sender\",,\"26/07/04,21:23:27+20\",120,2,3\npart two\nOK"),
		parsePart(t, "27", "+QCMGR: \"REC READ\",\"sender\",,\"26/07/04,21:23:31+20\",120,3,3\npart three\nOK"),
	}
	msgs := smsconcat.Merge(parts)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Text != "part onepart twopart three" {
		t.Fatalf("text = %q", msgs[0].Text)
	}
}

func parsePart(t *testing.T, id, resp string) smsconcat.Part {
	t.Helper()
	part, ok := parseQCMGR(id, resp)
	if !ok {
		t.Fatalf("parseQCMGR(%q) failed", id)
	}
	return part
}

func TestDeleteMessageRejectsNonIndex(t *testing.T) {
	d := &Driver{}
	ctx := context.Background()
	for _, id := range []string{"", "  ", "abc", "/org/freedesktop/ModemManager1/SMS/3"} {
		if err := d.DeleteMessage(ctx, id); err == nil {
			t.Fatalf("id %q: want an error before any AT command is written", id)
		}
	}
}
