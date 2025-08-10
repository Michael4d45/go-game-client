package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	pusher "github.com/bencurio/pusher-ws-go"
)

type PusherClient struct {
	client *pusher.Client
	cfg    *Config
}

func NewPusherClient(cfg *Config) *PusherClient {
	return &PusherClient{
		cfg: cfg,
	}
}

func (pc *PusherClient) ConnectAndListen(ctx context.Context) error {
	insecure := true
	cleanHost := pc.cfg.ServerURL

	if strings.HasPrefix(cleanHost, "http://") {
		insecure = true
		cleanHost = strings.TrimPrefix(cleanHost, "http://")
	} else if strings.HasPrefix(cleanHost, "https://") {
		insecure = false
		cleanHost = strings.TrimPrefix(cleanHost, "https://")
	}
	cleanHost = strings.TrimSuffix(cleanHost, "/")

	authURL := fmt.Sprintf("%s/broadcasting/auth", pc.cfg.ServerURL)
	log.Printf("[DEBUG] Connecting to Reverb: host=%s port=%d insecure=%v", cleanHost, pc.cfg.Reverb.HostPort, insecure)
	log.Printf("[DEBUG] Auth URL: %s", authURL)

	pc.client = &pusher.Client{
		Insecure: insecure,
		AuthURL:  authURL,
		AuthHeaders: http.Header{
			"Authorization": []string{"Bearer " + pc.cfg.Reverb.BearerToken},
			"Accept":        []string{"application/json"},
		},
		AuthParams:   url.Values{},
		Errors:       make(chan error, 1),
		OverrideHost: cleanHost,
		OverridePort: pc.cfg.Reverb.HostPort,
	}

	if err := pc.client.Connect(pc.cfg.Reverb.AppKey); err != nil {
		return fmt.Errorf("[ERROR] Pusher connect error: %w", err)
	}
	log.Println("[DEBUG] WebSocket connection established")

	playerChannelName := fmt.Sprintf("private-player.%s", pc.cfg.PlayerID)
	pch, err := pc.client.Subscribe(playerChannelName)
	if err != nil {
		log.Printf("[ERROR] Failed to subscribe to %s: %v", playerChannelName, err)
	} else {
		log.Printf("[DEBUG] Subscribed to channel: %s", playerChannelName)
	}

	// Bind to all known event names
	for _, ev := range []string{"command"} {
		go func(eventName string) {
			log.Printf("[DEBUG] %s: Subscribed to event: %s", playerChannelName, eventName)
			for raw := range pch.Bind(eventName) {
				pc.handleRawEvent(raw)
			}
		}(ev)
	}

	sessionChannelName := fmt.Sprintf("private-session.%s", pc.cfg.SessionName)
	sch, err := pc.client.Subscribe(sessionChannelName)
	if err != nil {
		log.Printf("[ERROR] Failed to subscribe to %s: %v", sessionChannelName, err)
	} else {
		log.Printf("[DEBUG] Subscribed to channel: %s", sessionChannelName)
	}

	// Bind to all known event names
	for _, ev := range []string{"command"} {
		go func(eventName string) {
			log.Printf("[DEBUG] %s: Subscribed to event: %s", sessionChannelName, eventName)
			for raw := range sch.Bind(eventName) {
				pc.handleRawEvent(raw)
			}
		}(ev)
	}

	return nil
}

func (pc *PusherClient) handleRawEvent(raw json.RawMessage) {
	log.Printf("[DEBUG] Raw event from Pusher: %s", string(raw))

	// Step 1: Unmarshal into a string (Pusher wraps the payload as a string)
	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		log.Printf("[ERROR] Unmarshal outer event: %v", err)
		return
	}
	log.Printf("[DEBUG] Outer JSON string: %s", inner)

	// Step 2: Unmarshal the inner JSON string into WSMessage
	var msg WSMessage
	if err := json.Unmarshal([]byte(inner), &msg); err != nil {
		log.Printf("[ERROR] Unmarshal inner WSMessage: %v", err)
		return
	}

	// Dispatch to existing handlers
	switch msg.Type {
	case "swap":
		handleSwap(pc.cfg, msg.Payload)
	case "download_rom":
		handleDownloadROM(msg.Payload)
	case "download_lua":
		handleDownloadLua(msg.Payload)
	case "message":
		handleServerMessage(msg.Payload)
	case "kick":
		handleKick(msg.Payload)
	case "start_game":
		handleStartGame(pc.cfg, msg.Payload)
	case "pause_game":
		handlePauseGame(pc.cfg, msg.Payload)
	case "session_ended":
		handleSessionEnded(pc.cfg, msg.Payload)
	case "prepare_swap":
		handlePrepareSwap(pc.cfg, msg.Payload)
	default:
		log.Printf("[WARN] Unknown event type: %s", msg.Type)
	}
}
