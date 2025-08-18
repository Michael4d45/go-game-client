package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

var verbose bool

func initLogging() {
	logFile, err := os.OpenFile(
		"client.log",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0666,
	)
	if err != nil {
		fmt.Println("Failed to open log file:", err)
		os.Exit(1)
	}

	if verbose {
		// Log to both file and console
		mw := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(mw)
	} else {
		// Log only to file
		log.SetOutput(logFile)
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func main() {
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging to console")
	flag.Parse()

	initLogging()
	log.Println("=== Game Client Starting ===")

	cfg, err := LoadOrCreateConfig("config.json")
	if err != nil {
		log.Fatalf("Config load/create failed: %v", err)
	}

	if err := Bootstrap(cfg); err != nil {
		log.Fatalf("Bootstrap failed: %v", err)
	}

	// Send ready signal
	sendReady(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start heartbeat loop
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		ping := 0
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ping = sendHeartbeat(cfg, ping, "unknown")
			}
		}
	}()

	// Connect to Reverb
	pc := NewPusherClient(cfg)

	if err := pc.ConnectAndListen(ctx); err != nil {
		log.Fatalf("Pusher connection failed: %v", err)
	}

	<-ctx.Done()
}