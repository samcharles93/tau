package bridge

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/skills"
)

// Runtime is the coordinator side of the bridge: it accepts commands.
type Runtime interface {
	Send(cmd tauchat.ChatCommand) error
	Close()
}

// InitInfo is sent to every browser on WebSocket connection.
type InitInfo struct {
	SessionID         string
	Model             string
	Provider          string
	Models            []tauchat.ChatModelRef
	Providers         []string
	Commands          []tauchat.CommandRef
	Skills            []tauchat.SkillInfo
	ExtensionCommands []tauchat.ExtensionCommand
}

// Bridge fans out ChatEvent values to all connected WebSocket clients and
// forwards ChatCommand values received from clients back to the runtime.
//
// It creates its own "web" client on the event bus and implements the Web UI
// side of the command/event boundary without interpreting either direction.
type Bridge struct {
	runtime   Runtime
	bus       *eventbus.Bus
	client    *eventbus.Client
	sub       *eventbus.Subscriber[tauchat.ChatEvent]
	skillsSub *eventbus.SubscriberFunc[skills.Event]

	mu       sync.RWMutex
	clients  map[*client]struct{}
	initData []byte
	// lastSnapshot is the most recent ChatSessionSnapshotEvent seen on the bus,
	// already marshalled. It is replayed to each newly connected client so a
	// browser joining mid-session sees the existing conversation and settings.
	lastSnapshot []byte

	upgrader websocket.Upgrader
	logger   *slog.Logger

	done chan struct{}
	wg   sync.WaitGroup
}

// NewBridge creates a bridge attached to the given runtime and event bus.
func NewBridge(runtime Runtime, bus *eventbus.Bus, init InitInfo, logger *slog.Logger) (*Bridge, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if bus == nil {
		return nil, errors.New("event bus is required")
	}

	initData, err := MarshalInit(init.SessionID, init.Model, init.Provider, init.Models, init.Providers, init.Commands, init.Skills, init.ExtensionCommands)
	if err != nil {
		return nil, fmt.Errorf("marshal init message: %w", err)
	}

	busClient := bus.Client("web")
	sub := eventbus.Subscribe[tauchat.ChatEvent](busClient)

	b := &Bridge{
		runtime:  runtime,
		bus:      bus,
		client:   busClient,
		sub:      sub,
		initData: initData,
		clients:  make(map[*client]struct{}),
		logger:   logger,
		done:     make(chan struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}

	// Subscribe to skills events after b is initialised so the callback
	// can safely reference the bridge instance.
	b.skillsSub = eventbus.SubscribeFunc(busClient, func(evt skills.Event) {
		skillsList := make([]tauchat.SkillInfo, 0, len(evt.ActiveSkills))
		for _, s := range evt.ActiveSkills {
			skillsList = append(skillsList, tauchat.SkillInfo{
				Name:        s.Name,
				Description: s.Description,
				Scope:       string(s.Scope),
			})
		}
		b.broadcastEvent(tauchat.SkillsChangedEvent{Skills: skillsList})
	})

	b.wg.Add(1)
	go b.broadcastLoop()

	return b, nil
}

// ClientCount returns the number of connected browser clients.
func (b *Bridge) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// UpgradeHTTP handles an HTTP request and upgrades it to a WebSocket
// connection. It blocks until the connection closes.
func (b *Bridge) UpgradeHTTP(w http.ResponseWriter, r *http.Request) error {
	conn, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("upgrade websocket: %w", err)
	}
	c := b.addClient(conn)
	defer b.removeClient(c)

	b.mu.RLock()
	init := b.initData
	snapshot := b.lastSnapshot
	b.mu.RUnlock()
	if len(init) > 0 {
		_ = c.write(websocket.TextMessage, init)
	}
	// Replay the latest session snapshot so this client renders existing history
	// and current settings without waiting for the next turn.
	if len(snapshot) > 0 {
		_ = c.write(websocket.TextMessage, snapshot)
	}

	return c.readLoop()
}

// Close shuts down the bridge, disconnects all clients, and unsubscribes from
// the event bus. Safe to call multiple times.
func (b *Bridge) Close() error {
	close(b.done)

	b.mu.Lock()
	clients := make([]*client, 0, len(b.clients))
	for c := range b.clients {
		clients = append(clients, c)
	}
	b.clients = make(map[*client]struct{})
	b.mu.Unlock()

	for _, c := range clients {
		c.close()
	}

	if b.sub != nil {
		b.sub.Close()
	}
	if b.skillsSub != nil {
		b.skillsSub.Close()
	}
	if b.client != nil {
		b.client.Close()
	}

	b.wg.Wait()
	return nil
}

func (b *Bridge) addClient(conn *websocket.Conn) *client {
	c := &client{
		bridge: b,
		conn:   conn,
		send:   make(chan []byte, 64),
	}
	b.mu.Lock()
	b.clients[c] = struct{}{}
	b.mu.Unlock()

	b.wg.Add(1)
	go c.writeLoop()

	b.logger.Info("web client connected", "clients", b.ClientCount())
	return c
}

func (b *Bridge) removeClient(c *client) {
	b.mu.Lock()
	delete(b.clients, c)
	b.mu.Unlock()
	c.close()
	b.logger.Info("web client disconnected", "clients", b.ClientCount())
}

func (b *Bridge) broadcastLoop() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		case ev, ok := <-b.sub.Events():
			if !ok {
				return
			}
			b.broadcastEvent(ev)
		}
	}
}

func (b *Bridge) broadcastEvent(ev tauchat.ChatEvent) {
	data, err := MarshalEvent(ev)
	if err != nil {
		b.logger.Warn("marshal chat event", "err", err)
		return
	}

	// Cache session snapshots for replay to clients that connect later.
	if _, ok := ev.(tauchat.ChatSessionSnapshotEvent); ok {
		b.mu.Lock()
		b.lastSnapshot = data
		b.mu.Unlock()
	}

	b.mu.RLock()
	clients := make([]*client, 0, len(b.clients))
	for c := range b.clients {
		clients = append(clients, c)
	}
	b.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- data:
		default:
			// Slow client: drop message rather than block the bus pump.
			b.logger.Warn("slow web client, dropping event")
		}
	}
}

type client struct {
	bridge    *Bridge
	conn      *websocket.Conn
	send      chan []byte
	closeOnce sync.Once
}

func (c *client) readLoop() error {
	defer func() {
		c.bridge.removeClient(c)
	}()

	if err := c.conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return err
	}
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			if errors.Is(err, websocket.ErrBadHandshake) {
				return err
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.bridge.logger.Warn("websocket read error", "err", err)
			}
			return nil
		}
		if messageType != websocket.TextMessage {
			continue
		}

		cmd, err := UnmarshalCommand(data)
		if err != nil {
			c.bridge.logger.Warn("unmarshal websocket command", "err", err)
			_ = c.write(websocket.TextMessage, []byte(`{"type":"error","payload":"invalid command"}`))
			continue
		}

		if err := c.bridge.runtime.Send(cmd); err != nil {
			c.bridge.logger.Warn("forward command to runtime", "err", err)
			_ = c.write(websocket.TextMessage, fmt.Appendf(nil, `{"type":"error","payload":%q}`, err.Error()))
		}
	}
}

func (c *client) writeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.bridge.wg.Done()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.bridge.done:
			return
		}
	}
}

func (c *client) write(messageType int, data []byte) error {
	select {
	case c.send <- data:
		return nil
	default:
		return errors.New("client send buffer full")
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() { close(c.send) })
}
