package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const echoServerUrl = "wss://echo.websocket.org"

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // In production, check origin properly
	},
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

type ProxyServer struct {
	clients             map[*Client]bool
	register            chan *Client
	unregister          chan *Client
	broadcast           chan []byte
	broadcastEchoServer chan []byte
	thirdPartyConn      *websocket.Conn
	mu                  sync.Mutex
}

func NewProxyServer() *ProxyServer {
	return &ProxyServer{
		clients:             make(map[*Client]bool),
		register:            make(chan *Client),
		unregister:          make(chan *Client),
		broadcast:           make(chan []byte),
		broadcastEchoServer: make(chan []byte),
	}
}

func (ps *ProxyServer) Run() {
	for {
		select {
		case client := <-ps.register:
			ps.clients[client] = true
			log.Println("Client connected")

		case client := <-ps.unregister:
			if _, ok := ps.clients[client]; ok {
				delete(ps.clients, client)
				close(client.send)
				client.conn.Close()
				log.Println("Client disconnected")
			}
		case message := <-ps.broadcast:
			for client := range ps.clients {
				select {
				case client.send <- message:
				default:
					ps.unregister <- client
				}
			}
		}
	}
}

func (ps *ProxyServer) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	// add new client to server
	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}

	ps.register <- client

	go ps.clientReadLoop(client)
	go ps.clientWriteLoop(client)
}

func (ps *ProxyServer) clientReadLoop(client *Client) {
	defer func() {
		ps.unregister <- client
	}()

	for {
		_, msg, err := client.conn.ReadMessage()
		if err != nil {
			log.Println("Client read error:", err)
			break
		}

		ps.broadcastEchoServer <- msg
	}
}

func (ps *ProxyServer) clientWriteLoop(client *Client) {
	for msg := range client.send {
		err := client.conn.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			log.Println("Client write error:", err)
			break
		}
	}
}

func (ps *ProxyServer) connectToEchoServer() {
	for {
		conn, _, err := websocket.DefaultDialer.Dial(echoServerUrl, nil)
		if err != nil {
			log.Println("Dial error:", err)
			time.Sleep(5 * time.Second) // retry
			continue
		}

		ps.setThirdPartyConn(conn)
		log.Println("Connected to third-party WebSocket")

		go ps.readFromThirdParty(conn)
		go ps.writeToThirdParty(conn)

		break // Exit loop once connected
	}
}

func (ps *ProxyServer) setThirdPartyConn(conn *websocket.Conn) {
	ps.mu.Lock()
	ps.thirdPartyConn = conn
	ps.mu.Unlock()
}

func (ps *ProxyServer) readFromThirdParty(conn *websocket.Conn) {
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Third-party read error:", err)
			break
		}

		ps.broadcast <- msg
	}
}

func (ps *ProxyServer) writeToThirdParty(conn *websocket.Conn) {
	for msg := range ps.broadcastEchoServer {
		err := conn.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			log.Println("Third-party write error:", err)
			break
		}
	}
}

func main() {
	server := NewProxyServer()
	go server.Run()
	go server.connectToEchoServer()

	http.HandleFunc("/ws", server.wsHandler)

	fmt.Println("websocket server is starting at :1234")
	err := http.ListenAndServe(":1234", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
