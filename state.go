package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// StateEventType enumerates the event types emitted by ClientState.
type StateEventType string

const (
	EventPingUpdated        StateEventType = "ping_updated"
	EventConnected          StateEventType = "connected"
	EventDisconnected       StateEventType = "disconnected"
	EventCurrentGameChanged StateEventType = "current_game_changed"
	EventReadyChanged       StateEventType = "ready_changed"
	EventStateChanged       StateEventType = "state_changed"
	EventStateTimeChanged   StateEventType = "state_time_changed"
)

// StateEvent is a small event sent to subscribers.
type StateEvent struct {
	Type StateEventType `json:"type"`
	Old  interface{}    `json:"old,omitempty"`
	New  interface{}    `json:"new,omitempty"`
	When time.Time      `json:"when"`
}

// ClientStateSnapshot is a serializable snapshot of important fields.
type ClientStateSnapshot struct {
	Ping          int       `json:"ping"`
	Connected     bool      `json:"connected"`
	CurrentGame   string    `json:"current_game"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Ready         bool      `json:"ready"`
	LastError     string    `json:"last_error,omitempty"`
	StateAt       time.Time `json:"state_at"`
	State         string    `json:"state"`
}

// ClientState holds ephemeral runtime state (concurrency safe).
type ClientState struct {
	mu sync.RWMutex

	ping          int
	connected     bool
	currentGame   string
	lastHeartbeat time.Time
	ready         bool
	lastError     string
	stateAt       time.Time
	state         string

	subMu sync.Mutex
	subs  map[chan StateEvent]struct{}
}

// NewClientState constructs an empty ClientState.
func NewClientState() *ClientState {
	return &ClientState{
		subs: make(map[chan StateEvent]struct{}),
	}
}

// Subscribe returns a buffered channel that receives StateEvent.
// Unsubscribe must be called to stop and close the channel.
func (s *ClientState) Subscribe(buf int) chan StateEvent {
	if buf <= 0 {
		buf = 4
	}
	ch := make(chan StateEvent, buf)
	s.subMu.Lock()
	s.subs[ch] = struct{}{}
	s.subMu.Unlock()
	return ch
}

// Unsubscribe closes and removes the subscription channel.
func (s *ClientState) Unsubscribe(ch chan StateEvent) {
	s.subMu.Lock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
	s.subMu.Unlock()
}

func (s *ClientState) notify(ev StateEvent) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
			// drop on slow subscriber
		}
	}
}

// SetPing updates ping and lastHeartbeat.
func (s *ClientState) SetPing(p int) {
	s.mu.Lock()
	old := s.ping
	s.ping = p
	s.lastHeartbeat = time.Now()
	s.mu.Unlock()

	s.notify(StateEvent{Type: EventPingUpdated, Old: old, New: p, When: time.Now()})
}

// SetConnected sets connection state and emits an event.
func (s *ClientState) SetConnected(c bool) {
	s.mu.Lock()
	old := s.connected
	s.connected = c
	s.mu.Unlock()

	typ := EventDisconnected
	if c {
		typ = EventConnected
	}
	s.notify(StateEvent{Type: typ, Old: old, New: c, When: time.Now()})
}

// SetCurrentGame updates current game and emits event.
func (s *ClientState) SetCurrentGame(name string) {
	s.mu.Lock()
	old := s.currentGame
	s.currentGame = name
	s.mu.Unlock()

	s.notify(StateEvent{
		Type: EventCurrentGameChanged,
		Old:  old,
		New:  name,
		When: time.Now(),
	})
}

// SetReady sets ready flag.
func (s *ClientState) SetReady(r bool) {
	s.mu.Lock()
	old := s.ready
	s.ready = r
	s.mu.Unlock()

	s.notify(StateEvent{
		Type: EventReadyChanged,
		Old:  old,
		New:  r,
		When: time.Now(),
	})
}

// SetStateTime sets the state time and emits event.
func (s *ClientState) SetState(t time.Time, state string) {
	s.mu.Lock()
	oldStateAt := s.stateAt
	s.stateAt = t
	oldState := s.state
	s.state = state
	s.mu.Unlock()

	s.notify(StateEvent{
		Type: EventStateTimeChanged,
		Old:  oldStateAt,
		New:  t,
		When: time.Now(),
	})

	s.notify(StateEvent{
		Type: EventStateChanged,
		Old:  oldState,
		New:  t,
		When: time.Now(),
	})
}

// Snapshot returns a copy of important runtime info.
func (s *ClientState) Snapshot() ClientStateSnapshot {
	s.mu.RLock()
	snap := ClientStateSnapshot{
		Ping:          s.ping,
		Connected:     s.connected,
		CurrentGame:   s.currentGame,
		LastHeartbeat: s.lastHeartbeat,
		Ready:         s.ready,
		LastError:     s.lastError,
		StateAt:       s.stateAt,
		State:         s.state,
	}
	s.mu.RUnlock()
	return snap
}

// SaveToFile persists a snapshot to disk (atomic-ish).
func (s *ClientState) SaveToFile(path string) error {
	snap := s.Snapshot()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

// LoadFromFile restores from the saved snapshot (best-effort).
func (s *ClientState) LoadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var snap ClientStateSnapshot
	if err := json.NewDecoder(f).Decode(&snap); err != nil {
		return err
	}
	s.mu.Lock()
	s.ping = snap.Ping
	s.connected = snap.Connected
	s.currentGame = snap.CurrentGame
	s.lastHeartbeat = snap.LastHeartbeat
	s.ready = snap.Ready
	s.lastError = snap.LastError
	s.stateAt = snap.StateAt
	s.state = snap.State
	s.mu.Unlock()
	return nil
}

// Convenience getters
func (s *ClientState) GetCurrentGame() string {
	s.mu.RLock()
	g := s.currentGame
	s.mu.RUnlock()
	return g
}

func (s *ClientState) GetPing() int {
	s.mu.RLock()
	p := s.ping
	s.mu.RUnlock()
	return p
}

func (s *ClientState) GetStateTime() time.Time {
	s.mu.RLock()
	t := s.stateAt
	s.mu.RUnlock()
	return t
}

func (s *ClientState) GetState() string {
	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()
	return state
}
