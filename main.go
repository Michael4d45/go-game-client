package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

var verbose bool
var logFile *os.File

func initLogging() error {
	var err error
	logFile, err = os.OpenFile("client.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
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
	if err := state.LoadFromFile("runtime_state.json"); err == nil {
		log.Println("Loaded runtime state")
	} else {
		log.Printf("No previous runtime state: %v", err)
	}

	if err := Bootstrap(cfg); err != nil {
		log.Fatalf("Bootstrap failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := NewAPI(cfg)

	// Start IPC listener for BizHawk Lua
	ipc := NewBizhawkIPC(cfg.BizhawkIPCPort)
	go func() {
		if err := ipc.Listen(ctx); err != nil && ctx.Err() == nil {
			log.Printf("IPC listener exited with error: %v", err)
		}
	}()

	// Heartbeat loop
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, err := api.Heartbeat(ctx, state)
				if err != nil {
					log.Printf("Heartbeat error: %v", err)
				} else {
					if err := state.SaveToFile("runtime_state.json"); err != nil {
						log.Println("Runtime state save failed")
					}
				}
			}
		}
	}()

	// Watchdog
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

	// Pusher client
	pc := NewPusherClient(cfg, state, api, ipc)
	go func() {
		if err := pc.ConnectAndListen(ctx); err != nil {
			log.Fatalf("Pusher client exited with error: %v", err)
		}
	}()

	// Store the cmd object to be able to kill it later
	var bizhawkCmd *exec.Cmd
	bizhawkCmd, err = LaunchBizHawk(cfg)
	if err != nil {
		log.Fatalf("Failed to launch BizHawk: %v", err)
	}

	// This defer will ensure BizHawk is killed if the Go program exits
	// for any reason (e.g., Ctrl+C, or a fatal error elsewhere).
	defer func() {
		if bizhawkCmd != nil && bizhawkCmd.Process != nil {
			log.Println("Attempting to terminate BizHawk process...")
			// Use Process.Kill() for a forceful termination.
			// For Windows, Terminate() is often preferred first for a graceful shutdown.
			// However, Kill() is more robust for ensuring the process actually stops.
			if err := bizhawkCmd.Process.Kill(); err != nil {
				log.Printf("Failed to terminate BizHawk process: %v", err)
			} else {
				log.Println("BizHawk process terminated.")
			}
		}
	}()

	// Watch BizHawk process
	go func() {
		err := bizhawkCmd.Wait()
		if err != nil {
			log.Printf("BizHawk exited with error: %v", err)
		} else {
			log.Println("BizHawk exited normally")
		}
		stop() // Cancel main context, leading to main function shutdown
	}()

	if err := api.Ready(ctx, state); err != nil {
		log.Fatalf("ready error: %v", err)
	} else {
		startAt := state.GetStartTime()
		if startAt.IsZero() {
			log.Printf("Ready confirmed. Game: %q, StartAt: (none)", state.GetCurrentGame())
		} else {
			log.Printf("Ready confirmed. Game: %q, StartAt: %s",
				state.GetCurrentGame(),
				startAt.Format(time.RFC3339),
			)
		}
	}

	<-ctx.Done() // Wait for main context to be cancelled (e.g., Ctrl+C or BizHawk exit)
	log.Println("Shutdown requested; saving runtime state...")

	if err := state.SaveToFile("runtime_state.json"); err != nil {
		log.Printf("Failed to save runtime state: %v", err)
	} else {
		log.Println("Runtime state saved")
	}

	log.Println("Client exiting")
	time.Sleep(200 * time.Millisecond) // Give a small buffer for logs to flush
}
