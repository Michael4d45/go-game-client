package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type BizhawkIPC struct {
	addr   string
	mu     sync.RWMutex // protects conn
	wmu    sync.Mutex   // serializes writes
	conn   net.Conn
	closed chan struct{}
}

func NewBizhawkIPC(port int) *BizhawkIPC {
	return &BizhawkIPC{
		addr:   fmt.Sprintf("127.0.0.1:%d", port),
		closed: make(chan struct{}),
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

		// Background reader just to detect close
		go func(conn net.Conn) {
			buf := make([]byte, 1)
			for {
				conn.SetReadDeadline(time.Now().Add(10 * time.Second))
				_, err := conn.Read(buf)
				if err != nil {
					if err == io.EOF {
						log.Printf("[IPC] connection closed by peer")
					} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
						// keepalive via timeouts
						select {
						case <-ctx.Done():
							return
						default:
						}
						continue
					} else {
						log.Printf("[IPC] read error: %v", err)
					}
					b.mu.Lock()
					if b.conn == conn {
						_ = b.conn.Close()
						b.conn = nil
					}
					b.mu.Unlock()
					return
				}
			}
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

// Convenience helpers
func (b *BizhawkIPC) SendSwap(at int64, game string) {
	if err := b.SendLine(fmt.Sprintf("SWAP|%d|%s", at, game)); err != nil {
		log.Printf("[IPC] SWAP send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendStart(at int64, game string) {
	if err := b.SendLine(fmt.Sprintf("START|%d|%s", at, game)); err != nil {
		log.Printf("[IPC] START send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendSave(path string) {
	if err := b.SendLine(fmt.Sprintf("SAVE|%s", path)); err != nil {
		log.Printf("[IPC] SAVE send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendPause(at *int64) {
	line := "PAUSE"
	if at != nil {
		line = fmt.Sprintf("PAUSE|%d", *at)
	}
	if err := b.SendLine(line); err != nil {
		log.Printf("[IPC] PAUSE send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendResume(at *int64) {
	line := "RESUME"
	if at != nil {
		line = fmt.Sprintf("RESUME|%d", *at)
	}
	if err := b.SendLine(line); err != nil {
		log.Printf("[IPC] RESUME send failed: %v", err)
	}
}
func (b *BizhawkIPC) SendMessage(msg string) {
	if err := b.SendLine(fmt.Sprintf("MSG|%s", msg)); err != nil {
		log.Printf("[IPC] MSG send failed: %v", err)
	}
}
