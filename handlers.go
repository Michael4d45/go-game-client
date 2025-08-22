package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Handlers contains methods for processing events received from the server.
type Handlers struct {
	api   *API
	cfg   *Config
	state *ClientState
	ipc   *BizhawkIPC
}

func NewHandlers(
	api *API,
	cfg *Config,
	state *ClientState,
	ipc *BizhawkIPC,
) *Handlers {
	return &Handlers{
		api:   api,
		cfg:   cfg,
		state: state,
		ipc:   ipc,
	}
}

func (h *Handlers) Swap(payload json.RawMessage) {
	var data struct {
		RoundNumber int    `json:"round_number"`
		SwapTime    int64  `json:"swap_at"`
		GameName    string `json:"new_game"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleSwap: bad payload: %v", err)
		return
	}
	if data.GameName == "" || data.SwapTime == 0 {
		log.Printf("handleSwap: missing fields: %+v", data)
		return
	}

	h.ipc.SendSwap(data.SwapTime, data.GameName)
	h.state.SetCurrentGame(data.GameName)
	log.Printf("Swap scheduled for game %s at %d", data.GameName, data.SwapTime)

	go func(round int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.api.SwapComplete(ctx, round); err != nil {
			log.Printf("swap-complete error: %v", err)
		}
	}(data.RoundNumber)
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
	if err := DownloadFile(httpClient, url, dest); err != nil {
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
	if err := DownloadFile(httpClient, url, dest); err != nil {
		log.Printf("handleDownloadLua: download failed: %v", err)
	} else {
		log.Printf("Downloaded Lua script: %s", data.Filename)
	}
}

func (h *Handlers) ServerMessage(payload json.RawMessage) {
	var data struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleServerMessage: bad payload: %v", err)
		return
	}
	log.Printf("[SERVER MESSAGE] %s", data.Text)
	h.ipc.SendMessage(data.Text)
}

func (h *Handlers) Kick(payload json.RawMessage) {
	var data struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(payload, &data)
	log.Printf("[KICKED] Reason: %s", data.Reason)

	h.ipc.SendMessage("Kicked: " + data.Reason)
	h.ipc.SendPause(nil)
	os.Exit(1)
}

func (h *Handlers) ChnageGameState(payload json.RawMessage) {
	var data struct {
		State   string `json:"state"`
		StateAt int64  `json:"state_at"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleChnageGameState: bad payload: %v", err)
		return
	}
	if data.StateAt == 0 {
		log.Printf("handleChnageGameState: missing or zero start_time")
		return
	}

	stateTime := time.Unix(data.StateAt, 0)
	log.Printf(
		"Scheduled %s at %s (%d)",
		data.State,
		stateTime.Format(time.RFC3339),
		data.StateAt,
	)

	h.state.SetState(stateTime, data.State)
	h.ipc.SendSync()
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

func (h *Handlers) ClearSaves(_payload json.RawMessage) {
	saveDir := h.cfg.SaveDir
	entries, err := os.ReadDir(saveDir)
	if err != nil {
		log.Printf("Error reading save directory '%s': %v", saveDir, err)
		return
	}

	for _, entry := range entries {
		path := filepath.Join(saveDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			log.Printf("Error deleting %s: %v", path, err)
		}
	}
	log.Println("All saves cleared.")
}

type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (h *Handlers) handleRawEvent(raw json.RawMessage) {
	// The pusher library wraps the event data in a JSON string,
	// so we need to unmarshal it twice. First to get the string,
	// then to get the actual message object.
	var eventData string
	if err := json.Unmarshal(raw, &eventData); err != nil {
		log.Printf("[ERROR] Unmarshal outer Pusher event: %v", err)
		return
	}

	var msg WSMessage
	if err := json.Unmarshal([]byte(eventData), &msg); err != nil {
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
	case "change_game_state":
		h.ChnageGameState(msg.Payload)
	case "session_ended":
		h.SessionEnded(msg.Payload)
	case "prepare_swap":
		h.PrepareSwap(msg.Payload)
	case "clear_saves":
		h.ClearSaves(msg.Payload)
	default:
		log.Printf("[WARN] Unknown event type: %s", msg.Type)
	}
}
