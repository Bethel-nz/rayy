package routes

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"notkyloren/rayy/internal/middleware"
	"notkyloren/rayy/internal/types"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// Router extends types.Router with WebSocket handling capabilities
type Router struct {
	*types.Router
	roomCleanupTimers map[string]*time.Timer
	cleanupMu         sync.Mutex
}

func NewRouter() *Router {
	return &Router{
		Router: &types.Router{
			Rooms: make(map[string]*types.Room),
			Mu:    &sync.RWMutex{},
		},
		roomCleanupTimers: make(map[string]*time.Timer),
	}
}

func (router *Router) SetupRoutes() {
	corsMiddleware := middleware.NewCORS()
	rateLimiter := middleware.NewRateLimiter()

	http.HandleFunc("/ws", corsMiddleware.CORS(
		rateLimiter.RateLimit(router.handleWebSocket),
	))
}

// handleWebSocket manages the WebSocket connection lifecycle
func (router *Router) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	client, room, err := router.setupConnection(w, r)
	if err != nil {
		log.Printf("Connection setup failed: %v", err)
		return
	}

	// Create a done channel to coordinate cleanup
	done := make(chan struct{})
	defer close(done)

	// Start message handling in a goroutine
	go func() {
		router.handleMessages(client, room)
		done <- struct{}{} // Signal that handling is complete
	}()

	// Wait for either done signal or context cancellation
	select {
	case <-done:
	case <-r.Context().Done():
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

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("upgrade failed: %v", err)
	}

	room := router.getOrCreateRoom(roomID)
	client := &types.Client{
		ID:   clientID,
		Conn: conn,
		Room: room,
	}

	// Cancel any pending cleanup when a client joins
	router.cancelRoomCleanup(roomID)

	// Add client to room
	router.addClientToRoom(client, room)

	// Send join notification to all other clients
	joinMsg := types.Message{
		Action:    "event",
		Event:     "join",
		From:      client.ID,
		Room:      room.ID,
		Timestamp: time.Now(),
	}
	router.broadcastToRoom(room, joinMsg, client)

	return client, room, nil
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
	log.Printf("Client %s joined room %s. Total clients: %d",
		client.ID, room.ID, len(room.Clients))
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
		log.Printf("Client %s left room %s", client.ID, room.ID)

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
		var msg types.Message
		if err := client.Conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Error reading message from client %s: %v", client.ID, err)
			}
			break
		}

		msg.From = client.ID
		msg.Room = room.ID
		msg.Timestamp = time.Now()

		router.handleMessage(client, room, msg)
	}
}

// handleMessage processes a single message
func (router *Router) handleMessage(client *types.Client, room *types.Room, msg types.Message) {
	switch msg.Action {
	case "message":
		router.broadcastToRoom(room, msg, client)
	case "event":
		router.handleEvent(client, room, msg)
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
	client.Conn.WriteJSON(msg)
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

	// Broadcast without cleanup - just log errors
	for id, client := range clients {
		client.WriteMu.Lock()
		err := client.Conn.WriteJSON(msg)
		client.WriteMu.Unlock()

		if err != nil {
			if !websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived) {
				// Only log unexpected errors
				log.Printf("Failed to broadcast to client %s: %v", id, err)
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
	router.roomCleanupTimers[roomID] = time.AfterFunc(30*time.Second, func() {
		router.cleanupMu.Lock()
		delete(router.roomCleanupTimers, roomID)
		router.cleanupMu.Unlock()

		router.Mu.Lock()
		if room, exists := router.Rooms[roomID]; exists {
			room.Mu.Lock()
			if len(room.Clients) == 0 {
				delete(router.Rooms, roomID)
				log.Printf("Room %s removed after timeout", roomID)
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
