package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type pendingCmd struct {
	line     string
	ch       chan string
	retries  int
	lastSent time.Time
}

type BizhawkIPC struct {
	addr   string
	mu     sync.RWMutex
	wmu    sync.Mutex
	conn   net.Conn
	closed chan struct{}

	cmdMu   sync.Mutex
	nextID  int
	pending map[int]*pendingCmd

	state *ClientState
}

func NewBizhawkIPC(port int, state *ClientState) *BizhawkIPC {
	return &BizhawkIPC{
		addr:    fmt.Sprintf("127.0.0.1:%d", port),
		closed:  make(chan struct{}),
		pending: make(map[int]*pendingCmd),
		state:   state,
	}
}

func (b *BizhawkIPC) Listen(ctx context.Context) error {
	ln, err := net.Listen("tcp", b.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", b.addr, err)
	}
	log.Printf("[IPC] Listening on %s", b.addr)

	defer func() {
		_ = ln.Close()
		b.mu.Lock()
		if b.conn != nil {
			_ = b.conn.Close()
			b.conn = nil
		}
		b.mu.Unlock()
		close(b.closed)
	}()

	// Start resend loop
	go b.startResender(ctx)

	for {
		ln.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
		c, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				continue
			}
			log.Printf("[IPC] accept error: %v", err)
			continue
		}
		log.Printf("[IPC] BizHawk connected from %s", c.RemoteAddr())
		b.mu.Lock()
		if b.conn != nil {
			_ = b.conn.Close()
		}
		b.conn = c
		b.mu.Unlock()

		// Background reader
		go func(conn net.Conn) {
			scanner := bufio.NewScanner(conn)
			for scanner.Scan() {
				line := scanner.Text()
				b.handleResponse(line)
			}
			if err := scanner.Err(); err != nil && err != io.EOF {
				log.Printf("[IPC] read error: %v", err)
			}
			b.mu.Lock()
			if b.conn == conn {
				_ = b.conn.Close()
				b.conn = nil
			}
			b.mu.Unlock()
		}(c)
	}
}

func (b *BizhawkIPC) SendLine(line string) error {
	b.mu.RLock()
	c := b.conn
	b.mu.RUnlock()
	if c == nil {
		return fmt.Errorf("bizhawk not connected")
	}
	b.wmu.Lock()
	defer b.wmu.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := io.WriteString(c, line+"\n")
	if err != nil {
		return fmt.Errorf("ipc write: %w", err)
	}
	return nil
}

// SendCommand sends a command with retries and waits for ACK/NACK.
func (b *BizhawkIPC) SendCommand(parts ...string) error {
	b.cmdMu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan string, 1)
	line := fmt.Sprintf("CMD|%d|%s", id, strings.Join(parts, "|"))
	cmd := &pendingCmd{
		line:     line,
		ch:       ch,
		retries:  3,
		lastSent: time.Now(),
	}
	b.pending[id] = cmd
	b.cmdMu.Unlock()

	if err := b.SendLine(line); err != nil {
		return err
	}

	select {
	case resp := <-ch:
		if strings.HasPrefix(resp, "ACK") {
			return nil
		}
		return fmt.Errorf("command %d failed: %s", id, resp)
	case <-time.After(5 * time.Second):
		return fmt.Errorf("command %d timeout", id)
	}
}

func (b *BizhawkIPC) handleResponse(line string) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 1 {
		return
	}
	switch parts[0] {
	case "ACK", "NACK":
		if len(parts) < 2 {
			return
		}
		id, _ := strconv.Atoi(parts[1])
		b.cmdMu.Lock()
		if cmd, ok := b.pending[id]; ok {
			delete(b.pending, id)
			cmd.ch <- parts[0]
		}
		b.cmdMu.Unlock()
	case "PING":
		if len(parts) >= 2 {
			_ = b.SendLine("PONG|" + parts[1])
		}
	case "HELLO":
		// Lua restarted, send SYNC
		go func() {
			if err := b.SendSync(); err != nil {
				log.Printf("[IPC] Failed to send SYNC: %v", err)
			} else {
				log.Printf("[IPC] Sent SYNC to BizHawk")
			}
		}()
	}
}

func (b *BizhawkIPC) startResender(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			b.cmdMu.Lock()
			for id, cmd := range b.pending {
				if now.Sub(cmd.lastSent) > 1*time.Second {
					if cmd.retries > 0 {
						log.Printf("[IPC] Resending command %d: %s", id, cmd.line)
						_ = b.SendLine(cmd.line)
						cmd.lastSent = now
						cmd.retries--
					} else {
						log.Printf("[IPC] Command %d failed after retries", id)
						delete(b.pending, id)
						cmd.ch <- "NACK|timeout"
					}
				}
			}
			b.cmdMu.Unlock()
		}
	}
}

// SendSync sends the current state to Lua after HELLO.
func (b *BizhawkIPC) SendSync() error {
	game := b.state.GetCurrentGame()
	stateAt := b.state.GetStateTime().Unix()
	state := b.state.GetState()
	return b.SendCommand("SYNC", game, state, fmt.Sprintf("%d", stateAt))
}

// Convenience helpers
func (b *BizhawkIPC) SendSwap(at int64, game string) {
	if err := b.SendCommand("SWAP", fmt.Sprintf("%d", at), game); err != nil {
		log.Printf("[IPC] SWAP send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendStart(at int64, game string) {
	if err := b.SendCommand("START", fmt.Sprintf("%d", at), game); err != nil {
		log.Printf("[IPC] START send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendSave(path string) {
	if err := b.SendCommand("SAVE", path); err != nil {
		log.Printf("[IPC] SAVE send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendPause(at *int64) {
	if at != nil {
		if err := b.SendCommand("PAUSE", fmt.Sprintf("%d", *at)); err != nil {
			log.Printf("[IPC] PAUSE send failed: %v", err)
		}
	} else {
		if err := b.SendCommand("PAUSE"); err != nil {
			log.Printf("[IPC] PAUSE send failed: %v", err)
		}
	}
}
func (b *BizhawkIPC) SendResume(at *int64) {
	if at != nil {
		if err := b.SendCommand("RESUME", fmt.Sprintf("%d", *at)); err != nil {
			log.Printf("[IPC] RESUME send failed: %v", err)
		}
	} else {
		if err := b.SendCommand("RESUME"); err != nil {
			log.Printf("[IPC] RESUME send failed: %v", err)
		}
	}
}
func (b *BizhawkIPC) SendMessage(msg string) {
	if err := b.SendCommand("MSG", msg); err != nil {
		log.Printf("[IPC] MSG send failed: %v", err)
	}
}
