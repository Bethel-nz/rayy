package routes

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"notkyloren/rayy/internal/config"
	"notkyloren/rayy/internal/middleware"
	"notkyloren/rayy/internal/types"

	"github.com/gorilla/websocket"
)

// Router extends types.Router with WebSocket handling capabilities
type Router struct {
	*types.Router
	config            *config.Config
	upgrader          websocket.Upgrader
	roomCleanupTimers map[string]*time.Timer
	cleanupMu         sync.Mutex
	shutdown          chan struct{}
	clients           sync.Map // track all clients for graceful shutdown
}

func NewRouter(cfg *config.Config) *Router {
	if cfg == nil {
		cfg = config.Load() // Fallback to default config if none provided
	}

	return &Router{
		Router: &types.Router{
			Rooms: make(map[string]*types.Room),
			Mu:    &sync.RWMutex{},
		},
		config: cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  cfg.WebSocket.ReadBufferSize,
			WriteBufferSize: cfg.WebSocket.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// If "*" is in allowed origins, allow all
				for _, allowed := range cfg.WebSocket.AllowedOrigins {
					if allowed == "*" {
						return true
					}
					if origin == allowed {
						return true
					}
				}
				return false
			},
		},
		roomCleanupTimers: make(map[string]*time.Timer),
		shutdown:          make(chan struct{}),
	}
}

func (router *Router) SetupRoutes() {
	// Use config values for CORS allowed origins
	corsMiddleware := middleware.NewCORS(router.config.WebSocket.AllowedOrigins...)

	// Use configuration values for rate limiting
	rateLimiter := middleware.NewRateLimiter(
		router.config.RateLimit.Requests,
		router.config.RateLimit.Duration,
	)

	http.HandleFunc("/ws", corsMiddleware.CORS(
		rateLimiter.RateLimit(router.handleWebSocket),
	))
}

// GracefulShutdown handles the graceful shutdown of all connections
func (router *Router) GracefulShutdown() {
	// Signal all goroutines to stop
	close(router.shutdown)

	// Send close message to all clients
	router.clients.Range(func(k, v interface{}) bool {
		client, ok := v.(*types.Client)
		if ok && client != nil && client.Conn != nil {
			// Try to send close message
			client.WriteMu.Lock()
			client.Conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "Server shutting down"),
			)
			client.WriteMu.Unlock()
			client.Conn.Close()
		}
		return true
	})

	// Give clients time to receive and process the close message
	time.Sleep(500 * time.Millisecond)

	slog.Info("All WebSocket connections closed")
}

// handleWebSocket manages the WebSocket connection lifecycle
func (router *Router) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	client, room, err := router.setupConnection(w, r)
	if err != nil {
		slog.Error("Connection setup failed", "error", err)
		return
	}

	// Store client for graceful shutdown
	router.clients.Store(client.ID, client)
	defer router.clients.Delete(client.ID)

	// Create a done channel to coordinate cleanup
	done := make(chan struct{})
	defer close(done)

	// Start message handling in a goroutine
	go func() {
		router.handleMessages(client, room)
		done <- struct{}{} // Signal that handling is complete
	}()

	// Wait for either done signal, context cancellation, or server shutdown
	select {
	case <-done:
	case <-r.Context().Done():
	case <-router.shutdown:
	}

	// Cleanup after everything is done
	router.cleanupConnection(client, room)
}

// setupConnection handles the initial connection setup
func (router *Router) setupConnection(w http.ResponseWriter, r *http.Request) (*types.Client, *types.Room, error) {
	roomID := r.URL.Query().Get("room")
	clientID := r.URL.Query().Get("client_id")

	if roomID == "" || clientID == "" {
		http.Error(w, "Missing room ID or client ID", http.StatusBadRequest)
		return nil, nil, fmt.Errorf("missing parameters")
	}

	conn, err := router.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("upgrade failed: %v", err)
	}

	// Set connection parameters
	conn.SetReadLimit(int64(router.config.WebSocket.MaxMessageSizeKB * 1024))
	conn.SetReadDeadline(time.Now().Add(time.Duration(router.config.WebSocket.PongWaitSec) * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(time.Duration(router.config.WebSocket.PongWaitSec) * time.Second))
		return nil
	})

	room := router.getOrCreateRoom(roomID)

	// Check if room is full
	room.Mu.RLock()
	if len(room.Clients) >= router.config.Room.MaxClients {
		room.Mu.RUnlock()
		conn.Close()
		return nil, nil, fmt.Errorf("room %s is at capacity", roomID)
	}
	room.Mu.RUnlock()

	client := &types.Client{
		ID:      clientID,
		Conn:    conn,
		Room:    room,
		WriteMu: &sync.Mutex{},
		Send:    make(chan types.Message, 256),
	}

	// Cancel any pending cleanup when a client joins
	router.cancelRoomCleanup(roomID)

	// Add client to room
	router.addClientToRoom(client, room)

	// Start the writer pump
	go router.writerPump(client)

	// Start the ping ticker
	go router.pingClient(client)

	// Send join notification to all other clients
	joinMsg := types.Message{
		Action:    "event",
		Event:     "join",
		From:      client.ID,
		Room:      room.ID,
		Timestamp: time.Now(),
	}
	router.broadcastToRoom(room, joinMsg, client)

	slog.Info("Client connected",
		"client_id", client.ID,
		"room_id", room.ID,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
	)

	return client, room, nil
}

