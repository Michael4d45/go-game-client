package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var verbose bool
var logFile *os.File

func initLogging() error {
	var err error
	logFile, err = os.OpenFile(
		"client.log",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o666,
	)
	if err != nil {
		return err
	}

	if verbose {
		mw := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(mw)
	} else {
		log.SetOutput(logFile)
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	return nil
}

func main() {
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging to console")
	flag.Parse()

	if err := initLogging(); err != nil {
		fmt.Println("Failed to open log file:", err)
		os.Exit(1)
	}
	defer func() {
		if logFile != nil {
			_ = logFile.Close()
		}
	}()

	log.Println("=== Game Client Starting ===")

	cfg, err := LoadOrCreateConfig("config.json")
	if err != nil {
		log.Fatalf("Config load/create failed: %v", err)
	}

	state := NewClientState()
	// try to restore last runtime snapshot if present (non-fatal)
	if err := state.LoadFromFile("runtime_state.json"); err == nil {
		log.Println("Loaded runtime state")
	} else {
		log.Printf("No previous runtime state: %v", err)
	}

	// Bootstrap (downloads etc). This will prompt for input if needed.
	if err := Bootstrap(cfg); err != nil {
		log.Fatalf("Bootstrap failed: %v", err)
	}

	// Setup cancellable context tied to OS signals
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// Notify server that this client is ready (best-effort)
	if err := sendReady(ctx, cfg, state); err != nil {
		log.Printf("sendReady error: %v", err)
	} else {
		state.SetReady(true)
	}

	// Heartbeat loop: only updates LastHeartbeat on success
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newPing, err := sendHeartbeat(ctx, cfg, state)
				if err != nil {
					log.Printf("Heartbeat error: %v", err)
					// don't update LastHeartbeat here
				} else {
					state.SetPing(newPing) // this updates LastHeartbeat
				}
			}
		}
	}()

	// Watchdog: decides if we are "connected"
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snap := state.Snapshot()
				if time.Since(snap.LastHeartbeat) > 15*time.Second {
					if snap.Connected {
						log.Println("No recent heartbeat observed; marking disconnected")
						state.SetConnected(false)
					}
				} else {
					if !snap.Connected {
						log.Println("Heartbeat restored; marking connected")
						state.SetConnected(true)
					}
				}
			}
		}
	}()

	// Start Pusher (websocket) client in background
	pc := NewPusherClient(cfg, state)
	go func() {
		if err := pc.ConnectAndListen(ctx); err != nil {
			log.Printf("Pusher client exited with error: %v", err)
			// do not force exit; let main handle shutdown
		}
	}()

	// Wait for termination signal
	<-ctx.Done()
	log.Println("Shutdown requested; saving runtime state...")

	if err := state.SaveToFile("runtime_state.json"); err != nil {
		log.Printf("Failed to save runtime state: %v", err)
	} else {
		log.Println("Runtime state saved")
	}

	log.Println("Client exiting")
	// give goroutines a small grace period to finish logs
	time.Sleep(200 * time.Millisecond)
}
