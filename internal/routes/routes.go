package routes

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

type Client struct {
	ID   string
	Conn *websocket.Conn
	Room *Room
}

type Room struct {
	ID      string
	Clients map[string]*Client
	mu      sync.Mutex
}

type Message struct {
	Action    string         `json:"action"`    // "message", "typing", "join", "leave", etc.
	Room      string         `json:"room"`
	Content   string         `json:"content"`
	From      string         `json:"from"`
	Timestamp time.Time      `json:"timestamp"`
	Event     string         `json:"event,omitempty"`    // For custom events
	Data      map[string]any `json:"data,omitempty"`    // Additional event data
}

type Router struct {
	rooms map[string]*Room
	mu    sync.Mutex
}

func NewRouter() *Router {
	return &Router{
		rooms: make(map[string]*Room),
	}
}

func (r *Router) SetupRoutes() {
	http.HandleFunc("/ws", r.handleWebSocket)
}

func (r *Router) handleWebSocket(w http.ResponseWriter, req *http.Request) {
	// Get room ID and client ID from query params
	roomID := req.URL.Query().Get("room")
	clientID := req.URL.Query().Get("client_id")
	
	log.Printf("New connection request - Room: %s, Client: %s", roomID, clientID)
	
	if roomID == "" || clientID == "" {
		log.Printf("Missing parameters - Room: %s, Client: %s", roomID, clientID)
		http.Error(w, "Missing room ID or client ID", http.StatusBadRequest)
		return
	}

	// Upgrade connection
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	log.Printf("WebSocket connection upgraded for client %s in room %s", clientID, roomID)

	// Create or get room
	r.mu.Lock()
	room, exists := r.rooms[roomID]
	if !exists {
		log.Printf("Creating new room: %s", roomID)
		room = &Room{
			ID:      roomID,
			Clients: make(map[string]*Client),
		}
		r.rooms[roomID] = room
	}
	r.mu.Unlock()

	// Create client
	client := &Client{
		ID:   clientID,
		Conn: conn,
		Room: room,
	}

	// Add client to room
	room.mu.Lock()
	room.Clients[client.ID] = client
	log.Printf("Client %s joined room %s. Total clients in room: %d", clientID, roomID, len(room.Clients))
	room.mu.Unlock()

	// Cleanup on disconnect
	defer func() {
		room.mu.Lock()
		delete(room.Clients, client.ID)
		room.mu.Unlock()

		// Remove empty room
		if len(room.Clients) == 0 {
			r.mu.Lock()
			delete(r.rooms, roomID)
			r.mu.Unlock()
		}

		conn.Close()
	}()

	// Message handling loop
	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Error reading message from client %s: %v", clientID, err)
			}
			break
		}

		log.Printf("Received message from %s: %+v", clientID, msg)

		// Set common fields
		msg.From = client.ID
		msg.Room = roomID
		msg.Timestamp = time.Now()

		// Handle different message types
		switch msg.Action {
		case "message":
			// Regular chat message
			r.broadcastToRoom(room, msg, client)
		
		case "event":
			// Handle events
			switch msg.Event {
			case "typing":
				r.broadcastToRoom(room, msg, client)
			case "join":
				// Send join notification
				joinMsg := Message{
					Action: "event",
					Event: "join",
					From: client.ID,
					Room: roomID,
					Timestamp: time.Now(),
				}
				r.broadcastToRoom(room, joinMsg, client)
			case "leave":
				// Send leave notification
				leaveMsg := Message{
					Action: "event",
					Event: "leave",
					From: client.ID,
					Room: roomID,
					Timestamp: time.Now(),
				}
				r.broadcastToRoom(room, leaveMsg, client)
			case "list_users":
				// Send user list
				users := make([]string, 0)
				for clientID := range room.Clients {
					users = append(users, clientID)
				}
				userListMsg := Message{
					Action: "event",
					Event: "users",
					From: "system",
					Room: roomID,
					Timestamp: time.Now(),
					Data: map[string]any{
						"users": users,
					},
				}
				client.Conn.WriteJSON(userListMsg)
			}
		}
	}
}

func (r *Router) broadcastToRoom(room *Room, msg Message, sender *Client) {
	room.mu.Lock()
	defer room.mu.Unlock()

	log.Printf("Broadcasting message to room %s from %s", room.ID, sender.ID)
	
	for clientID, client := range room.Clients {
		if clientID == sender.ID {
			continue
		}
		err := client.Conn.WriteJSON(msg)
		if err != nil {
			log.Printf("Error broadcasting to client %s: %v", clientID, err)
		} else {
			log.Printf("Message sent to client %s", clientID)
		}
	}
}
