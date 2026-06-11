package eventbus

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// --- subscriber interface ---

// subscriber is a uniformly typed wrapper around the typed subscriber
// implementations, so the bus can dispatch without knowing about T.
type subscriber interface {
	subscribeType() reflect.Type
	// dispatch delivers the head value in vals to the subscriber while
	// also handling stop and incoming queue write events.
	dispatch(
		ctx context.Context,
		vals *queue[DeliveredEvent],
		acceptCh func() chan DeliveredEvent,
		snapshot chan chan []DeliveredEvent,
	) bool
	Close()
}

// --- subscribeState ---

// subscribeState handles dispatching events received from a Bus to a
// Client's per-type subscribers. Each Client has at most one
// subscribeState, which multiplexes events by type.
type subscribeState struct {
	client *Client

	dispatcher *worker
	write      chan DeliveredEvent
	snapshot   chan chan []DeliveredEvent

	outputsMu sync.Mutex
	outputs   map[reflect.Type]subscriber
}

func newSubscribeState(c *Client) *subscribeState {
	s := &subscribeState{
		client:   c,
		write:    make(chan DeliveredEvent),
		snapshot: make(chan chan []DeliveredEvent),
		outputs:  map[reflect.Type]subscriber{},
	}
	s.dispatcher = runWorker(s.pump)
	return s
}

func (s *subscribeState) pump(ctx context.Context) {
	var vals queue[DeliveredEvent]
	acceptCh := func() chan DeliveredEvent {
		if vals.Full() {
			return nil
		}
		return s.write
	}
	for {
		if !vals.Empty() {
			val := vals.Peek()
			sub := s.subscriberForType(val.Type)
			if sub == nil {
				// Raced with unsubscribe.
				vals.Drop()
				continue
			}
			if !sub.dispatch(ctx, &vals, acceptCh, s.snapshot) {
				return
			}
		} else {
			select {
			case val := <-s.write:
				vals.Add(val)
			case <-ctx.Done():
				return
			case ch := <-s.snapshot:
				ch <- vals.Snapshot()
			}
		}
	}
}

func (s *subscribeState) addSubscriber(sub subscriber) {
	s.outputsMu.Lock()
	defer s.outputsMu.Unlock()
	t := sub.subscribeType()
	if s.outputs[t] != nil {
		panic(fmt.Errorf("eventbus: double subscription for event %s on client %q", t, s.client.Name()))
	}
	s.outputs[t] = sub
	s.client.bus.subscribe(t, s)
}

func (s *subscribeState) deleteSubscriber(t reflect.Type) {
	s.outputsMu.Lock()
	defer s.outputsMu.Unlock()
	delete(s.outputs, t)
	s.client.bus.unsubscribe(t, s)
}

func (s *subscribeState) subscriberForType(t reflect.Type) subscriber {
	s.outputsMu.Lock()
	defer s.outputsMu.Unlock()
	return s.outputs[t]
}

func (s *subscribeState) close() {
	s.dispatcher.StopAndWait()

	var subs map[reflect.Type]subscriber
	s.outputsMu.Lock()
	subs, s.outputs = s.outputs, nil
	s.outputsMu.Unlock()
	for _, sub := range subs {
		sub.Close()
	}
}

func (s *subscribeState) closed() <-chan struct{} {
	return s.dispatcher.Done()
}

// --- Subscriber ---

// A Subscriber delivers one type of event from a Client. Events are
// sent to the [Subscriber.Events] channel.
type Subscriber[T any] struct {
	core *subscriberCore
	read chan T
}

// newSubscriber creates a typed Subscriber. The generic dispatchTyped
// method is used for delivery because the typed channel send must
// appear lexically inside the select statement; a non-generic bridge
// would require a separate goroutine and ~2.7x throughput regression.
func newSubscriber[T any](r *subscribeState) *Subscriber[T] {
	core := newSubscriberCore(r, reflect.TypeFor[T]())
	s := &Subscriber[T]{
		core: core,
		read: make(chan T),
	}
	core.dispatchFn = func(
		ctx context.Context,
		vals *queue[DeliveredEvent],
		acceptCh func() chan DeliveredEvent,
		snapshot chan chan []DeliveredEvent,
	) bool {
		return s.dispatchTyped(ctx, vals, acceptCh, snapshot)
	}
	return s
}

// dispatchTyped is the per-T dispatch loop for Subscriber[T]. It must
// remain generic because the typed channel send must appear lexically
// inside the select.
func (s *Subscriber[T]) dispatchTyped(
	ctx context.Context,
	vals *queue[DeliveredEvent],
	acceptCh func() chan DeliveredEvent,
	snapshot chan chan []DeliveredEvent,
) bool {
	t := vals.Peek().Event.(T)

	start := time.Now()
	s.core.slow.Reset(slowSubscriberTimeout)
	defer s.core.slow.Stop()

	for {
		select {
		case s.read <- t:
			vals.Drop()
			return true
		case val := <-acceptCh():
			vals.Add(val)
		case <-ctx.Done():
			return false
		case ch := <-snapshot:
			ch <- vals.Snapshot()
		case <-s.core.slow.C:
			warn("eventbus: subscriber is slow",
				"type", s.core.typeName,
				"elapsed", time.Since(start).String(),
			)
			s.core.slow.Reset(slowSubscriberTimeout)
		}
	}
}

// Events returns the channel on which events are delivered.
func (s *Subscriber[T]) Events() <-chan T {
	return s.read
}

