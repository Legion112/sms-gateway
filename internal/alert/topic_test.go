package alert

import (
	"context"
	"strings"
	"testing"
	"time"
)

func pathObs(subject string, lastOK time.Time, detail string) []Observation {
	return []Observation{{Subject: subject, LastOK: lastOK, Detail: detail}}
}

const labReg = "cs=not registered ps=registered (roaming) ims=unsupported"

// A monitor's topic has to reach the events it produces, or the dispatcher
// cannot tell two conditions about one modem apart.
func TestTopicMonitorStampsItsTopic(t *testing.T) {
	m := NewTopicMonitor(5*time.Minute, TopicSMSPath)

	evs := m.Evaluate(t0.Add(5*time.Minute), pathObs("ec25-backup", t0, labReg))
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Topic != TopicSMSPath {
		t.Errorf("topic = %q, want %q", evs[0].Topic, TopicSMSPath)
	}
	if evs[0].Kind != KindDown {
		t.Errorf("kind = %q, want %q", evs[0].Kind, KindDown)
	}
	if evs[0].Detail != labReg {
		t.Errorf("detail = %q, want the observed registration %q", evs[0].Detail, labReg)
	}
}

// NewMonitor is the silence monitor and must keep stamping that topic, so the
// existing alert keeps its own slot in the dispatcher.
func TestNewMonitorUsesThePollingTopic(t *testing.T) {
	m := NewMonitor(5 * time.Minute)

	evs := m.Evaluate(t0.Add(5*time.Minute), obs("ec25-main", t0))
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Topic != TopicPolling {
		t.Errorf("topic = %q, want %q", evs[0].Topic, TopicPolling)
	}
}

// The recovery carries the detail too: an operator reading "can receive SMS
// again" wants to see which domain came back.
func TestTopicMonitorRecoveryCarriesDetail(t *testing.T) {
	m := NewTopicMonitor(5*time.Minute, TopicSMSPath)
	const recovered = "cs=registered (roaming) ps=registered (roaming) ims=unsupported"

	if evs := m.Evaluate(t0.Add(5*time.Minute), pathObs("ec25-backup", t0, labReg)); len(evs) != 1 {
		t.Fatalf("expected the down event first, got %d", len(evs))
	}
	evs := m.Evaluate(t0.Add(6*time.Minute), pathObs("ec25-backup", t0.Add(6*time.Minute), recovered))
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Kind != KindRecovered {
		t.Errorf("kind = %q, want %q", evs[0].Kind, KindRecovered)
	}
	if evs[0].Detail != recovered {
		t.Errorf("detail = %q, want %q", evs[0].Detail, recovered)
	}
}

// TestBothTopicsAreDeliveredForOneModem is the regression test for the
// dispatcher's keying. It used to hold one pending event per subject, so a
// modem with both problems would have had one of them silently replaced by the
// other, and whichever arrived second would be the only one an operator saw.
func TestBothTopicsAreDeliveredForOneModem(t *testing.T) {
	sender := &stubSender{}
	d := NewDispatcher(NewChannelNotifier(sender), nil)

	d.Enqueue(
		Event{Topic: TopicPolling, Kind: KindDown, Subject: "ec25-backup", At: t0, Silent: 5 * time.Minute},
		Event{Topic: TopicSMSPath, Kind: KindDown, Subject: "ec25-backup", At: t0, Silent: 5 * time.Minute, Detail: labReg},
	)
	if got := d.PendingCount(); got != 2 {
		t.Fatalf("pending = %d, want 2: the two topics must not share a slot", got)
	}

	d.Flush(context.Background())
	if got := d.PendingCount(); got != 0 {
		t.Fatalf("pending after flush = %d, want 0", got)
	}
	if got := len(sender.sent); got != 2 {
		t.Fatalf("sent %d alerts, want 2: %v", got, sender.sent)
	}
	if n := sender.matching("has not polled"); n != 1 {
		t.Errorf("polling alert sent %d times, want 1: %v", n, sender.sent)
	}
	if n := sender.matching("no proven path for incoming SMS"); n != 1 {
		t.Errorf("sms path alert sent %d times, want 1: %v", n, sender.sent)
	}
}

