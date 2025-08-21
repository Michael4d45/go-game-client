package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	pusher "github.com/bencurio/pusher-ws-go"
)

type PusherClient struct {
	client   *pusher.Client
	cfg      *Config
	state    *ClientState
	handlers *Handlers
}

func NewPusherClient(cfg *Config, state *ClientState, handlers *Handlers) *PusherClient {
	return &PusherClient{
		cfg:      cfg,
		state:    state,
		handlers: handlers,
	}
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

	playerChannelName := fmt.Sprintf("private-player.%s", pc.cfg.PlayerName)
	pch, err := pc.client.Subscribe(playerChannelName)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", playerChannelName, err)
	}
	log.Printf("[DEBUG] Subscribed to channel: %s", playerChannelName)

	sessionChannelName := fmt.Sprintf("private-session.%s", pc.cfg.SessionName)
	sch, err := pc.client.Subscribe(sessionChannelName)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", sessionChannelName, err)
	}
	log.Printf("[DEBUG] Subscribed to channel: %s", sessionChannelName)

	for _, ev := range []string{"command"} {
		go pc.listenChannel(ctx, pch, playerChannelName, ev)
		go pc.listenChannel(ctx, sch, sessionChannelName, ev)
	}

	return nil
}

func (pc *PusherClient) listenChannel(
	ctx context.Context,
	ch pusher.Channel,
	channelName, eventName string,
) {
	log.Printf("[DEBUG] %s: Subscribed to event: %s", channelName, eventName)

	boundChan := ch.Bind(eventName)

	defer func() {
		ch.Unbind(eventName, boundChan)
		log.Printf("[DEBUG] %s: Unbound from event: %s", channelName, eventName)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case raw, ok := <-boundChan:
			if !ok {
				log.Printf("[WARN] Channel %s closed", channelName)
				pc.state.SetConnected(false)
				return
			}
			pc.handlers.handleRawEvent(raw)
		}
	}
}
