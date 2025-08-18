package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// sendHeartbeat sends ping + current game to Laravel
func sendHeartbeat(cfg *Config, ping int, currentGame string) int {
	payload := map[string]any{
		"ping":         ping,
		"current_game": currentGame,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/heartbeat", bytes.NewReader(body))
	if err != nil {
		log.Printf("Heartbeat request error: %v", err)
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	start := time.Now()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Heartbeat send error: %v", err)
		return 0
	}
	defer resp.Body.Close()

	rtt := time.Since(start)
	newPing := int(rtt.Milliseconds())

	if resp.StatusCode != http.StatusOK {
		log.Printf("Heartbeat failed: %s", resp.Status)
	}

	return newPing
}

// sendReady notifies Laravel that the player is ready
func sendReady(cfg *Config) {
	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/ready", nil)
	if err != nil {
		log.Printf("Ready request error: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Ready send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Ready failed: %s", resp.Status)
	}
}

// notifySwapComplete tells Laravel the swap is done
func notifySwapComplete(cfg *Config, roundNumber int) {
	payload := map[string]any{
		"round_number": roundNumber,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/swap-complete", bytes.NewReader(body))
	if err != nil {
		log.Printf("Swap-complete request error: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Swap-complete send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Swap-complete failed: %s", resp.Status)
	}
}

// sendGameStarted tells Laravel which game started
func sendGameStarted(cfg *Config, gameName string) {
	payload := map[string]any{
		"current_game": gameName,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/game-started", bytes.NewReader(body))
	if err != nil {
		log.Printf("Game-started request error: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Game-started send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Game-started failed: %s", resp.Status)
	}
}

// sendGameStopped tells Laravel the game stopped
func sendGameStopped(cfg *Config) {
	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/game-stopped", nil)
	if err != nil {
		log.Printf("Game-stopped request error: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Game-stopped send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Game-stopped failed: %s", resp.Status)
	}
}
