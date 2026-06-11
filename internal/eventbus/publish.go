package eventbus

import (
	"log/slog"
	"reflect"
	"sync"
)

// --- Client ---

// A Client can publish and subscribe to events on its attached bus.
// Use [Publish] to publish events and [Subscribe] to receive them.
//
// Subscribers that share the same client receive events one at a time,
// in publication order.
type Client struct {
	name string
	bus  *Bus

	mu   sync.Mutex
	pubs publisherSet
	sub  *subscribeState // lazily created on first subscribe
	stop stopFlag        // signaled on Close
}

// Name returns the client's human-readable name.
func (c *Client) Name() string { return c.name }

// Close closes the client. It implicitly closes all publishers and
// subscribers obtained from this client.
func (c *Client) Close() {
	var pubs publisherSet
	var sub *subscribeState

	c.mu.Lock()
	pubs, c.pubs = c.pubs, nil
	sub, c.sub = c.sub, nil
	c.mu.Unlock()

	if sub != nil {
		sub.close()
	}
	for p := range pubs {
		p.Close()
	}
	c.stop.Stop()
}

func (c *Client) isClosed() bool { return c.pubs == nil && c.sub == nil }

// Done returns a channel that is closed when Close is called.
func (c *Client) Done() <-chan struct{} { return c.stop.Done() }

func (c *Client) subscribeState() *subscribeState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subscribeStateLocked()
}

func (c *Client) subscribeStateLocked() *subscribeState {
	if c.sub == nil {
		c.sub = newSubscribeState(c)
	}
	return c.sub
}

func (c *Client) addPublisher(pub publisher) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed() {
		panic("cannot Publish on a closed client")
	}
	c.pubs.Add(pub)
}

func (c *Client) deletePublisher(pub publisher) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pubs.Delete(pub)
}

func (c *Client) publish() chan<- PublishedEvent {
	return c.bus.write
}

func (c *Client) shouldPublish(t reflect.Type) bool {
	return c.bus.shouldPublish(t)
}

// --- publisher ---

// publisher is a uniformly typed wrapper around publisherCore so that
// the bus can enumerate active publishers without per-T itab cost.
type publisher interface {
	publishType() reflect.Type
	Close()
}

// A Publisher publishes typed events on a bus.
type Publisher[T any] struct {
	core *publisherCore
}

// publisherCore is the non-generic implementation of a Publisher.
type publisherCore struct {
	client *Client
	stop   stopFlag
	typ    reflect.Type // cached reflect.TypeFor[T]()
}

func newPublisher[T any](c *Client) *Publisher[T] {
	p := &Publisher[T]{
		core: &publisherCore{
			client: c,
			typ:    reflect.TypeFor[T](),
		},
	}
	c.addPublisher(p.core)
	return p
}

// Close closes the publisher. Calls to Publish after Close silently do
// nothing.
func (p *Publisher[T]) Close() { p.core.Close() }

func (c *publisherCore) Close() {
	c.stop.Stop()
	c.client.deletePublisher(c)
}

func (c *publisherCore) publishType() reflect.Type { return c.typ }

// Publish publishes event v on the bus.
func (p *Publisher[T]) Publish(v T) {
	publish(p.core, v)
}

// publish is the non-generic body of Publisher[T].Publish. The only
// per-T work is boxing v into the Event field (an `any`); everything
// else is shared across all T.
func publish(c *publisherCore, v any) {
	// Silently drop if the publisher has been closed.
	select {
	case <-c.stop.Done():
		return
	default:
	}

	evt := PublishedEvent{
		Event: v,
		Type:  c.typ,
		From:  c.client,
	}

	select {
	case c.client.publish() <- evt:
	case <-c.stop.Done():
	}
}

// ShouldPublish reports whether anyone is subscribed to events of this
// publisher's type. Use it to skip expensive event construction when
// nobody is listening.
func (p *Publisher[T]) ShouldPublish() bool {
	return p.core.client.shouldPublish(p.core.typ)
}

// Publish returns a typed publisher for event type T using the given
// client. It panics if c is closed.
func Publish[T any](c *Client) *Publisher[T] {
	p := newPublisher[T](c)
	// Register the non-generic core so the bus's per-Client publisher
	// set and the publisher interface itab are not parameterized by T.
	c.addPublisher(p.core)
	return p
}

// --- slog helpers ---

func warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}
