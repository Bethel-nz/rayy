package types

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Router is the main router structure
type Router struct {
	Rooms map[string]*Room
	Mu    *sync.RWMutex
}

// Room represents a chat room
type Room struct {
	ID      string
	Clients map[string]*Client
	Mu      *sync.RWMutex
}

// Client represents a connected websocket client
type Client struct {
	ID      string
	Conn    *websocket.Conn
	Room    *Room
	WriteMu *sync.Mutex
	Send    chan Message
}

// Message represents a message exchanged between clients
type Message struct {
	Action    string         `json:"action"`
	Event     string         `json:"event,omitempty"`
	Content   string         `json:"content,omitempty"`
	From      string         `json:"from"`
	Room      string         `json:"room"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}
