package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

type Handlers struct {
	api   *API
	cfg   *Config
	state *ClientState
	ipc   *BizhawkIPC
}

func NewHandlers(api *API, cfg *Config, state *ClientState, ipc *BizhawkIPC) *Handlers {
	return &Handlers{
		api:   api,
		cfg:   cfg,
		state: state,
		ipc:   ipc,
	}
}

func (h *Handlers) Swap(payload json.RawMessage) {
	var data struct {
		RoundNumber int     `json:"round_number"`
		SwapTime    int64   `json:"swap_at"`
		GameName    string  `json:"new_game"`
		SaveURL     *string `json:"save_url"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleSwap: bad payload: %v", err)
		return
	}
	if data.GameName == "" || data.SwapTime == 0 {
		log.Printf("handleSwap: missing fields: %+v", data)
		return
	}

	// Notify BizHawk to swap at the timestamp
	h.ipc.SendSwap(data.SwapTime, data.GameName)
	h.state.SetCurrentGame(data.GameName)
	log.Printf("Swap scheduled for game %s at %d", data.GameName, data.SwapTime)

	// Notify server at/after swap time
	go func(round int, at int64) {
		swapAt := time.Unix(at, 0)
		if dur := time.Until(swapAt); dur > 0 {
			time.Sleep(dur)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.api.SwapComplete(ctx, round); err != nil {
			log.Printf("swap-complete error: %v", err)
		}
	}(data.RoundNumber, data.SwapTime)
}

func (h *Handlers) DownloadROM(payload json.RawMessage) {
	var data struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleDownloadROM: bad payload: %v", err)
		return
	}
	dest := filepath.Join(h.cfg.RomDir, data.File)
	url := h.cfg.ServerURL + "/api/roms/" + data.File
	if err := DownloadFile(url, dest); err != nil {
		log.Printf("handleDownloadROM: download failed: %v", err)
	} else {
		log.Printf("Downloaded ROM: %s", data.File)
	}
}

func (h *Handlers) DownloadLua(payload json.RawMessage) {
	var data struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleDownloadLua: bad payload: %v", err)
		return
	}
	dest := filepath.Join("scripts", data.Filename)
	url := h.cfg.ServerURL + "/api/scripts/latest"
	if err := DownloadFile(url, dest); err != nil {
		log.Printf("handleDownloadLua: download failed: %v", err)
	} else {
		log.Printf("Downloaded Lua script: %s", data.Filename)
	}
}

func (h *Handlers) ServerMessage(payload json.RawMessage) {
	var data struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleServerMessage: bad payload: %v", err)
		return
	}
	log.Printf("[SERVER MESSAGE] %s", data.Message)
	h.ipc.SendMessage(data.Message)
}

func (h *Handlers) Kick(payload json.RawMessage) {
	type kick struct {
		Reason string `json:"reason"`
	}
	var data kick
	_ = json.Unmarshal(payload, &data)
	log.Printf("[KICKED] Reason: %s", data.Reason)
	// Propagate to BizHawk as message and pause
	h.ipc.SendMessage("Kicked: " + data.Reason)
	h.ipc.SendPause(nil)
	// Exit process
	os.Exit(1)
}

// In handlers.go

func (h *Handlers) StartGame(payload json.RawMessage) {
	var data struct {
		StartTime int64 `json:"start_time"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleStartGame: bad payload: %v", err)
		return
	}
	if data.StartTime == 0 {
		log.Printf("handleStartGame: missing or zero start_time")
		return
	}

	gameName := h.state.GetCurrentGame()
	if gameName == "" {
		log.Printf("handleStartGame: no current game set in state")
		return
	}

	startTime := time.Unix(data.StartTime, 0)
	log.Printf("Scheduled START for game %q at %s (%d)",
		gameName,
		startTime.Format(time.RFC3339),
		data.StartTime,
	)

	h.state.SetStartTime(startTime)

	h.ipc.SendStart(data.StartTime, gameName)
}

func (h *Handlers) PauseGame(payload json.RawMessage) {
	// Optional: pause_at timestamp
	var data struct {
		At *int64 `json:"at,omitempty"`
	}
	_ = json.Unmarshal(payload, &data)
	h.ipc.SendPause(data.At)
	log.Printf("Game pause requested at: %v", data.At)
}

func (h *Handlers) SessionEnded(payload json.RawMessage) {
	log.Printf("Session ended (payload: %s)", string(payload))
	h.state.SetConnected(false)
	h.ipc.SendMessage("Session ended")
	h.ipc.SendPause(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.api.GameStopped(ctx); err != nil {
		log.Printf("game-stopped error: %v", err)
	}
}

func (h *Handlers) PrepareSwap(payload json.RawMessage) {
	var data struct {
		SavePath string `json:"save_path"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handlePrepareSwap: bad payload: %v", err)
		return
	}
	h.ipc.SendSave(data.SavePath)
	log.Printf("Prepare swap: saving state to %s", data.SavePath)
}

type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (h *Handlers) handleRawEvent(raw json.RawMessage) {
	log.Printf("[DEBUG] Raw event from Pusher: %s", string(raw))

	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		log.Printf("[ERROR] Unmarshal outer event: %v", err)
		return
	}
	log.Printf("[DEBUG] Outer JSON string: %s", inner)

	var msg WSMessage
	if err := json.Unmarshal([]byte(inner), &msg); err != nil {
		log.Printf("[ERROR] Unmarshal inner WSMessage: %v", err)
		return
	}

	switch msg.Type {
	case "swap":
		h.Swap(msg.Payload)
	case "download_rom":
		h.DownloadROM(msg.Payload)
	case "download_lua":
		h.DownloadLua(msg.Payload)
	case "message":
		h.ServerMessage(msg.Payload)
	case "kick":
		h.Kick(msg.Payload)
	case "start_game":
		h.StartGame(msg.Payload)
	case "pause_game":
		h.PauseGame(msg.Payload)
	case "session_ended":
		h.SessionEnded(msg.Payload)
	case "prepare_swap":
		h.PrepareSwap(msg.Payload)
	default:
		log.Printf("[WARN] Unknown event type: %s", msg.Type)
	}
}
