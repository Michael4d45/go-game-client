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

// App encapsulates all the components of the application.
type App struct {
	cfg        *Config
	state      *ClientState
	api        *API
	ipc        *BizhawkIPC
	handlers   *Handlers
	pusher     *PusherClient
	bizhawkCmd *exec.Cmd
	logFile    *os.File
}

// NewApp creates and initializes a new application instance.
func NewApp() (*App, error) {
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging to console")
	flag.Parse()

	app := &App{}
	var err error

	app.logFile, err = initLogging()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logging: %w", err)
	}

	log.Println("=== Game Client Starting ===")

	app.cfg, err = LoadOrCreateConfig("config.json")
	if err != nil {
		return nil, fmt.Errorf("config load/create failed: %w", err)
	}

	app.state = NewClientState()
	if err := app.state.LoadFromFile("runtime_state.json"); err == nil {
		log.Println("Loaded runtime state")
	} else {
		log.Printf("No previous runtime state: %v", err)
	}

	return app, nil
}

// Run starts the application and blocks until a shutdown signal is received.
func (a *App) Run() error {
	if err := Bootstrap(a.cfg); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	a.api = NewAPI(a.cfg)

	// Start IPC listener for BizHawk Lua (now requires state for SYNC)
	a.ipc = NewBizhawkIPC(a.cfg.BizhawkIPCPort, a.state)
	go func() {
		if err := a.ipc.Listen(ctx); err != nil && ctx.Err() == nil {
			log.Printf("IPC listener exited with error: %v", err)
		}
	}()

	// Heartbeat loop
	go a.startHeartbeatLoop(ctx)

	// Watchdog
	go a.startWatchdog(ctx)

	// Handlers and Pusher
	a.handlers = NewHandlers(a.api, a.cfg, a.state, a.ipc)
	a.pusher = NewPusherClient(a.cfg, a.state, a.handlers)
	go func() {
		if err := a.pusher.ConnectAndListen(ctx); err != nil && ctx.Err() == nil {
			log.Fatalf("Pusher client exited with error: %v", err)
		}
	}()

	// Launch BizHawk
	var err error
	a.bizhawkCmd, err = LaunchBizHawk(a.cfg)
	if err != nil {
		return fmt.Errorf("failed to launch BizHawk: %w", err)
	}
	go a.watchBizHawkProcess(stop)

	// Notify server we are ready
	if err := a.api.Ready(ctx, a.state); err != nil {
		return fmt.Errorf("ready error: %w", err)
	}
	a.handleInitialStartState()

	a.ipc.SendMessage("Welcome")

	<-ctx.Done()
	return a.Shutdown()
}

// Shutdown performs graceful shutdown of the application.
func (a *App) Shutdown() error {
	log.Println("Shutdown requested...")

	if a.bizhawkCmd != nil && a.bizhawkCmd.Process != nil {
		log.Println("Terminating BizHawk process...")
		if err := a.bizhawkCmd.Process.Kill(); err != nil {
			log.Printf("Failed to terminate BizHawk process: %v", err)
		} else {
			log.Println("BizHawk process terminated.")
		}
	}

	log.Println("Saving runtime state...")
	if err := a.state.SaveToFile("runtime_state.json"); err != nil {
		log.Printf("Failed to save runtime state: %v", err)
	} else {
		log.Println("Runtime state saved.")
	}

	log.Println("Client exiting.")
	if a.logFile != nil {
		_ = a.logFile.Close()
	}
	time.Sleep(200 * time.Millisecond) // Allow logs to flush
	return nil
}

func (a *App) startHeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.api.Heartbeat(ctx, a.state); err != nil {
				log.Printf("Heartbeat error: %v", err)
			} else {
				if err := a.state.SaveToFile("runtime_state.json"); err != nil {
					log.Printf("Runtime state save failed: %v", err)
				}
			}
		}
	}
}

func (a *App) startWatchdog(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := a.state.Snapshot()
			if time.Since(snap.LastHeartbeat) > 15*time.Second {
				if snap.Connected {
					log.Println("No recent heartbeat; marking disconnected")
					a.state.SetConnected(false)
				}
			} else {
				if !snap.Connected {
					log.Println("Heartbeat restored; marking connected")
					a.state.SetConnected(true)
				}
			}
		}
	}
}

func (a *App) watchBizHawkProcess(stop context.CancelFunc) {
	if err := a.bizhawkCmd.Wait(); err != nil {
		log.Printf("BizHawk exited with error: %v", err)
	} else {
		log.Println("BizHawk exited normally")
	}
	stop() // Trigger application shutdown
}

func (a *App) handleInitialStartState() {
	startAt := a.state.GetStartTime()
	game := a.state.GetCurrentGame()
	if startAt.IsZero() {
		log.Printf("Ready confirmed. Game: %q, StartAt: (not set)", game)
	} else {
		log.Printf(
			"Ready confirmed. Game: %q, StartAt: %s",
			game,
			startAt.Format(time.RFC3339),
		)
		// Give BizHawk a moment to load before sending the start command
		time.Sleep(3 * time.Second)
		a.handlers.SendStart()
	}
}

func initLogging() (*os.File, error) {
	logFile, err := os.OpenFile(
		"client.log",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o666,
	)
	if err != nil {
		return nil, err
	}
	if verbose {
		mw := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(mw)
	} else {
		log.SetOutput(logFile)
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	return logFile, nil
}

func main() {
	app, err := NewApp()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Initialization failed: %v\n", err)
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		log.Fatalf("Application run failed: %v", err)
	}
}
