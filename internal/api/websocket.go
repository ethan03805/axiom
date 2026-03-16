package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/gorilla/websocket"
)

// WSHub manages WebSocket connections for real-time event streaming.
// See Architecture Section 24.2 (WebSocket).
type WSHub struct {
	emitter  *events.Emitter
	upgrader websocket.Upgrader

	mu      sync.Mutex
	clients map[*websocket.Conn]string // conn -> project ID filter
	stopCh  chan struct{}
}

// NewWSHub creates a WebSocket hub that streams engine events.
func NewWSHub(emitter *events.Emitter) *WSHub {
	return &WSHub{
		emitter: emitter,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients: make(map[*websocket.Conn]string),
		stopCh:  make(chan struct{}),
	}
}

// Run starts the event subscription loop. Events from the engine emitter
// are broadcast to all connected WebSocket clients.
func (h *WSHub) Run() {
	h.emitter.SubscribeAll(func(event events.Event) {
		h.broadcast(event)
	})
}

// Stop closes all client connections.
func (h *WSHub) Stop() {
	close(h.stopCh)
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		conn.Close()
	}
	h.clients = make(map[*websocket.Conn]string)
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and registers
// the client for event streaming.
// Endpoint: ws://localhost:3000/ws/projects/:id
func (h *WSHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	h.mu.Lock()
	h.clients[conn] = projectID
	h.mu.Unlock()

	// Read loop (keeps connection alive; client sends pings/close).
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			conn.Close()
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()
}

// broadcast sends an event to all connected WebSocket clients.
func (h *WSHub) broadcast(event events.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
			delete(h.clients, conn)
		}
	}
}

// ClientCount returns the number of connected WebSocket clients.
func (h *WSHub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
