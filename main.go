package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Constants
const (
	screenWidth  = 800
	screenHeight = 600
)

// Player represents a player's character
type Player struct {
	ID        string  `json:"id"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	VelocityY float64 `json:"velocityY"`
	IsJumping bool    `json:"isJumping"`
	Score     int     `json:"score"`
	Color     string  `json:"color"`
}

// Coin represents a collectible coin
type Coin struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Active bool    `json:"active"`
}

// Platform represents a static platform
type Platform struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Game represents the state of the game
type Game struct {
	mu          sync.Mutex
	Players     map[string]*Player
	Coins       map[string]*Coin
	Platforms   []Platform
	Connections map[string]*websocket.Conn
}

var game = Game{
	Players: make(map[string]*Player),
	Coins:   make(map[string]*Coin),
	Platforms: []Platform{
		{X: 0, Y: screenHeight - 50, Width: screenWidth, Height: 50}, // Ground
		{X: 300, Y: 400, Width: 200, Height: 20},                     // Floating platform
		{X: 100, Y: 300, Width: 150, Height: 20},                     // Another platform
	},
	Connections: make(map[string]*websocket.Conn),
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Initialize coins with central Y position
func init() {
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < 10; i++ {
		game.Coins[fmt.Sprintf("coin%d", i)] = &Coin{
			ID:     fmt.Sprintf("coin%d", i),
			X:      float64(50 + i*70),
			Y:      float64(screenHeight/2 + rand.Float64()*100 - 50), // Center around Y=300
			Active: true,
		}
	}
}

// Broadcast game state
func broadcastGameState() {
	game.mu.Lock()
	defer game.mu.Unlock()

	// Ensure all platforms have valid values
	for i, platform := range game.Platforms {
		if platform.X == 0 && platform.Y == 0 && platform.Width == 0 && platform.Height == 0 {
			game.Platforms[i] = Platform{X: 0, Y: screenHeight - 50, Width: screenWidth, Height: 50}
		}
	}

	data, err := json.Marshal(struct {
		Players   map[string]*Player `json:"players"`
		Coins     map[string]*Coin   `json:"coins"`
		Platforms []Platform         `json:"platforms"`
	}{
		Players:   game.Players,
		Coins:     game.Coins,
		Platforms: game.Platforms,
	})
	if err != nil {
		log.Println("Failed to encode game state:", err)
		return
	}

	for playerID, conn := range game.Connections {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("Failed to send to %s: %v", playerID, err)
		}
	}
}

// Handle WebSocket connections
func handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket Upgrade Error:", err)
		return
	}

	playerID := r.URL.Query().Get("id")
	if playerID == "" {
		log.Println("Missing player ID")
		conn.Close()
		return
	}

	game.mu.Lock()
	game.Players[playerID] = &Player{
		ID:        playerID,
		X:         50,
		Y:         screenHeight - 70, // Start on ground
		VelocityY: 0,
		IsJumping: false,
		Score:     0,
		Color:     randomColor(),
	}
	game.Connections[playerID] = conn
	game.mu.Unlock()

	log.Println("Player connected:", playerID)
	broadcastGameState()

	defer func() {
		game.mu.Lock()
		delete(game.Players, playerID)
		delete(game.Connections, playerID)
		game.mu.Unlock()
		conn.Close()
		log.Println("Player disconnected:", playerID)
		broadcastGameState()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Connection closed for", playerID, ":", err)
			break
		}

		var playerUpdate Player
		if err := json.Unmarshal(msg, &playerUpdate); err != nil {
			log.Println("Invalid message from", playerID, ":", err)
			continue
		}

		game.mu.Lock()
		if player, ok := game.Players[playerID]; ok {
			// Ensure valid coordinates
			if playerUpdate.X == 0 && playerUpdate.Y == 0 {
				playerUpdate.X = player.X
				playerUpdate.Y = player.Y
			}
			player.X = playerUpdate.X
			player.Y = playerUpdate.Y
			player.VelocityY = playerUpdate.VelocityY
			player.IsJumping = playerUpdate.IsJumping
			player.Score = playerUpdate.Score
		}
		game.mu.Unlock()

		broadcastGameState()
	}
}

// Handle game state HTTP endpoint
func handleGameState(w http.ResponseWriter, r *http.Request) {
	game.mu.Lock()
	defer game.mu.Unlock()

	data, err := json.Marshal(struct {
		Players   map[string]*Player `json:"players"`
		Coins     map[string]*Coin   `json:"coins"`
		Platforms []Platform         `json:"platforms"`
	}{
		Players:   game.Players,
		Coins:     game.Coins,
		Platforms: game.Platforms,
	})
	if err != nil {
		http.Error(w, "Failed to encode game state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// Random color
func randomColor() string {
	colors := []string{"red", "blue", "green", "purple", "orange"}
	return colors[rand.Intn(len(colors))]
}

func main() {
	rand.Seed(time.Now().UnixNano())
	http.Handle("/", http.FileServer(http.Dir(".")))
	http.HandleFunc("/ws", handleConnection)
	http.HandleFunc("/state", handleGameState)
	log.Println("Server started on :8080")
	go func() {
		for {
			time.Sleep(100 * time.Millisecond) // Update every 100ms
			broadcastGameState()
		}
	}()
	log.Fatal(http.ListenAndServe(":8080", nil))
}