// Within one topic a newer event must still replace an older undelivered one,
// which is what keeps a retry queue from becoming a backlog.
func TestSameTopicStillSupersedes(t *testing.T) {
	sender := &stubSender{}
	d := NewDispatcher(NewChannelNotifier(sender), nil)

	d.Enqueue(Event{Topic: TopicSMSPath, Kind: KindDown, Subject: "ec25-backup", At: t0, Detail: labReg})
	d.Enqueue(Event{Topic: TopicSMSPath, Kind: KindRecovered, Subject: "ec25-backup", At: t0.Add(time.Minute), Detail: labReg})
	if got := d.PendingCount(); got != 1 {
		t.Fatalf("pending = %d, want 1", got)
	}

	d.Flush(context.Background())
	if got := len(sender.sent); got != 1 {
		t.Fatalf("sent %d alerts, want 1: %v", got, sender.sent)
	}
	if !strings.Contains(sender.sent[0], "can receive SMS again") {
		t.Errorf("sent the superseded event: %q", sender.sent[0])
	}
}

// Two modems with the same problem stay separate, as they did before topics.
func TestTopicKeyIncludesTheSubject(t *testing.T) {
	sender := &stubSender{}
	d := NewDispatcher(NewChannelNotifier(sender), nil)

	d.Enqueue(
		Event{Topic: TopicSMSPath, Kind: KindDown, Subject: "ec25-main", At: t0, Detail: labReg},
		Event{Topic: TopicSMSPath, Kind: KindDown, Subject: "ec25-backup", At: t0, Detail: labReg},
	)
	if got := d.PendingCount(); got != 2 {
		t.Fatalf("pending = %d, want 2", got)
	}
}

// The alert text must claim no more than the reading supports: no proven path
// is not proof of no path, and an operator told the wrong thing once stops
// believing the next alert.
func TestSMSPathAlertTextIsHonest(t *testing.T) {
	down := Event{
		Topic: TopicSMSPath, Kind: KindDown, Subject: "ec25-backup",
		At: t0, Silent: 7 * time.Minute, Detail: labReg,
	}
	text := down.Text()

	for _, want := range []string{
		"PROBLEM",
		"ec25-backup",
		"no proven path for incoming SMS",
		"7m0s",
		labReg,
		"the modem answers and its polls succeed",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("alert text is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "are not being received") {
		t.Errorf("the sms path alert must not claim SMS are definitely lost:\n%s", text)
	}
}

func TestSMSPathRecoveryText(t *testing.T) {
	const recovered = "cs=registered (roaming) ps=registered (roaming) ims=unsupported"
	ev := Event{
		Topic: TopicSMSPath, Kind: KindRecovered, Subject: "ec25-backup",
		At: t0, Silent: 7 * time.Minute, Detail: recovered,
	}
	text := ev.Text()

	for _, want := range []string{"RECOVERED", "ec25-backup", "can receive SMS again", "7m0s", recovered} {
		if !strings.Contains(text, want) {
			t.Errorf("recovery text is missing %q:\n%s", want, text)
		}
	}
}

// The polling alert's wording is load-bearing for the other topic's honesty:
// that one does know the SMS are not arriving, and must keep saying so.
func TestPollingAlertTextUnchanged(t *testing.T) {
	ev := Event{Topic: TopicPolling, Kind: KindDown, Subject: "ec25-main", At: t0, Silent: 5 * time.Minute}
	text := ev.Text()

	if !strings.Contains(text, "has not polled for 5m0s") {
		t.Errorf("polling alert text changed:\n%s", text)
	}
	if !strings.Contains(text, "SMS to that SIM are not being received.") {
		t.Errorf("polling alert no longer states the consequence:\n%s", text)
	}
}

func TestEventKey(t *testing.T) {
	polling := Event{Topic: TopicPolling, Subject: "ec25-main"}
	path := Event{Topic: TopicSMSPath, Subject: "ec25-main"}
	other := Event{Topic: TopicPolling, Subject: "ec25-backup"}

	if polling.Key() == path.Key() {
		t.Errorf("two topics share the key %q", polling.Key())
	}
	if polling.Key() == other.Key() {
		t.Errorf("two subjects share the key %q", polling.Key())
	}
	// The kind must not be part of the key, or a down alert and its recovery
	// would queue side by side and both be sent.
	down := Event{Topic: TopicSMSPath, Kind: KindDown, Subject: "ec25-main"}
	up := Event{Topic: TopicSMSPath, Kind: KindRecovered, Subject: "ec25-main"}
	if down.Key() != up.Key() {
		t.Errorf("a condition's down and recovered events have different keys: %q vs %q", down.Key(), up.Key())
	}
}

// stubSender records what the notifier hands it.
type stubSender struct {
	sent []string
}

func (s *stubSender) Name() string { return "ops" }

func (s *stubSender) SendText(_ context.Context, text string) error {
	s.sent = append(s.sent, text)
	return nil
}

func (s *stubSender) matching(substr string) int {
	n := 0
	for _, m := range s.sent {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}