// Done returns a channel that is closed when the subscriber is closed.
func (s *Subscriber[T]) Done() <-chan struct{} {
	return s.core.stop.Done()
}

// Close closes the Subscriber. After Close, receives on Events block
// forever.
func (s *Subscriber[T]) Close() { s.core.Close() }

// --- SubscriberFunc ---

// A SubscriberFunc delivers events to a callback function instead of a
// channel. The callback is invoked synchronously from the client's
// dispatch goroutine; it must not block for extended periods.
type SubscriberFunc[T any] struct {
	core *subscriberCore
}

func newSubscriberFunc[T any](r *subscribeState, f func(T)) *SubscriberFunc[T] {
	core := newSubscriberCore(r, reflect.TypeFor[T]())
	core.dispatchFn = func(
		ctx context.Context,
		vals *queue[DeliveredEvent],
		acceptCh func() chan DeliveredEvent,
		snapshot chan chan []DeliveredEvent,
	) bool {
		t := vals.Peek().Event.(T)
		callDone := make(chan struct{})
		go runFuncCallback(f, t, callDone)
		return dispatchFunc(ctx, core, vals, acceptCh, snapshot, callDone)
	}
	return &SubscriberFunc[T]{core: core}
}

// Close closes the SubscriberFunc.
func (s *SubscriberFunc[T]) Close() { s.core.Close() }

// --- subscriberCore ---

// subscriberCore is the non-generic backing for both Subscriber[T] and
// SubscriberFunc[T]. It implements the package-private subscriber
// interface so that the bus can store subscribers without per-T
// itabs or dictionaries.
type subscriberCore struct {
	stop       stopFlag
	unregister func(reflect.Type)
	slow       *time.Timer
	typ        reflect.Type
	typeName   string

	// dispatchFn is the per-T dispatch closure. It performs the type
	// assertion and runs the typed delivery (channel send or callback).
	dispatchFn func(
		ctx context.Context,
		vals *queue[DeliveredEvent],
		acceptCh func() chan DeliveredEvent,
		snapshot chan chan []DeliveredEvent,
	) bool
}

func newSubscriberCore(r *subscribeState, typ reflect.Type) *subscriberCore {
	slow := time.NewTimer(0)
	slow.Stop() // reset in dispatch
	core := &subscriberCore{
		slow:     slow,
		typ:      typ,
		typeName: typ.String(),
	}
	core.unregister = r.deleteSubscriber
	return core
}

func (c *subscriberCore) Close() {
	c.stop.Stop()
	c.unregister(c.typ)
}

func (c *subscriberCore) subscribeType() reflect.Type { return c.typ }

func (c *subscriberCore) dispatch(
	ctx context.Context,
	vals *queue[DeliveredEvent],
	acceptCh func() chan DeliveredEvent,
	snapshot chan chan []DeliveredEvent,
) bool {
	return c.dispatchFn(ctx, vals, acceptCh, snapshot)
}

// dispatchFunc is the non-generic body of SubscriberFunc dispatch. It
// waits for the user callback to complete while also handling new
// events, context cancellation, and snapshots.
func dispatchFunc(
	ctx context.Context,
	core *subscriberCore,
	vals *queue[DeliveredEvent],
	acceptCh func() chan DeliveredEvent,
	snapshot chan chan []DeliveredEvent,
	callDone chan struct{},
) bool {
	start := time.Now()
	core.slow.Reset(slowSubscriberTimeout)
	defer core.slow.Stop()

	for {
		select {
		case <-callDone:
			vals.Drop()
			return true
		case val := <-acceptCh():
			vals.Add(val)
		case <-ctx.Done():
			core.slow.Reset(5 * slowSubscriberTimeout)
			select {
			case <-core.slow.C:
				warn("eventbus: giving up on slow subscriber at close",
					"type", core.typeName,
					"elapsed", time.Since(start).String(),
				)
			case <-callDone:
			}
			return false
		case ch := <-snapshot:
			ch <- vals.Snapshot()
		case <-core.slow.C:
			warn("eventbus: subscriber is slow",
				"type", core.typeName,
				"elapsed", time.Since(start).String(),
			)
			core.slow.Reset(slowSubscriberTimeout)
		}
	}
}

// runFuncCallback runs f(t) and closes done when it returns. As a
// regular generic function (not a closure), `go runFuncCallback(f, t, done)`
// binds arguments to the goroutine's frame directly, avoiding a
// per-event closure allocation.
func runFuncCallback[T any](f func(T), t T, done chan struct{}) {
	defer close(done)
	f(t)
}

// --- Subscribe / SubscribeFunc ---

// Subscribe returns a typed subscriber for events of type T delivered
// through the given client. It panics if c already has a subscriber for
// type T, or if c is closed.
func Subscribe[T any](c *Client) *Subscriber[T] {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed() {
		panic("cannot Subscribe on a closed client")
	}

	r := c.subscribeStateLocked()
	s := newSubscriber[T](r)
	r.addSubscriber(s.core)
	return s
}

// SubscribeFunc is like Subscribe but calls f for each event of type T.
// f is called synchronously from the client's dispatch goroutine and
// must not block for extended periods.
func SubscribeFunc[T any](c *Client, f func(T)) *SubscriberFunc[T] {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed() {
		panic("cannot SubscribeFunc on a closed client")
	}

	r := c.subscribeStateLocked()
	s := newSubscriberFunc[T](r, f)
	r.addSubscriber(s.core)
	return s
}
