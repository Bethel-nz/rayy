import websockets
import json
import asyncio
import time
import sys
import threading
from datetime import datetime
import argparse
import os

class ChatClient:
    def __init__(self, username, room_id):
        self.username = username
        self.room_id = room_id
        self.uri = f"ws://localhost:8080/ws?room={room_id}&client_id={username}"
        self.websocket = None
        self.running = False
        self.typing_timer = None
        self.event_handlers = {}

    def clear_screen(self):
        os.system('cls' if os.name == 'nt' else 'clear')

    def print_header(self):
        self.clear_screen()
        print(f"""
╭{'─' * 50}╮
│{'Chat Room'.center(50)}│
│{'─' * 50}│
│{f'Room: {self.room_id}'.center(50)}│
│{f'Username: {self.username}'.center(50)}│
╰{'─' * 50}╯
""")

    def format_message(self, message):
        timestamp = datetime.fromtimestamp(time.time()).strftime('%H:%M:%S')
        if message['from'] == self.username:
            return f"\033[94m[{timestamp}] You: {message['content']}\033[0m"
        else:
            return f"\033[92m[{timestamp}] {message['from']}: {message['content']}\033[0m"

    async def receive_messages(self):
        try:
            while self.running:
                message = await self.websocket.recv()
                print(f"\rDEBUG: Received raw message: {message}")  # Debug line
                message_obj = json.loads(message)
                self.handle_event(message_obj)
        except websockets.exceptions.ConnectionClosed:
            print("\nConnection closed by server")
            self.running = False
        except Exception as e:
            print(f"\nError receiving message: {e}")
            self.running = False

    async def send_message(self, content):
        if not content.strip():
            return
        
        message = {
            "action": "message",
            "content": content.strip(),
            "room": self.room_id,
            "from": self.username
        }
        try:
            await self.websocket.send(json.dumps(message))
        except Exception as e:
            print(f"\nError sending message: {e}")

    def register_event(self, event_name, handler):
        """Register a handler for custom events"""
        self.event_handlers[event_name] = handler

    def handle_event(self, message):
        """Handle incoming events"""
        try:
            if message.get('action') == 'event':
                event_type = message.get('event')
                if event_type in self.event_handlers:
                    self.event_handlers[event_type](message)
                    print("\r> ", end="", flush=True)
            elif message.get('action') == 'message':
                print(f"\r{self.format_message(message)}")
                print("\r> ", end="", flush=True)
        except Exception as e:
            print(f"\nError handling event: {e}")

    async def send_event(self, event_name, data=None):
        """Send a custom event"""
        message = {
            "action": "event",
            "event": event_name,
            "room": self.room_id,
            "from": self.username,
            "data": data or {}
        }
        try:
            await self.websocket.send(json.dumps(message))
        except Exception as e:
            print(f"\nError sending event: {e}")

    async def notify_typing(self, is_typing=True):
        """Send typing notification"""
        await self.send_event("typing", {
            "is_typing": is_typing,
            "username": self.username
        })

    def handle_typing_event(self, message):
        """Handle typing notifications"""
        data = message.get('data', {})
        username = data.get('username')
        is_typing = data.get('is_typing')
        
        if username != self.username:
            if is_typing:
                print(f"\r\033[93m{username} is typing...\033[0m")
            print("\r> ", end="", flush=True)

    async def get_user_input(self):
        while self.running:
            try:
                # Create input prompt
                message = await asyncio.get_event_loop().run_in_executor(
                    None, lambda: input("\r> ")
                )

                # Handle commands
                if message.lower() in ['/quit', '/exit']:
                    await self.send_event("leave")
                    self.running = False
                    break
                elif message.lower() == '/clear':
                    self.print_header()
                    continue
                elif message.lower() == '/users':
                    await self.send_event("list_users")
                    continue
                
                # Send typing notification
                await self.notify_typing(False)
                # Send actual message
                await self.send_message(message)

            except Exception as e:
                print(f"\nError sending message: {e}")
                self.running = False
                break

    async def run(self):
        try:
            self.print_header()
            print("\nConnecting to chat server...")
            
            # Register event handlers
            self.register_event("typing", self.handle_typing_event) # tried handling this,might work in web version
            self.register_event("join", lambda m: print(f"\r\033[95m{m['from']} joined the room\033[0m"))
            self.register_event("leave", lambda m: print(f"\r\033[95m{m['from']} left the room\033[0m"))
            self.register_event("users", lambda m: print(f"\rUsers in room: {', '.join(m['data']['users'])}"))

            async with websockets.connect(self.uri) as websocket:
                self.websocket = websocket
                self.running = True
                print("Connected! Available commands:")
                print("/quit - Exit chat")
                print("/clear - Clear screen")
                print("/users - List users in room")
                
                # Send join event
                await self.send_event("join")
                
                # Start message receiver
                receiver = asyncio.create_task(self.receive_messages())
                # Start user input handler
                sender = asyncio.create_task(self.get_user_input())
                
                # Handle typing events
                async def typing_handler():
                    while self.running:
                        if self.typing_timer:
                            await self.notify_typing(True)
                        await asyncio.sleep(1)
                
                typing = asyncio.create_task(typing_handler())
                
                # Wait for tasks to complete
                await asyncio.gather(receiver, sender, typing)
                
        except Exception as e:
            print(f"Error: {e}")
        finally:
            self.running = False
            print("\nDisconnected from chat server")

def main():
    parser = argparse.ArgumentParser(description='CLI Chat Client')
    parser.add_argument('--username', '-u', help='Your username', required=True)
    parser.add_argument('--room', '-r', help='Room ID to join', required=True)
    args = parser.parse_args()

    client = ChatClient(args.username, args.room)
    asyncio.run(client.run())

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nChat client stopped by user") 