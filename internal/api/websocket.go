package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Kind   string `json:"kind"`
	Value  any    `json:"value"`
	RoomID string `json:"-"`
}

type subscriber struct {
	conn   *websocket.Conn
	events chan Message
	cancel context.CancelFunc
}

func (h *Handler) notifyClients(message Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.subscribers[message.RoomID] {
		select {
		case client.events <- message:
		default:
			client.cancel()
		}
	}
}

func (h *Handler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, clients := range h.subscribers {
		for client := range clients {
			client.cancel()
		}
	}
}

func (h *Handler) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	room, ok := h.readRoom(w, r)
	if !ok {
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	client := &subscriber{conn: conn, events: make(chan Message, 32), cancel: cancel}
	roomID := room.ID.String()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		cancel()
		_ = conn.Close()
		return
	}
	if h.subscribers[roomID] == nil {
		h.subscribers[roomID] = make(map[*subscriber]struct{})
	}
	h.subscribers[roomID][client] = struct{}{}
	h.mu.Unlock()
	defer func() {
		cancel()
		_ = conn.Close()
		h.mu.Lock()
		delete(h.subscribers[roomID], client)
		if len(h.subscribers[roomID]) == 0 {
			delete(h.subscribers, roomID)
		}
		h.mu.Unlock()
	}()
	go client.writePump(ctx)
	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *subscriber) writePump(ctx context.Context) {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()
	defer s.cancel()
	defer s.conn.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.events:
			_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := s.conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			if err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}
