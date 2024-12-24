package types

import "time"

type Room struct {
	ID        string    `json:"id"`
	Clients   map[string]*Client `json:"clients"`
	CreatedAt time.Time `json:"created_at"`
}

type Client struct {
	ID   string          `json:"id"`
	Conn *websocket.Conn `json:"-"`
	Room *Room          `json:"-"`
}

type Message struct {
	Action    string    `json:"action"`
	Room      string    `json:"room"`
	Content   string    `json:"content"`
	From      string    `json:"from"`
	Timestamp time.Time `json:"timestamp"`
}