// pingClient periodically sends ping messages to keep the connection alive
func (router *Router) pingClient(client *types.Client) {
	ticker := time.NewTicker(time.Duration(router.config.WebSocket.PingIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			client.WriteMu.Lock()
			client.Conn.SetWriteDeadline(time.Now().Add(time.Duration(router.config.WebSocket.WriteWaitSec) * time.Second))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				client.WriteMu.Unlock()
				return
			}
			client.WriteMu.Unlock()
		case <-router.shutdown:
			return
		}
	}
}

// writerPump pumps messages from the client's send channel to the websocket connection
func (router *Router) writerPump(client *types.Client) {
	for {
		select {
		case msg, ok := <-client.Send:
			client.WriteMu.Lock()
			client.Conn.SetWriteDeadline(time.Now().Add(time.Duration(router.config.WebSocket.WriteWaitSec) * time.Second))
			if !ok {
				// Channel was closed
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				client.WriteMu.Unlock()
				return
			}

			if err := client.Conn.WriteJSON(msg); err != nil {
				slog.Error("Failed to write message",
					"client_id", client.ID,
					"room_id", client.Room.ID,
					"error", err,
				)
				client.WriteMu.Unlock()
				return
			}
			client.WriteMu.Unlock()
		case <-router.shutdown:
			return
		}
	}
}

// getOrCreateRoom returns an existing room or creates a new one
func (router *Router) getOrCreateRoom(roomID string) *types.Room {
	router.Mu.Lock()
	defer router.Mu.Unlock()

	room, exists := router.Rooms[roomID]
	if !exists {
		room = &types.Room{
			ID:      roomID,
			Clients: make(map[string]*types.Client),
			Mu:      &sync.RWMutex{},
		}
		router.Rooms[roomID] = room
	}
	return room
}

// addClientToRoom adds a client to a room
func (router *Router) addClientToRoom(client *types.Client, room *types.Room) {
	room.Mu.Lock()
	defer room.Mu.Unlock()

	room.Clients[client.ID] = client
	slog.Info("Client joined room",
		"client_id", client.ID,
		"room_id", room.ID,
		"total_clients", len(room.Clients),
	)
}

// cleanupConnection handles cleanup when a client disconnects
func (router *Router) cleanupConnection(client *types.Client, room *types.Room) {
	if client == nil || room == nil {
		return
	}

	// First remove the client from the room
	room.Mu.Lock()
	if _, exists := room.Clients[client.ID]; exists {
		delete(room.Clients, client.ID)
		slog.Info("Client left room", "client_id", client.ID, "room_id", room.ID)

		// If room is empty, schedule cleanup
		if len(room.Clients) == 0 {
			router.scheduleRoomCleanup(room.ID)
		} else {
			// Only broadcast leave message if there are other clients
			leaveMsg := types.Message{
				Action:    "event",
				Event:     "leave",
				From:      client.ID,
				Room:      room.ID,
				Timestamp: time.Now(),
			}
			room.Mu.Unlock() // Unlock before broadcast
			router.broadcastToRoom(room, leaveMsg, client)
			return
		}
	}
	room.Mu.Unlock()

	// Close the client connection last
	if client.Conn != nil {
		client.Conn.Close()
	}
}

