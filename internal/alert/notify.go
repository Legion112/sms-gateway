package alert

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Notifier delivers one event. This is the seam: today ChannelNotifier below
// writes to the same Telegram channels that carry forwarded SMS, and a
// standalone notification service replaces it by implementing this and nothing
// else.
type Notifier interface {
	Notify(ctx context.Context, ev Event) error
}

// TextSender is the part of a forward channel that alerting needs. Declaring
// the narrow interface here rather than importing the channel package keeps
// alert free of any dependency on how SMS are forwarded.
type TextSender interface {
	Name() string
	SendText(ctx context.Context, text string) error
}

// ChannelNotifier sends alerts through one or more existing channels.
//
// Delivery succeeds if any channel accepts the event. One operator reachable is
// the goal; failing the whole alert because a second destination is down would
// make the alert less reliable the more places it is sent to.
type ChannelNotifier struct {
	senders []TextSender
}

// NewChannelNotifier returns nil when given no senders, which callers treat as
// alerting being switched off.
func NewChannelNotifier(senders ...TextSender) *ChannelNotifier {
	if len(senders) == 0 {
		return nil
	}
	return &ChannelNotifier{senders: senders}
}

// Notify delivers ev, reporting an error only if every destination refused it.
func (n *ChannelNotifier) Notify(ctx context.Context, ev Event) error {
	if n == nil || len(n.senders) == 0 {
		return fmt.Errorf("no alert destinations configured")
	}
	text := ev.Text()
	var errs []error
	for _, s := range n.senders {
		if err := s.SendText(ctx, text); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			continue
		}
		return nil
	}
	return fmt.Errorf("every alert destination failed: %v", errs)
}

// Dispatcher holds events until they are delivered.
//
// An alert dropped because the network was briefly unreachable is exactly the
// alert that mattered, so a failed send is retried on the next flush. At most
// one event per subject is kept: an operator needs the subject's current state,
// not a backlog, so a newer event replaces an older undelivered one.
type Dispatcher struct {
	notifier Notifier
	log      func(format string, args ...any)

	mu      sync.Mutex
	pending map[string]Event
}

// NewDispatcher returns nil when notifier is nil, so alerting stays off without
// the caller having to branch on it: every method tolerates a nil receiver.
func NewDispatcher(notifier Notifier, log func(string, ...any)) *Dispatcher {
	if notifier == nil {
		return nil
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Dispatcher{notifier: notifier, log: log, pending: map[string]Event{}}
}

// Enqueue queues events for delivery, superseding anything still undelivered
// for the same subject.
func (d *Dispatcher) Enqueue(events ...Event) {
	if d == nil || len(events) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ev := range events {
		d.pending[ev.Subject] = ev
	}
}

// Flush attempts every pending event, keeping the ones that fail. It is safe to
// call when nothing is pending, which is what makes it double as the retry.
func (d *Dispatcher) Flush(ctx context.Context) {
	if d == nil {
		return
	}
	d.mu.Lock()
	subjects := make([]string, 0, len(d.pending))
	for subject := range d.pending {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	batch := make([]Event, 0, len(subjects))
	for _, subject := range subjects {
		batch = append(batch, d.pending[subject])
	}
	d.mu.Unlock()

	for _, ev := range batch {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := d.notifier.Notify(ctx, ev); err != nil {
			d.log("alert_failed subject=%s kind=%s error=%v (will retry)", ev.Subject, ev.Kind, err)
			continue
		}
		d.log("alert_sent subject=%s kind=%s silent=%s", ev.Subject, ev.Kind, round(ev.Silent))

		d.mu.Lock()
		// Only drop it if nothing newer arrived while the send was in flight.
		if cur, ok := d.pending[ev.Subject]; ok && cur == ev {
			delete(d.pending, ev.Subject)
		}
		d.mu.Unlock()
	}
}

// PendingCount reports how many events are still undelivered.
func (d *Dispatcher) PendingCount() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}
