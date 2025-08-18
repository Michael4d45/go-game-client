package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func handleSwap(cfg *Config, payload json.RawMessage) {
	var data struct {
		RoundNumber int    `json:"round_number"`
		SwapAt      string `json:"swap_at"` // ISO8601 UTC
		NewGame     string `json:"new_game"`
		SaveURL     string `json:"save_url"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Println("Error parsing swap:", err)
		return
	}

	log.Printf("Swap command: round %d, new game: %s at %s", data.RoundNumber, data.NewGame, data.SwapAt)

	// Parse swap_at into epoch seconds
	swapTime, err := time.Parse(time.RFC3339, data.SwapAt)
	if err != nil {
		log.Println("Error parsing swap_at:", err)
		return
	}
	swapEpoch := swapTime.Unix()

	// --- STEP 1: Download ROM if needed ---
	romPath := filepath.Join("roms", data.NewGame)
	if _, err := os.Stat(romPath); os.IsNotExist(err) {
		log.Println("Downloading ROM:", data.NewGame)
		DownloadFile(fmt.Sprintf("%s/api/roms/%s", cfg.ServerURL, data.NewGame), romPath)
	}

	// --- STEP 2: Download save state if provided ---
	if data.SaveURL != "" {
		savePath := filepath.Join("saves", fmt.Sprintf("%s.state", data.NewGame))
		log.Println("Downloading save state:", savePath)
		DownloadFile(data.SaveURL, savePath)
	}

	// --- STEP 3: Write swap trigger with timestamp + game name ---
	swapFile := fmt.Sprintf("%d\n%s", swapEpoch, data.NewGame)
	os.WriteFile("swap_trigger.txt", []byte(swapFile), 0644)

	log.Printf("Swap scheduled for %s (epoch: %d)", data.SwapAt, swapEpoch)
	notifySwapComplete(cfg, data.RoundNumber)
}

func handleDownloadROM(payload json.RawMessage) {
	var data struct {
		RomName string `json:"rom_name"`
		RomURL  string `json:"rom_url"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Println("Error parsing download_rom:", err)
		return
	}
	log.Printf("Downloading ROM: %s", data.RomName)
	DownloadFile(data.RomURL, filepath.Join("roms", data.RomName))
}

func handleDownloadLua(payload json.RawMessage) {
	var data struct {
		LuaVersion string `json:"lua_version"`
		LuaURL     string `json:"lua_url"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Println("Error parsing download_lua:", err)
		return
	}
	log.Printf("Downloading Lua script v%s", data.LuaVersion)
	DownloadFile(data.LuaURL, filepath.Join("scripts", fmt.Sprintf("swap_v%s.lua", data.LuaVersion)))
}

func handleServerMessage(payload json.RawMessage) {
	var data struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Println("Error parsing message:", err)
		return
	}
	log.Println("[SERVER MESSAGE]", data.Text)
}

func handleKick(payload json.RawMessage) {
	var data struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Println("Error parsing kick:", err)
		return
	}
	log.Printf("Kicked from server: %s", data.Reason)
	os.Exit(0)
}

func handleStartGame(cfg *Config, payload json.RawMessage) {
	log.Printf("[EVENT] Start game")

	// TODO: Launch BizHawk with this game immediately
	romPath := filepath.Join(cfg.RomDir, cfg.SessionName)
	if _, err := os.Stat(romPath); os.IsNotExist(err) {
		log.Println("Downloading ROM:", cfg.SessionName)
		DownloadFile(fmt.Sprintf("%s/api/roms/%s", cfg.ServerURL, cfg.SessionName), romPath)
	}
	// Optionally trigger Lua to load immediately
}

func handlePauseGame(cfg *Config, payload json.RawMessage) {
	log.Println("[EVENT] Pause game")
	// TODO: Implement pause logic (e.g., send pause trigger to Lua)
}

func handleSessionEnded(cfg *Config, payload json.RawMessage) {
	log.Println("[EVENT] Session ended by server")
	// TODO: Clean up, stop BizHawk, etc.
	os.Exit(0)
}

func handlePrepareSwap(cfg *Config, payload json.RawMessage) {
	var data struct {
		RoundNumber int    `json:"round_number"`
		UploadBy    string `json:"upload_by"` // ISO8601 UTC
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Println("Error parsing prepare_swap:", err)
		return
	}
	log.Printf("[EVENT] Prepare swap for round %d, upload by %s", data.RoundNumber, data.UploadBy)

	// Trigger Lua to save state
	savePath := filepath.Join(cfg.SaveDir, "current.state")
	os.WriteFile("save_trigger.txt", []byte(savePath), 0644)

	// Wait for Lua to write the file
	waitForFile(savePath, 5)

	// Upload to server
	uploadURL := fmt.Sprintf("%s/api/upload-save", cfg.ServerURL)
	if err := UploadFile(uploadURL, savePath, cfg.PlayerName, cfg.SessionName); err != nil {
		log.Println("Error uploading save state:", err)
	} else {
		log.Println("Save state uploaded successfully")
	}
}
