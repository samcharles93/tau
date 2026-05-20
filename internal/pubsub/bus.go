package pubsub

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var (
	ErrClosed        = errors.New("pubsub bus is closed")
	ErrInvalidBuffer = errors.New("pubsub buffer must be zero or greater")
	ErrTopicRequired = errors.New("pubsub topic is required")
)

type Bus[T any] struct {
	mu     sync.Mutex
	topics map[string]map[uint64]chan T
	nextID uint64
	closed bool
}

type Subscription[T any] struct {
	channel     <-chan T
	unsubscribe func()
	once        sync.Once
}

func New[T any]() *Bus[T] {
	return &Bus[T]{
		topics: make(map[string]map[uint64]chan T),
	}
}

func (b *Bus[T]) Subscribe(topic string, buffer int) (*Subscription[T], error) {
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return nil, err
	}
	if buffer < 0 {
		return nil, ErrInvalidBuffer
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, ErrClosed
	}

	b.nextID++
	id := b.nextID
	channel := make(chan T, buffer)
	if b.topics[normalized] == nil {
		b.topics[normalized] = make(map[uint64]chan T)
	}
	b.topics[normalized][id] = channel

	return &Subscription[T]{
		channel: channel,
		unsubscribe: func() {
			b.unsubscribe(normalized, id)
		},
	}, nil
}

func (b *Bus[T]) Publish(ctx context.Context, topic string, message T) error {
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	for _, channel := range b.topics[normalized] {
		select {
		case channel <- message:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (b *Bus[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for topic, subscriptions := range b.topics {
		for id, channel := range subscriptions {
			close(channel)
			delete(subscriptions, id)
		}
		delete(b.topics, topic)
	}
}

func (s *Subscription[T]) Channel() <-chan T {
	if s == nil {
		return nil
	}
	return s.channel
}

func (s *Subscription[T]) Unsubscribe() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.unsubscribe != nil {
			s.unsubscribe()
		}
	})
}

func (b *Bus[T]) unsubscribe(topic string, id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subscriptions, ok := b.topics[topic]
	if !ok {
		return
	}
	channel, ok := subscriptions[id]
	if !ok {
		return
	}

	delete(subscriptions, id)
	if len(subscriptions) == 0 {
		delete(b.topics, topic)
	}
	close(channel)
}

func normalizeTopic(topic string) (string, error) {
	normalized := strings.TrimSpace(topic)
	if normalized == "" {
		return "", ErrTopicRequired
	}
	return normalized, nil
}
