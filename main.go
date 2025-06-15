package main

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Upgrader is used to upgrade HTTP connections to WebSocket connections.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var clients = make(map[*websocket.Conn]bool)
var chanBroadcast = make(chan []byte)
var mu = sync.Mutex{}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("failed to upgrade", err)
		return
	}

	defer conn.Close()

	mu.Lock()
	clients[conn] = true
	mu.Unlock()

	// broadcast messages
	for {
		// Read message from the client
		_, message, err := conn.ReadMessage()
		if err != nil {
			mu.Lock()
			delete(clients, conn)
			mu.Unlock()
			break
		}

		chanBroadcast <- message
	}
}

func handleConnection() {
	// Listen for incoming messages
	for {
		// Grab the next message from the broadcast channel
		message := <-chanBroadcast

		// Send the message to all connected clients
		mu.Lock()
		for client := range clients {
			if err := client.WriteMessage(websocket.TextMessage, message); err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		mu.Unlock()
	}
}

func main() {
	http.HandleFunc("/ws", wsHandler)
	go handleConnection()

	fmt.Println("websocket server is starting at :1234")
	err := http.ListenAndServe(":1234", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
