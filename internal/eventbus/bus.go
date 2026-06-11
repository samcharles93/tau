package eventbus

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"time"
)

// PublishedEvent wraps an event alongside its source client and declared
// publish type. The Type field carries the publisher's declared type parameter
// rather than the concrete value type, so that interface-typed publishers
// (e.g. Publisher[ChatEvent]) route correctly to subscribers of the same
// interface type.
type PublishedEvent struct {
	Event any
	Type  reflect.Type
	From  *Client
}

// DeliveredEvent wraps an event alongside its source, destination, and
// declared publish type. Type carries the publisher's type parameter so
// that interface-typed routing works correctly.
type DeliveredEvent struct {
	Event any
	Type  reflect.Type
	From  *Client
	To    *Client
}

// Bus is an event bus that distributes published events to interested
// subscribers. Typically there is one global Bus per process.
//
// Events are routed by their Go type: Publish[ChatEvent] delivers to
// all Subscribe[ChatEvent] subscribers. Multiple clients may subscribe
// to the same type; all receive every event (multicast).
//
// Published events are totally ordered: the bus serializes all
// Publish calls and delivers events to each client in publication order.
type Bus struct {
	router   *worker
	write    chan PublishedEvent
	snapshot chan chan []PublishedEvent

	topicsMu sync.Mutex
	topics   map[reflect.Type][]*subscribeState

	clientsMu sync.Mutex
	clients   clientSet
}

// New returns a new Bus ready for use.
func New() *Bus {
	b := &Bus{
		write:    make(chan PublishedEvent),
		snapshot: make(chan chan []PublishedEvent),
		topics:   map[reflect.Type][]*subscribeState{},
		clients:  clientSet{},
	}
	b.router = runWorker(b.pump)
	return b
}

// Client returns a new named client on the bus. Use [Publish] to
// emit events through the client and [Subscribe] to receive them.
//
// The client name is used only for debugging. Aim for something short
// and unique, e.g. "coordinator" or "tui".
func (b *Bus) Client(name string) *Client {
	c := &Client{
		name: name,
		bus:  b,
		pubs: map[publisher]struct{}{},
	}
	b.clientsMu.Lock()
	b.clients.Add(c)
	b.clientsMu.Unlock()
	return c
}

// Close closes the bus. It implicitly closes all clients, publishers
// and subscribers attached to the bus. Close blocks until the bus is
// fully shut down. The bus is permanently unusable after closing.
func (b *Bus) Close() {
	b.router.StopAndWait()

	b.clientsMu.Lock()
	clients := make([]*Client, 0, len(b.clients))
	for c := range b.clients {
		clients = append(clients, c)
	}
	b.clients = nil
	b.clientsMu.Unlock()

	for _, c := range clients {
		c.Close()
	}
}

// pump is the main routing loop. It receives published events, looks up
// subscribers by type, and delivers to each subscriber's per-client
// dispatch queue. The pump serializes all events, establishing a total
// order.
func (b *Bus) pump(ctx context.Context) {
	const maxPublishedEvents = 16
	vals := queue[PublishedEvent]{capacity: maxPublishedEvents}
	acceptCh := func() chan PublishedEvent {
		if vals.Full() {
			return nil
		}
		return b.write
	}
	for {
		// Drain all pending events.
		for !vals.Empty() {
			val := vals.Peek()
			dests := b.dests(val.Type)

			for _, d := range dests {
				evt := DeliveredEvent{Type: val.Type,
					Event: val.Event,
					From:  val.From,
					To:    d.client,
				}
			deliverOne:
				for {
					select {
					case d.write <- evt:
						break deliverOne
					case <-d.closed():
						break deliverOne
					case in := <-acceptCh():
						vals.Add(in)
					case <-ctx.Done():
						return
					case ch := <-b.snapshot:
						ch <- vals.Snapshot()
					}
				}
			}
			vals.Drop()
		}

		// Inbound queue empty, wait for at least one event.
		for vals.Empty() {
			select {
			case <-ctx.Done():
				return
			case in := <-b.write:
				vals.Add(in)
			case ch := <-b.snapshot:
				ch <- nil
			}
		}
	}
}

