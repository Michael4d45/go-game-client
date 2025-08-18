package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	pusher "github.com/bencurio/pusher-ws-go"
)

type PusherClient struct {
	client *pusher.Client
	cfg    *Config
	state  *ClientState
}

func NewPusherClient(cfg *Config, state *ClientState) *PusherClient {
	return &PusherClient{cfg: cfg, state: state}
}

func (pc *PusherClient) ConnectAndListen(ctx context.Context) error {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := pc.connectOnce(ctx); err != nil {
			log.Printf("[ERROR] Pusher connect failed: %v", err)
			pc.state.SetConnected(false)
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		backoff = time.Second
		<-ctx.Done()
		return nil
	}
}

func (pc *PusherClient) connectOnce(ctx context.Context) error {
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

	if err := pc.client.Connect(pc.cfg.AppKey); err != nil {
		return fmt.Errorf("pusher connect error: %w", err)
	}
	log.Println("[DEBUG] WebSocket connection established")
	pc.state.SetConnected(true)
	// Subscribe to player channel
	playerChannelName := fmt.Sprintf("private-player.%s", pc.cfg.PlayerName)
	pch, err := pc.client.Subscribe(playerChannelName)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", playerChannelName, err)
	}
	log.Printf("[DEBUG] Subscribed to channel: %s", playerChannelName)

	// Subscribe to session channel
	sessionChannelName := fmt.Sprintf("private-session.%s", pc.cfg.SessionName)
	sch, err := pc.client.Subscribe(sessionChannelName)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", sessionChannelName, err)
	}
	log.Printf("[DEBUG] Subscribed to channel: %s", sessionChannelName)

	// Bind events
	for _, ev := range []string{"command"} {
		go pc.listenChannel(ctx, pch, playerChannelName, ev)
		go pc.listenChannel(ctx, sch, sessionChannelName, ev)
	}

	return nil
}

func (pc *PusherClient) listenChannel(ctx context.Context, ch pusher.Channel, channelName, eventName string) {
    log.Printf("[DEBUG] %s: Subscribed to event: %s", channelName, eventName)
    for {
        select {
        case <-ctx.Done():
            return
        case raw, ok := <-ch.Bind(eventName):
            if !ok {
                log.Printf("[WARN] Channel %s closed", channelName)
                pc.state.SetConnected(false)
                return
            }
            pc.handleRawEvent(raw)
        }
    }
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
		handleSwap(pc.cfg, pc.state, msg.Payload)
	case "download_rom":
		handleDownloadROM(pc.cfg, msg.Payload)
	case "download_lua":
		handleDownloadLua(pc.cfg, msg.Payload)
	case "message":
		handleServerMessage(msg.Payload)
	case "kick":
		handleKick(msg.Payload)
	case "start_game":
		handleStartGame(pc.cfg, pc.state, msg.Payload)
	case "pause_game":
		handlePauseGame(pc.cfg, msg.Payload)
	case "session_ended":
		handleSessionEnded(pc.cfg, pc.state, msg.Payload)
	case "prepare_swap":
		handlePrepareSwap(pc.cfg, pc.state, msg.Payload)
	default:
		log.Printf("[WARN] Unknown event type: %s", msg.Type)
	}
}

// WSMessage is the envelope for Pusher events
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
