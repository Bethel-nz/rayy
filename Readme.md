# Simple WebSocket Relay Server

A lightweight WebSocket relay server built with Go that allows real-time message broadcasting between connected clients in rooms.

## What it does

- Acts as a message relay between WebSocket clients
- Supports multiple chat rooms
- Handles custom events (typing, join/leave)
- No authentication required


## Installation

1. Clone the repository:
```bash
git clone https://github.com/Bethel-nz/rayy.git
cd rayy
```
2. Run the server:
```bash
go run main.go
```
3. Run the web client:
```bash
cd client
python3 -m http.server 8080
```
4. Run the CLI client:
```bash
cd cli
python3 cli_chat.py
```

## Connect to the relay server

By default, the server listens on `ws://localhost:8080/ws`, point your clients at it.

and then you can create rooms dynamically through the url definition

```
ws://localhost:8080/ws?room=<dynamic_room_id>&client_id=<dynamic_client_id>
```

example:

run the cli client:
```
python cli_chat.py -u Alice -r test_room

python cli_chat.py -u Bob -r test_room
```

run the web client:
```
python3 -m http.server 3000
```