// dests returns the subscribers for the given event type.
func (b *Bus) dests(t reflect.Type) []*subscribeState {
	b.topicsMu.Lock()
	defer b.topicsMu.Unlock()
	return b.topics[t]
}

// shouldPublish reports whether any subscriber is interested in events
// of the given type.
func (b *Bus) shouldPublish(t reflect.Type) bool {
	b.topicsMu.Lock()
	defer b.topicsMu.Unlock()
	return len(b.topics[t]) > 0
}

// subscribe registers a subscribeState for the given type and returns
// a function that unregisters it.
func (b *Bus) subscribe(t reflect.Type, q *subscribeState) (cancel func()) {
	b.topicsMu.Lock()
	defer b.topicsMu.Unlock()
	b.topics[t] = append(b.topics[t], q)
	return func() {
		b.unsubscribe(t, q)
	}
}

func (b *Bus) unsubscribe(t reflect.Type, q *subscribeState) {
	b.topicsMu.Lock()
	defer b.topicsMu.Unlock()

	subs := b.topics[t]
	for i, s := range subs {
		if s == q {
			// Replace the slice to avoid races with the pump
			// goroutine, which reads without holding the lock.
			cloned := make([]*subscribeState, len(subs))
			copy(cloned, subs)
			b.topics[t] = append(cloned[:i], cloned[i+1:]...)
			return
		}
	}
}

// snapshotPublishQueue returns a snapshot of the publish queue.
func (b *Bus) snapshotPublishQueue() []PublishedEvent {
	resp := make(chan []PublishedEvent)
	select {
	case b.snapshot <- resp:
		return <-resp
	case <-b.router.Done():
		return nil
	}
}

// --- client set helpers ---

type clientSet map[*Client]struct{}

func (s clientSet) Add(c *Client)   { s[c] = struct{}{} }
func (s clientSet) slice() []*Client {
	out := make([]*Client, 0, len(s))
	for c := range s {
		out = append(out, c)
	}
	return out
}

// --- publisher set helpers ---

type publisherSet map[publisher]struct{}

func (s publisherSet) Add(p publisher)  { s[p] = struct{}{} }
func (s publisherSet) Delete(p publisher) { delete(s, p) }

// --- worker ---
// A worker runs a goroutine and helps coordinate its shutdown.

type worker struct {
	ctx     context.Context
	stop    context.CancelFunc
	stopped chan struct{}
}

func runWorker(fn func(context.Context)) *worker {
	ctx, stop := context.WithCancel(context.Background())
	w := &worker{
		ctx:     ctx,
		stop:    stop,
		stopped: make(chan struct{}),
	}
	go w.run(fn)
	return w
}

func (w *worker) run(fn func(context.Context)) {
	defer close(w.stopped)
	fn(w.ctx)
}

// Stop signals the worker goroutine to shut down.
func (w *worker) Stop() { w.stop() }

// Done returns a channel that is closed when the worker goroutine exits.
func (w *worker) Done() <-chan struct{} { return w.stopped }

// StopAndWait signals shutdown and waits for the worker to exit.
func (w *worker) StopAndWait() {
	w.stop()
	<-w.stopped
}

// --- stopFlag ---
// A one-way shutdown signal that's lighter than a cancellable context.

type stopFlag struct {
	mu             sync.Mutex
	ch             chan struct{}
	alreadyStopped bool
}

func (s *stopFlag) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.alreadyStopped {
		return
	}
	s.alreadyStopped = true
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	close(s.ch)
}

func (s *stopFlag) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	return s.ch
}

// slowSubscriberTimeout is how long a subscriber can block before a
// warning is logged.
const slowSubscriberTimeout = 5 * time.Second

func logf(format string, args ...any) {
	slog.Debug(format, args...)
}
