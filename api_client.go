package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

type requestOptions struct {
	skipAuth bool
	token    string
}

func (a *API) newRequest(
	ctx context.Context,
	method, path string,
	payload any,
	opts ...requestOptions,
) (*http.Request, error) {
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

	// Apply options
	var opt requestOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	if !opt.skipAuth {
		token := a.bearer
		if opt.token != "" {
			token = opt.token
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
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

// readErrorBody safely reads the response body for inclusion in an error message.
func readErrorBody(r io.Reader) string {
	b, err := io.ReadAll(r)
	if err != nil {
		return fmt.Sprintf("(failed to read body: %v)", err)
	}
	return strings.TrimSpace(string(b))
}

// Heartbeat posts a heartbeat and returns measured ping (ms).
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
	if resp == nil {
		return 0, fmt.Errorf("nil heartbeat response")
	}
	defer resp.Body.Close()

	newPing := int(rtt.Milliseconds())
	if resp.StatusCode != http.StatusOK {
		return newPing, fmt.Errorf("heartbeat status: %s", resp.Status)
	}

	state.SetPing(newPing)
	return newPing, nil
}

// Ready notifies the server that the client is ready.
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
		return fmt.Errorf(
			"ready failed: %s: %s",
			resp.Status,
			readErrorBody(resp.Body),
		)
	}

	var data struct {
		GameFile *string `json:"game_file"`
		State    string  `json:"state"`
		StateAt  int64   `json:"state_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("decode ready response: %w", err)
	}

	state.SetReady(true)
	if data.GameFile != nil {
		state.SetCurrentGame(*data.GameFile)
	} else {
		state.SetCurrentGame("")
	}
	stateTime := time.Unix(data.StateAt, 0)
	log.Printf(
		"Scheduled %s at %s (%d)",
		data.State,
		stateTime.Format(time.RFC3339),
		data.StateAt,
	)
	state.SetState(stateTime, data.State)

	return nil
}

// SwapComplete notifies server that a swap finished.
func (a *API) SwapComplete(ctx context.Context, roundNumber int) error {
	payload := map[string]any{"round_number": roundNumber}
	req, err := a.newRequest(
		ctx,
		http.MethodPost,
		"/api/swap-complete",
		payload,
	)
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
func (a *API) RegisterPlayer(
	ctx context.Context,
	playerName string,
) (string, string, error) {
	payload := map[string]string{"name": playerName}
	// Registration should not send an existing bearer token.
	req, err := a.newRequest(
		ctx,
		http.MethodPost,
		"/api/register-player",
		payload,
		requestOptions{skipAuth: true},
	)
	if err != nil {
		return "", "", err
	}

	resp, _, err := a.do(req)
	if err != nil {
		return "", "", fmt.Errorf("register send error: %w", err)
	}
	if resp == nil {
		return "", "", fmt.Errorf("nil register response")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf(
			"register failed: %s: %s",
			resp.Status,
			readErrorBody(resp.Body),
		)
	}
	var data struct {
		BearerToken  string `json:"bearer_token"`
		ReverbAppKey string `json:"reverb_app_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", fmt.Errorf("decode register response: %w", err)
	}
	return data.BearerToken, data.ReverbAppKey, nil
}

// CheckTokenExists validates a token.
func (a *API) CheckTokenExists(ctx context.Context, token string) (bool, error) {
	req, err := a.newRequest(
		ctx,
		http.MethodPost,
		"/api/check-token",
		nil,
		requestOptions{token: token},
	)
	if err != nil {
		return false, err
	}

	resp, _, err := a.do(req)
	if err != nil {
		return false, fmt.Errorf("check-token send error: %w", err)
	}
	if resp == nil {
		return false, fmt.Errorf("nil check-token response")
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf(
			"check-token failed: %s: %s",
			resp.Status,
			readErrorBody(resp.Body),
		)
	}
}

// CheckSessionExists returns true if the session exists.
func (a *API) CheckSessionExists(
	ctx context.Context,
	sessionName string,
) (bool, error) {
	path := fmt.Sprintf("/api/check-session/%s", sessionName)
	req, err := a.newRequest(ctx, http.MethodGet, path, nil)
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
		return false, fmt.Errorf(
			"check-session failed: %s: %s",
			resp.Status,
			readErrorBody(resp.Body),
		)
	}
}

// JoinSession joins a session and returns the list of game files.
func (a *API) JoinSession(
	ctx context.Context,
	sessionName string,
) ([]string, error) {
	path := fmt.Sprintf("/api/join-session/%s", sessionName)
	req, err := a.newRequest(ctx, http.MethodPost, path, nil)
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
		return nil, fmt.Errorf(
			"join-session failed: %s: %s",
			resp.Status,
			readErrorBody(resp.Body),
		)
	}
	var session struct {
		Games []struct {
			File      string  `json:"file"`
			ExtraFile *string `json:"extra_file"`
		} `json:"games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decode join-session response: %w", err)
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
