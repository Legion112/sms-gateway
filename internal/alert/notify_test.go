package alert

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSender struct {
	mu   sync.Mutex
	name string
	sent []string
	fail error
}

func (f *fakeSender) Name() string { return f.name }

func (f *fakeSender) SendText(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, text)
	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeSender) setFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

func downEvent(subject string) Event {
	return Event{Kind: KindDown, Subject: subject, At: t0, Silent: 5 * time.Minute}
}

func TestChannelNotifierSends(t *testing.T) {
	s := &fakeSender{name: "my-telegram"}
	n := NewChannelNotifier(s)

	if err := n.Notify(context.Background(), downEvent("ec25-main")); err != nil {
		t.Fatal(err)
	}
	if s.count() != 1 {
		t.Fatalf("sent %d, want 1", s.count())
	}
	if !strings.Contains(s.sent[0], "ec25-main") {
		t.Fatalf("message = %q", s.sent[0])
	}
}

// TestChannelNotifierFallsBackToASecondDestination: one reachable operator is
// the goal, so a dead first channel must not swallow the alert.
func TestChannelNotifierFallsBackToASecondDestination(t *testing.T) {
	dead := &fakeSender{name: "dead", fail: fmt.Errorf("connection refused")}
	alive := &fakeSender{name: "alive"}
	n := NewChannelNotifier(dead, alive)

	if err := n.Notify(context.Background(), downEvent("ec25-main")); err != nil {
		t.Fatalf("notify failed though a destination was reachable: %v", err)
	}
	if alive.count() != 1 {
		t.Fatalf("second destination got %d, want 1", alive.count())
	}
}

func TestChannelNotifierFailsOnlyWhenEveryDestinationDoes(t *testing.T) {
	a := &fakeSender{name: "a", fail: fmt.Errorf("boom")}
	b := &fakeSender{name: "b", fail: fmt.Errorf("bang")}
	err := NewChannelNotifier(a, b).Notify(context.Background(), downEvent("m"))
	if err == nil {
		t.Fatal("expected an error when nothing could be reached")
	}
	for _, want := range []string{"a:", "b:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v should name %s", err, want)
		}
	}
}

// TestNilNotifierMeansDisabled lets alerting be off without callers branching.
func TestNilNotifierMeansDisabled(t *testing.T) {
	if n := NewChannelNotifier(); n != nil {
		t.Fatal("no senders should yield a nil notifier")
	}
	var d *Dispatcher = NewDispatcher(nil, nil)
	if d != nil {
		t.Fatal("a nil notifier should yield a nil dispatcher")
	}
	// Every method must tolerate the nil receiver.
	d.Enqueue(downEvent("m"))
	d.Flush(context.Background())
	if d.PendingCount() != 0 {
		t.Fatal("nil dispatcher should report nothing pending")
	}
}

// TestDispatcherRetriesUntilDelivered is the whole reason the dispatcher
// exists: the alert that fails to send is the one worth keeping.
func TestDispatcherRetriesUntilDelivered(t *testing.T) {
	s := &fakeSender{name: "tg", fail: fmt.Errorf("telegram unreachable")}
	d := NewDispatcher(NewChannelNotifier(s), nil)

	d.Enqueue(downEvent("ec25-main"))
	for i := 0; i < 3; i++ {
		d.Flush(context.Background())
	}
	if s.count() != 0 {
		t.Fatalf("sent %d while the destination was down", s.count())
	}
	if d.PendingCount() != 1 {
		t.Fatalf("pending = %d, want the alert kept for retry", d.PendingCount())
	}

	s.setFail(nil)
	d.Flush(context.Background())
	if s.count() != 1 {
		t.Fatalf("sent %d after recovery, want 1", s.count())
	}
	if d.PendingCount() != 0 {
		t.Fatalf("pending = %d after a successful send, want 0", d.PendingCount())
	}
}

// TestDispatcherDeliversEachEventOnce guards against a flush loop re-sending
// what already landed.
func TestDispatcherDeliversEachEventOnce(t *testing.T) {
	s := &fakeSender{name: "tg"}
	d := NewDispatcher(NewChannelNotifier(s), nil)

	d.Enqueue(downEvent("ec25-main"))
	for i := 0; i < 5; i++ {
		d.Flush(context.Background())
	}
	if s.count() != 1 {
		t.Fatalf("sent %d, want exactly 1", s.count())
	}
}

// TestNewerEventSupersedesAnUndeliveredOne: an operator needs the current
// state, not a backlog describing a problem that has since resolved.
func TestNewerEventSupersedesAnUndeliveredOne(t *testing.T) {
	s := &fakeSender{name: "tg", fail: fmt.Errorf("down")}
	d := NewDispatcher(NewChannelNotifier(s), nil)

	d.Enqueue(downEvent("ec25-main"))
	d.Flush(context.Background())
	d.Enqueue(Event{Kind: KindRecovered, Subject: "ec25-main", At: t0, Silent: 9 * time.Minute})
	if d.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1 after superseding", d.PendingCount())
	}

	s.setFail(nil)
	d.Flush(context.Background())
	if s.count() != 1 {
		t.Fatalf("sent %d, want 1", s.count())
	}
	if !strings.Contains(s.sent[0], "RECOVERED") {
		t.Fatalf("delivered the stale event: %q", s.sent[0])
	}
}

// TestDispatcherKeepsSubjectsApart stops one unreachable subject's alert from
// blocking another's.
func TestDispatcherKeepsSubjectsApart(t *testing.T) {
	s := &fakeSender{name: "tg"}
	d := NewDispatcher(NewChannelNotifier(s), nil)

	d.Enqueue(downEvent("ec25-main"), downEvent("ec25-backup"))
	if d.PendingCount() != 2 {
		t.Fatalf("pending = %d, want 2", d.PendingCount())
	}
	d.Flush(context.Background())
	if s.count() != 2 {
		t.Fatalf("sent %d, want 2", s.count())
	}
	// Ordered by subject so output does not reshuffle.
	if !strings.Contains(s.sent[0], "ec25-backup") {
		t.Fatalf("first message = %q, want ec25-backup first", s.sent[0])
	}
}

// TestFlushStopsOnCancelledContext keeps shutdown prompt and leaves the
// undelivered alert queued.
func TestFlushStopsOnCancelledContext(t *testing.T) {
	s := &fakeSender{name: "tg"}
	d := NewDispatcher(NewChannelNotifier(s), nil)
	d.Enqueue(downEvent("ec25-main"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.Flush(ctx)

	if s.count() != 0 {
		t.Fatalf("sent %d after cancellation", s.count())
	}
	if d.PendingCount() != 1 {
		t.Fatal("the alert should still be queued")
	}
}
