package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
}

// sendHeartbeat posts a heartbeat and returns measured ping (ms).
func sendHeartbeat(ctx context.Context, cfg *Config, state *ClientState) (int, error) {
	payload := map[string]any{
		"ping":         state.GetPing(),
		"current_game": state.GetCurrentGame(),
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.ServerURL+"/api/heartbeat", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("heartbeat request error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("heartbeat send error: %w", err)
	}
	defer resp.Body.Close()

	rtt := time.Since(start)
	newPing := int(rtt.Milliseconds())

	if resp.StatusCode != http.StatusOK {
		log.Printf("Heartbeat failed: %s", resp.Status)
		// still return measured ping
		return newPing, fmt.Errorf("heartbeat status: %s", resp.Status)
	}

	// update state lastHeartbeat + ping
	state.SetPing(newPing)
	return newPing, nil
}

// sendReady notifies Laravel that the player is ready.
func sendReady(ctx context.Context, cfg *Config, state *ClientState) error {
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.ServerURL+"/api/ready", nil)
	if err != nil {
		return fmt.Errorf("ready request error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ready send error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ready failed: %s", resp.Status)
	}

	state.SetReady(true)
	return nil
}

// notifySwapComplete tells Laravel the swap is done
func notifySwapComplete(ctx context.Context, cfg *Config, roundNumber int) error {
	payload := map[string]any{
		"round_number": roundNumber,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		cfg.ServerURL+"/api/swap-complete",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("swap-complete request error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("swap-complete send error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("swap-complete failed: %s", resp.Status)
	}
	return nil
}

// sendGameStarted tells Laravel which game started
func sendGameStarted(ctx context.Context, cfg *Config, gameName string) error {
	payload := map[string]any{
		"current_game": gameName,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		cfg.ServerURL+"/api/game-started",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("game-started request error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("game-started send error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("game-started failed: %s", resp.Status)
	}
	return nil
}

// sendGameStopped tells Laravel the game stopped
func sendGameStopped(ctx context.Context, cfg *Config) error {
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		cfg.ServerURL+"/api/game-stopped",
		nil,
	)
	if err != nil {
		return fmt.Errorf("game-stopped request error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("game-stopped send error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("game-stopped failed: %s", resp.Status)
	}
	return nil
}
