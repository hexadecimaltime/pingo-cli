package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

type FayeData struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Time      float64         `json:"time"`
	Timestamp string          `json:"timestamp"`
	Iteration int             `json:"iteration"`
	Session   string          `json:"session"`
	Event     string          `json:"event"`
}

type FayeEvent struct {
	Channel string
	Data    FayeData
}

type fayeMessage struct {
	Channel      string          `json:"channel"`
	ClientID     string          `json:"clientId"`
	Subscription string          `json:"subscription"`
	Successful   *bool           `json:"successful"`
	Error        string          `json:"error"`
	Advice       json.RawMessage `json:"advice"`
	Data         json.RawMessage `json:"data"`
	ID           string          `json:"id"`
}

type FayeClient struct {
	conn       *websocket.Conn
	mu         sync.Mutex
	clientID   string
	nextID     int
	events     chan FayeEvent
	subscribed map[string]struct{}
	done       chan struct{}
}

func NewFayeClient() *FayeClient {
	return &FayeClient{
		events:     make(chan FayeEvent, 16),
		subscribed: map[string]struct{}{},
		done:       make(chan struct{}),
	}
}

func (c *FayeClient) Events() <-chan FayeEvent {
	return c.events
}

func (c *FayeClient) Connect(ctx context.Context, url string, sessionChannel string) error {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {

		return err
	}
	c.conn = conn

	if err := c.handshake(ctx); err != nil {
		_ = c.conn.Close(websocket.StatusNormalClosure, "handshake failed")

		return err
	}

	if sessionChannel != "" {
		if err := c.Subscribe(ctx, sessionChannel); err != nil {

			return err
		}
	}

	if err := c.sendConnect(ctx); err != nil {

		return err
	}

	go c.readLoop()
	go c.connectLoop()

	return nil
}

func (c *FayeClient) Close() error {
	select {
	case <-c.done:

		return nil
	default:
	}
	close(c.done)
	if c.conn != nil {

		return c.conn.Close(websocket.StatusNormalClosure, "closing")
	}

	return nil
}

func (c *FayeClient) Subscribe(ctx context.Context, channel string) error {
	if !strings.HasPrefix(channel, "/") {
		channel = "/" + channel
	}
	c.mu.Lock()
	if _, ok := c.subscribed[channel]; ok {
		c.mu.Unlock()

		return nil
	}
	c.subscribed[channel] = struct{}{}
	c.mu.Unlock()

	msg := map[string]any{
		"channel":      "/meta/subscribe",
		"clientId":     c.clientID,
		"subscription": channel,
		"id":           c.nextIDString(),
	}

	return c.send(ctx, msg)
}

func (c *FayeClient) handshake(ctx context.Context) error {
	msg := map[string]any{
		"channel":                  "/meta/handshake",
		"version":                  "1.0",
		"supportedConnectionTypes": []string{"websocket"},
		"id":                       c.nextIDString(),
	}
	if err := c.send(ctx, msg); err != nil {

		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, data, err := c.conn.Read(ctx)
		if err != nil {

			return err
		}
		msgs, err := decodeMessages(data)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.Channel != "/meta/handshake" {
				continue
			}
			if m.Successful != nil && *m.Successful && m.ClientID != "" {
				c.clientID = m.ClientID

				return nil
			}
			if m.Error != "" {

				return errors.New(m.Error)
			}
		}
	}

	return errors.New("handshake timeout")
}

func (c *FayeClient) connectLoop() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = c.sendConnect(context.Background())
		case <-c.done:

			return
		}
	}
}

func (c *FayeClient) sendConnect(ctx context.Context) error {
	msg := map[string]any{
		"channel":        "/meta/connect",
		"clientId":       c.clientID,
		"connectionType": "websocket",
		"id":             c.nextIDString(),
	}

	return c.send(ctx, msg)
}

func (c *FayeClient) readLoop() {
	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			close(c.events)

			return
		}
		msgs, err := decodeMessages(data)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if strings.HasPrefix(m.Channel, "/meta/") {
				continue
			}
			if len(m.Data) == 0 {
				continue
			}
			var data FayeData
			if err := json.Unmarshal(m.Data, &data); err != nil {
				continue
			}
			c.events <- FayeEvent{Channel: m.Channel, Data: data}
		}
	}
}

func (c *FayeClient) nextIDString() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++

	return fmt.Sprintf("%d", c.nextID)
}

func (c *FayeClient) send(ctx context.Context, msg map[string]any) error {
	payload, err := json.Marshal([]map[string]any{msg})
	if err != nil {

		return err
	}

	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func decodeMessages(data []byte) ([]fayeMessage, error) {
	var msgs []fayeMessage
	if err := json.Unmarshal(data, &msgs); err == nil {

		return msgs, nil
	}
	var single fayeMessage
	if err := json.Unmarshal(data, &single); err == nil {

		return []fayeMessage{single}, nil
	}

	return nil, errors.New("invalid faye message")
}