// handleMessages processes incoming messages
func (router *Router) handleMessages(client *types.Client, room *types.Room) {
	for {
		select {
		case <-router.shutdown:
			return
		default:
			var msg types.Message
			if err := client.Conn.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					slog.Error("Error reading message",
						"client_id", client.ID,
						"room_id", room.ID,
						"error", err,
					)
				}
				return
			}

			msg.From = client.ID
			msg.Room = room.ID
			msg.Timestamp = time.Now()

			router.handleMessage(client, room, msg)
		}
	}
}

// handleMessage processes a single message
func (router *Router) handleMessage(client *types.Client, room *types.Room, msg types.Message) {
	switch msg.Action {
	case "message":
		router.broadcastToRoom(room, msg, client)
	case "event":
		router.handleEvent(client, room, msg)
	default:
		// Log unknown message types but don't crash
		slog.Warn("Unknown message action",
			"action", msg.Action,
			"client_id", client.ID,
			"room_id", room.ID,
		)
	}
}

// handleEvent processes different event types
func (router *Router) handleEvent(client *types.Client, room *types.Room, msg types.Message) {
	switch msg.Event {
	case "typing", "join", "leave":
		router.broadcastToRoom(room, msg, client)
	case "list_users":
		router.sendUserList(client, room)
	default:
		// Handle any custom event by broadcasting it
		router.broadcastToRoom(room, msg, client)
	}
}

// sendUserList sends the list of users in a room to a client
func (router *Router) sendUserList(client *types.Client, room *types.Room) {
	room.Mu.RLock()
	users := make([]string, 0, len(room.Clients))
	for clientID := range room.Clients {
		users = append(users, clientID)
	}
	room.Mu.RUnlock()

	msg := types.Message{
		Action:    "event",
		Event:     "list_users",
		From:      "system",
		Room:      room.ID,
		Timestamp: time.Now(),
		Data: map[string]any{
			"users": users,
		},
	}
	client.Send <- msg
}

// broadcastToRoom sends a message to all clients in a room except the sender
func (router *Router) broadcastToRoom(room *types.Room, msg types.Message, sender *types.Client) {
	room.Mu.RLock()
	// Copy clients map to avoid holding lock during broadcast
	clients := make(map[string]*types.Client)
	for id, client := range room.Clients {
		if id != sender.ID && client.Conn != nil {
			clients[id] = client
		}
	}
	room.Mu.RUnlock()

	// Count errors to handle problematic connections
	var errorCount sync.Map

	// Broadcast without blocking - use the send channel
	for id, client := range clients {
		// Non-blocking send to channel
		select {
		case client.Send <- msg:
			// Message sent successfully
		default:
			// Channel is full, client might be slow or unresponsive
			// Record as error for potential cleanup
			count, _ := errorCount.LoadOrStore(id, 1)
			if count.(int) >= 5 {
				// Too many errors, clean up this client
				slog.Warn("Client send buffer full, disconnecting",
					"client_id", id,
					"room_id", room.ID,
				)
				client.Conn.Close() // Force close to trigger normal cleanup
			} else {
				errorCount.Store(id, count.(int)+1)
			}
		}
	}
}

// scheduleRoomCleanup starts a timer to clean up an empty room
func (router *Router) scheduleRoomCleanup(roomID string) {
	router.cleanupMu.Lock()
	defer router.cleanupMu.Unlock()

	// Cancel existing timer if any
	if timer, exists := router.roomCleanupTimers[roomID]; exists {
		timer.Stop()
	}

	// Set new timer
	cleanupTime := time.Duration(router.config.Room.CleanupTimeoutSec) * time.Second
	router.roomCleanupTimers[roomID] = time.AfterFunc(cleanupTime, func() {
		router.cleanupMu.Lock()
		delete(router.roomCleanupTimers, roomID)
		router.cleanupMu.Unlock()

		router.Mu.Lock()
		if room, exists := router.Rooms[roomID]; exists {
			room.Mu.Lock()
			if len(room.Clients) == 0 {
				delete(router.Rooms, roomID)
				slog.Info("Room removed after timeout", "room_id", roomID)
			}
			room.Mu.Unlock()
		}
		router.Mu.Unlock()
	})
}

// cancelRoomCleanup cancels a scheduled room cleanup
func (router *Router) cancelRoomCleanup(roomID string) {
	router.cleanupMu.Lock()
	defer router.cleanupMu.Unlock()

	if timer, exists := router.roomCleanupTimers[roomID]; exists {
		timer.Stop()
		delete(router.roomCleanupTimers, roomID)
	}
}
