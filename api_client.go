package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
}

// API centralizes all server HTTP calls.
type API struct {
	baseURL string
	bearer  string
	client  *http.Client
}

// NewAPI constructs an API helper for the provided config.
func NewAPI(cfg *Config) *API {
	base := strings.TrimRight(cfg.ServerURL, "/")
	return &API{
		baseURL: base,
		bearer:  cfg.BearerToken,
		client:  httpClient,
	}
}

func (a *API) newRequest(ctx context.Context, method, path string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("%s request error: %w", path, err)
	}
	if a.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+a.bearer)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (a *API) do(req *http.Request) (*http.Response, time.Duration, error) {
	start := time.Now()
	resp, err := a.client.Do(req)
	rtt := time.Since(start)
	return resp, rtt, err
}

// Heartbeat posts a heartbeat and returns measured ping (ms). On success the
// client's state is updated with the measured ping.
func (a *API) Heartbeat(ctx context.Context, state *ClientState) (int, error) {
	payload := map[string]any{
		"ping":         state.GetPing(),
		"current_game": state.GetCurrentGame(),
	}
	req, err := a.newRequest(ctx, http.MethodPost, "/api/heartbeat", payload)
	if err != nil {
		return 0, err
	}
	resp, rtt, err := a.do(req)
	if err != nil {
		return 0, fmt.Errorf("heartbeat send error: %w", err)
	}
	newPing := int(rtt.Milliseconds())

	// Check status and close body (we don't expect useful JSON here).
	if resp == nil {
		return newPing, fmt.Errorf("nil heartbeat response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return newPing, fmt.Errorf("heartbeat status: %s", resp.Status)
	}

	state.SetPing(newPing)
	return newPing, nil
}

// Ready notifies the server that the client is ready and sets state ready on success.
// It also updates the current game and start time from the server response.
func (a *API) Ready(ctx context.Context, state *ClientState) error {
	req, err := a.newRequest(ctx, http.MethodPost, "/api/ready", nil)
	if err != nil {
		return err
	}
	resp, _, err := a.do(req)
	if err != nil {
		return fmt.Errorf("ready send error: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("nil ready response")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ready failed: %s: %s",
			resp.Status, strings.TrimSpace(string(b)))
	}

	// Decode JSON response
	var data struct {
		GameFile *string `json:"game_file"` // nullable string
		StartAt  *int64  `json:"start_at"`  // nullable unix timestamp
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("decode ready response: %w", err)
	}

	// Update client state
	state.SetReady(true)

	if data.GameFile != nil {
		state.SetCurrentGame(*data.GameFile)
	} else {
		state.SetCurrentGame("")
	}

	if data.StartAt != nil {
		state.SetStartTime(time.Unix(*data.StartAt, 0))
	} else {
		state.SetStartTime(time.Time{}) // zero value = unset
	}

	return nil
}

// SwapComplete notifies server that a swap finished.
func (a *API) SwapComplete(ctx context.Context, roundNumber int) error {
	payload := map[string]any{"round_number": roundNumber}
	req, err := a.newRequest(ctx, http.MethodPost, "/api/swap-complete", payload)
	if err != nil {
		return err
	}
	resp, _, err := a.do(req)
	if err != nil {
		return fmt.Errorf("swap-complete send error: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("nil swap-complete response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("swap-complete failed: %s", resp.Status)
	}
	return nil
}

// GameStopped notifies server that the game stopped.
func (a *API) GameStopped(ctx context.Context) error {
	req, err := a.newRequest(ctx, http.MethodPost, "/api/game-stopped", nil)
	if err != nil {
		return err
	}
	resp, _, err := a.do(req)
	if err != nil {
		return fmt.Errorf("game-stopped send error: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("nil game-stopped response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("game-stopped failed: %s", resp.Status)
	}
	return nil
}

// RegisterPlayer registers a player and returns bearer token + app key.
func (a *API) RegisterPlayer(ctx context.Context, playerName string) (string, string, error) {
	payload := map[string]string{"name": playerName}
	req, err := a.newRequest(ctx, http.MethodPost, "/api/register-player", payload)
	if err != nil {
		return "", "", err
	}
	// registration shouldn't send the client's bearer (but newRequest will
	// set the Authorization header if a.bearer is set). If you want to force
	// no auth for registration, create the request manually.
	resp, _, err := a.do(req)
	if err != nil {
		return "", "", fmt.Errorf("register send error: %w", err)
	}
	if resp == nil {
		return "", "", fmt.Errorf("nil register response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("register failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var data struct {
		BearerToken  string `json:"bearer_token"`
		ReverbAppKey string `json:"reverb_app_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}
	return data.BearerToken, data.ReverbAppKey, nil
}

// CheckTokenExists validates an arbitrary token (use token != "" to pass a
// token other than the API client's bearer). Returns (exists, error).
func (a *API) CheckTokenExists(ctx context.Context, token string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/check-token", nil)
	if err != nil {
		return false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if a.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+a.bearer)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("check-token send error: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		b, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("check-token failed: %s: %s",
			resp.Status, strings.TrimSpace(string(b)))
	}
}

// CheckSessionExists returns true if the session exists.
func (a *API) CheckSessionExists(ctx context.Context, sessionName string) (bool, error) {
	req, err := a.newRequest(ctx, http.MethodGet, "/api/check-session/"+sessionName, nil)
	if err != nil {
		return false, err
	}
	resp, _, err := a.do(req)
	if err != nil {
		return false, fmt.Errorf("check-session send error: %w", err)
	}
	if resp == nil {
		return false, fmt.Errorf("nil check-session response")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		b, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("check-session failed: %s: %s",
			resp.Status, strings.TrimSpace(string(b)))
	}
}

// JoinSession joins a session and returns the list of game files.
func (a *API) JoinSession(ctx context.Context, sessionName string) ([]string, error) {
	req, err := a.newRequest(ctx, http.MethodPost, "/api/join-session/"+sessionName, nil)
	if err != nil {
		return nil, err
	}
	resp, _, err := a.do(req)
	if err != nil {
		return nil, fmt.Errorf("join-session send error: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("nil join-session response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("join-session failed: %s: %s",
			resp.Status, strings.TrimSpace(string(b)))
	}
	var session struct {
		ID    int `json:"id"`
		Name  string `json:"name"`
		Games []struct {
			File string `json:"file"`
			ExtraFile *string `json:"extra_file"`
		} `json:"games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}
	var files []string
	for _, g := range session.Games {
		files = append(files, g.File)
		if g.ExtraFile != nil {
			files = append(files, *g.ExtraFile)
		}
	}
	return files, nil
}