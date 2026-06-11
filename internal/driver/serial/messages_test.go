package serial

import "testing"

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

func TestParseCMGLEmpty(t *testing.T) {
	if msgs := parseCMGL("OK"); len(msgs) != 0 {
		t.Fatalf("got %v", msgs)
	}
}
