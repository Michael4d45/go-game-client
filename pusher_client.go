package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	pusher "github.com/bencurio/pusher-ws-go"
)

type PusherClient struct {
	client *pusher.Client
	cfg    *Config
}

func NewPusherClient(cfg *Config) *PusherClient {
	return &PusherClient{cfg: cfg}
}

func (pc *PusherClient) ConnectAndListen(ctx context.Context) error {
	authURL := fmt.Sprintf("%s/broadcasting/auth", pc.cfg.ServerURL)
	log.Printf("[DEBUG] Auth URL: %s", authURL)

	pc.client = &pusher.Client{
		Insecure: pc.cfg.ServerScheme == "http",
		AuthURL:  authURL,
		AuthHeaders: http.Header{
			"Authorization": []string{"Bearer " + pc.cfg.BearerToken},
			"Accept":        []string{"application/json"},
		},
		OverrideHost: pc.cfg.ServerHost,
		OverridePort: pc.cfg.PusherPort,
	}
	log.Printf("[DEBUG] Creating pusher.Client with settings:\n"+
		"Insecure: %v\n"+
		"AuthURL: %s\n"+
		"AuthHeaders: %v\n"+
		"OverrideHost: %s\n"+
		"OverridePort: %d\n",
		pc.cfg.ServerScheme == "http",
		authURL,
		http.Header{
			"Authorization": []string{"Bearer " + pc.cfg.BearerToken},
			"Accept":        []string{"application/json"},
		},
		pc.cfg.ServerHost,
		pc.cfg.PusherPort,
	)

	if err := pc.client.Connect(pc.cfg.AppKey); err != nil {
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
