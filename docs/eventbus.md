# Event Bus Architecture

Tau uses a central in-process event bus for communication between subsystems. The design is adapted from Tailscale's
`util/eventbus` package (BSD-3-Clause).

## Overview

The event bus connects **publishers** of typed events with **subscribers** interested in those events. There is one
global bus per process, created by `internal/app/run.go` at startup and passed to every subsystem that needs it.

```go
bus := eventbus.New()
defer bus.Close()
```

## Core Concepts

### Type-as-Topic

Events are routed by their Go type, not by string constants. There is no topic naming convention to remember, no typos
to debug:

```go
pub := eventbus.Publish[ChatEvent](client)    // publishes ChatEvent
sub := eventbus.Subscribe[ChatEvent](client)  // subscribes to ChatEvent
// No "coordinator.events" string anywhere.
```

### Clients

A **Client** is a named handle on the bus. Each subsystem creates its own client:

```go
coordClient := bus.Client("coordinator")  // agent coordinator
tuiClient   := bus.Client("tui")           // terminal UI
skillsClient := bus.Client("skills")        // skill catalog
```

A client owns its publishers and subscribers. `Client.Close()` cascades cleanup - it closes every publisher and
subscriber created through that client.

### Publishers

A `Publisher[T]` sends typed events onto the bus. Create one per event type per client:

```go
chatPub := eventbus.Publish[ChatEvent](coordClient)
chatPub.Publish(ChatResponseStartedEvent{...})
```

- `Publish()` blocks briefly if the bus is backlogged - this is intentional backpressure.
- `ShouldPublish()` reports whether anyone is subscribed, so you can skip expensive event construction.
- `Close()` stops the publisher.

### Subscribers

A `Subscriber[T]` receives typed events from the bus:

```go
chatSub := eventbus.Subscribe[ChatEvent](tuiClient)
for ev := range chatSub.Events() {
    // handle event
}
```

- `Events()` returns a `<-chan T` for receiving events.
- `Close()` stops the subscriber and unregisters from the bus.
- Events are delivered one at a time, in publication order.
- Slow subscribers (>5s to accept an event) trigger a warning log.

Alternatively, use `SubscribeFunc` for callback-based subscriptions:

```go
eventbus.SubscribeFunc[ChatEvent](client, func(ev ChatEvent) {
    // handle event synchronously - don't block!
})
```

## Event Flow

```
Publisher[T].Publish(v T)
        │
        ▼  (boxes v into PublishedEvent{Event: v, Type: reflect.TypeFor[T](), From: client})
        │
bus.write chan ──► bus.pump() goroutine (single router, total ordering)
        │
        ▼  (looks up bus.topics[event.Type] - all subscribers for this type)
        │
per-client subscribeState.write chan ──► subscribeState.pump() goroutine
        │
        ▼  (looks up subscriberForType - the Subscriber[T] for this type)
        │
subscriber.dispatch() ──► typed channel send (Subscriber[T]) or callback (SubscriberFunc[T])
        │
        ▼
sub.Events() channel ──► application handler
```

## Current Usage

| Client          | Publishes        | Subscribes       | Purpose                                                               |
| --------------- | ---------------- | ---------------- | --------------------------------------------------------------------- |
| `"coordinator"` | `chat.ChatEvent` | -                | Agent runtime publishes session, streaming, tool, notification events |
| `"tui"`         | -                | `chat.ChatEvent` | Terminal UI subscribes to render updates                              |
| `"skills"`      | `skills.Event`   | -                | Skill manager publishes refreshed catalog snapshots                   |

## Concurrency Properties

- **Total ordering**: The bus serializes all published events through a single pump goroutine. Two events cannot be
  published at the same instant - one always happens before the other.
- **Per-client ordering**: Each client receives events in publication order. If event A happens before event B, every
  client sees A before B.
- **Independent clients**: Different clients progress through the timeline independently. Client C1 may deliver both A
  and B before client C2 delivers A.
- **Actor model**: Structure your code so that each subsystem has authority over its local state, and interacts with
  other subsystems solely through events.

## Interface Type Routing

Tau extends the Tailscale design with **interface-type routing**. Many tau events are defined as a `ChatEvent` interface
with concrete implementations. The bus routes by the publisher's **declared** type parameter, not the concrete value
type:

```go
// Publisher declared as Publisher[ChatEvent]
pub := eventbus.Publish[chat.ChatEvent](client)

// Concrete event type is ChatResponseStartedEvent
pub.Publish(chat.ChatResponseStartedEvent{...})
// Routes to all Subscribe[ChatEvent] subscribers - works correctly.
```

This is implemented via a `Type` field on `PublishedEvent` and `DeliveredEvent` that carries the publisher's
`reflect.TypeFor[T]()` through the routing pipeline.

## Adding a New Event Type

1. Define the event type in the appropriate domain package (e.g., `internal/chat/types.go`).
2. If publishing: create a `Publisher[T]` via `eventbus.Publish[T](client)`.
3. If subscribing: create a `Subscriber[T]` via `eventbus.Subscribe[T](client)`.
4. Both sides import `eventbus` and the type's package - never each other.

## When NOT to Use the Event Bus

- **Point-to-point commands**: `ChatCommand` is sent directly to the coordinator via `Send()`, not through the bus. Use
  a direct channel or method call for 1:1 communication.
- **High-frequency streaming deltas**: Streaming response deltas are published as events, but if volume becomes
  problematic, consider a dedicated streaming channel.
- **Request-response patterns**: The bus is fire-and-forget multicast. For request-response, use a separate mechanism
  (like the coordinator's interactive prompt bridge).
