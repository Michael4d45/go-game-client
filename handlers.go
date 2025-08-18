package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// handleSwap writes a swap_trigger.txt for Lua to pick up
func handleSwap(cfg *Config, state *ClientState, payload json.RawMessage) {
	var data struct {
		RoundNumber int    `json:"round_number"`
		GameName    string `json:"game_name"`
		SwapTime    int64  `json:"swap_time"` // epoch seconds
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleSwap: bad payload: %v", err)
		return
	}

	trigger := fmt.Sprintf("%d\n%s\n", data.SwapTime, data.GameName)
	if err := os.WriteFile("swap_trigger.txt", []byte(trigger), 0o644); err != nil {
		log.Printf("handleSwap: write trigger failed: %v", err)
		return
	}
	log.Printf("Swap scheduled for game %s at %d", data.GameName, data.SwapTime)

	// update state
	state.SetCurrentGame(data.GameName)

	// notify server when swap is complete (after swap time)
	go func() {
		now := time.Now().Unix()
		if data.SwapTime > now {
			time.Sleep(time.Duration(data.SwapTime-now) * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := notifySwapComplete(ctx, cfg, data.RoundNumber); err != nil {
			log.Printf("notifySwapComplete error: %v", err)
		}
	}()
}

func handleDownloadROM(cfg *Config, payload json.RawMessage) {
	var data struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleDownloadROM: bad payload: %v", err)
		return
	}
	dest := filepath.Join(cfg.RomDir, data.File)
	url := cfg.ServerURL + "/api/roms/" + data.File
	if err := DownloadFile(url, dest); err != nil {
		log.Printf("handleDownloadROM: download failed: %v", err)
	} else {
		log.Printf("Downloaded ROM: %s", data.File)
	}
}

func handleDownloadLua(cfg *Config, payload json.RawMessage) {
	var data struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleDownloadLua: bad payload: %v", err)
		return
	}
	dest := filepath.Join("scripts", data.Filename)
	url := cfg.ServerURL + "/api/scripts/latest"
	if err := DownloadFile(url, dest); err != nil {
		log.Printf("handleDownloadLua: download failed: %v", err)
	} else {
		log.Printf("Downloaded Lua script: %s", data.Filename)
	}
}

func handleServerMessage(payload json.RawMessage) {
	var data struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleServerMessage: bad payload: %v", err)
		return
	}
	log.Printf("[SERVER MESSAGE] %s", data.Message)
}

func handleKick(payload json.RawMessage) {
	var data struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(payload, &data)
	log.Printf("[KICKED] Reason: %s", data.Reason)
	os.Exit(1)
}

func handleStartGame(cfg *Config, state *ClientState, payload json.RawMessage) {
	var data struct {
		GameName string `json:"game_name"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handleStartGame: bad payload: %v", err)
		return
	}
	state.SetCurrentGame(data.GameName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sendGameStarted(ctx, cfg, data.GameName); err != nil {
		log.Printf("sendGameStarted error: %v", err)
	}
}

func handlePauseGame(cfg *Config, payload json.RawMessage) {
	log.Printf("Game paused (payload: %s)", string(payload))
	// Could write a pause_trigger.txt for Lua if needed
}

func handleSessionEnded(cfg *Config, state *ClientState, payload json.RawMessage) {
	log.Printf("Session ended (payload: %s)", string(payload))
	state.SetConnected(false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sendGameStopped(ctx, cfg); err != nil {
		log.Printf("sendGameStopped error: %v", err)
	}
}

func handlePrepareSwap(cfg *Config, state *ClientState, payload json.RawMessage) {
	var data struct {
		SavePath string `json:"save_path"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("handlePrepareSwap: bad payload: %v", err)
		return
	}
	// Write save_trigger.txt for Lua
	if err := os.WriteFile("save_trigger.txt", []byte(data.SavePath+"\n"), 0o644); err != nil {
		log.Printf("handlePrepareSwap: write trigger failed: %v", err)
	}
	log.Printf("Prepare swap: saving state to %s", data.SavePath)
}