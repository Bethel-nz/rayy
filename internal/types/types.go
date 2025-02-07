package types

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	ID      string
	Conn    *websocket.Conn
	Room    *Room
	WriteMu sync.Mutex
}

type Room struct {
	ID      string
	Clients map[string]*Client
	Mu      *sync.RWMutex
}

type Message struct {
	Action    string         `json:"action"` // "message", "typing", "join", "leave", etc.
	Room      string         `json:"room"`
	Content   string         `json:"content"`
	From      string         `json:"from"`
	Timestamp time.Time      `json:"timestamp"`
	Event     string         `json:"event,omitempty"` // For custom events
	Data      map[string]any `json:"data,omitempty"`  // Additional event data
}

type Router struct {
	Rooms map[string]*Room
	Mu    *sync.RWMutex
}
