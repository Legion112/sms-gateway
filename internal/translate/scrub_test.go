package translate

import "strings"

import "testing"

const secretBody = "Demobank: data plateja 29 October 2026 08:25:33 summa 30.00 USD s karti ***0002. Kod podtverjdeniya: 100001"

func TestScrubRemovesAWholeEchoedBody(t *testing.T) {
	msg := "translate: 400 Bad Request: rejected q=" + secretBody
	got := scrubBody(msg, secretBody)
	if strings.Contains(got, "100001") || strings.Contains(got, "Demobank") {
		t.Errorf("body survived: %q", got)
	}
	if !strings.Contains(got, "400 Bad Request") {
		t.Errorf("diagnosis lost: %q", got)
	}
}

// A service that truncates the request before quoting it is the common case,
// and the one-time code tends to be at the end -- so a head echo must go too.
func TestScrubRemovesATruncatedHead(t *testing.T) {
	got := scrubBody("upstream rejected: "+secretBody[:60], secretBody)
	if strings.Contains(got, "Demobank") {
		t.Errorf("head survived: %q", got)
	}
}

// And the tail, which is where the code lives.
func TestScrubRemovesATail(t *testing.T) {
	tail := secretBody[len(secretBody)-40:]
	got := scrubBody("could not parse near "+tail, secretBody)
	if strings.Contains(got, "100001") {
		t.Errorf("one-time code survived: %q", got)
	}
}

func TestScrubLeavesAnUnrelatedErrorAlone(t *testing.T) {
	const msg = "Post \"http://192.0.2.5:5000/translate\": dial tcp 192.0.2.5:5000: connect: connection refused"
	if got := scrubBody(msg, secretBody); got != msg {
		t.Errorf("rewrote a clean error:\n got %q\nwant %q", got, msg)
	}
}

func TestScrubIgnoresABodyTooShortToIdentify(t *testing.T) {
	const msg = "translate: 503 Service Unavailable"
	if got := scrubBody(msg, "hi"); got != msg {
		t.Errorf("got %q", got)
	}
}
