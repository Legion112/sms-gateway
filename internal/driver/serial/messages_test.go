package serial

import (
	"testing"

	"github.com/legion/sms-gateway/internal/smsconcat"
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

func TestParseQCMGRQuectelFormat(t *testing.T) {
	resp := "+QCMGR: \"REC READ\",\"6610110182111117109105110103\",,\"26/07/04,21:23:24+20\",120,1,3\nРекомендации\nOK"
	part, ok := parseQCMGR("25", resp)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if part.Timestamp != "26/07/04,21:23:24+20" {
		t.Fatalf("timestamp = %q", part.Timestamp)
	}
	if part.Part != 1 || part.PartTotal != 3 {
		t.Fatalf("concat meta: part=%d total=%d", part.Part, part.PartTotal)
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
