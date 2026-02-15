package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	server     *Server
	clients    map[*websocket.Conn]*wsClient
	register   chan *wsClient
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

type wsClient struct {
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	clientID   string
	playerName string
	sessionID  string
	roomID     string
	resumed    bool
}

func NewHub(server *Server) *Hub {
	return &Hub{
		server:     server,
		clients:    make(map[*websocket.Conn]*wsClient),
		register:   make(chan *wsClient),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.conn] = client
			h.mu.Unlock()

			h.server.AddClient(&Client{
				ID:        client.clientID,
				Name:      client.playerName,
				SessionID: client.sessionID,
			}, client.roomID)

			// Set owner for new scheduled rooms if unset
			room := h.server.GetRoom(client.roomID)
			if room != nil {
				room.mu.Lock()
				if room.roomType == RoomTypeScheduled && room.ownerID == "" {
					room.ownerID = client.clientID
				}
				room.mu.Unlock()
			}

			// Send client their ID first
			idMsg := map[string]interface{}{
				"type":      "your_id",
				"client_id": client.clientID,
				"resumed":   client.resumed,
				"name":      client.playerName,
				"room_id":   client.roomID,
			}
			idJSON, err := json.Marshal(idMsg)
			if err == nil {
				client.send <- idJSON
			}

			// Broadcast updated state to clients in the same room
			h.BroadcastStateTo(client.roomID)

		case conn := <-h.unregister:
			h.mu.Lock()
			if client, ok := h.clients[conn]; ok {
				roomID := client.roomID
				delete(h.clients, conn)
				close(client.send)
				h.server.RemoveClient(client.clientID, roomID)
				h.mu.Unlock()
				h.BroadcastStateTo(roomID)
				h.server.CleanupRoomIfEmpty(roomID)
			} else {
				h.mu.Unlock()
			}
		}
	}
}

// sendToRoom sends a JSON message to all clients in the given room.
func (h *Hub) sendToRoom(roomID string, msgJSON []byte) {
	h.mu.RLock()
	// Collect clients to send to
	clients := make([]*wsClient, 0)
	for _, client := range h.clients {
		if client.roomID == roomID {
			clients = append(clients, client)
		}
	}
	h.mu.RUnlock()

	// Track clients that need to be removed
	var toRemove []*wsClient
	for _, client := range clients {
		select {
		case client.send <- msgJSON:
		default:
			close(client.send)
			toRemove = append(toRemove, client)
		}
	}

	// Remove dead clients with write lock
	if len(toRemove) > 0 {
		h.mu.Lock()
		for _, client := range toRemove {
			delete(h.clients, client.conn)
		}
		h.mu.Unlock()
	}
}

// SendToClient sends a message to a specific client by their clientID.
func (h *Hub) SendToClient(clientID string, msgJSON []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		if client.clientID == clientID {
			select {
			case client.send <- msgJSON:
			default:
			}
			return
		}
	}
}

// DisconnectClient forcibly closes a client's connection (used after kick).
func (h *Hub) DisconnectClient(clientID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		if client.clientID == clientID {
			client.conn.Close()
			return
		}
	}
}

func (h *Hub) BroadcastStateTo(roomID string) {
	state := h.server.GetState(roomID)
	msg := map[string]interface{}{
		"type": "state",
		"data": state,
	}
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.sendToRoom(roomID, msgJSON)
}

func (h *Hub) BroadcastEventTo(roomID string, playerName, action, result string) {
	msg := map[string]interface{}{
		"type": "event",
		"data": map[string]interface{}{
			"player": playerName,
			"action": action,
			"result": result,
		},
	}
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.sendToRoom(roomID, msgJSON)
}

func (h *Hub) BroadcastChatTo(roomID string, playerName, message string) {
	msg := map[string]interface{}{
		"type": "chat",
		"data": map[string]interface{}{
			"player":  playerName,
			"message": message,
		},
	}
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.sendToRoom(roomID, msgJSON)
}

func validatePlayerName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("player name cannot be empty")
	}
	if len(name) > 20 {
		name = name[:20]
	}
	// Remove control characters and trim again
	name = strings.TrimFunc(name, func(r rune) bool {
		return r < 32 || r == 127
	})
	if name == "" {
		return "", fmt.Errorf("player name cannot be empty")
	}
	return name, nil
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	playerName := r.URL.Query().Get("name")
	if playerName == "" {
		playerName = "Player"
	}

	// Validate player name
	validatedName, err := validatePlayerName(playerName)
	if err != nil {
		http.Error(w, "Invalid player name: "+err.Error(), http.StatusBadRequest)
		return
	}
	playerName = validatedName

	roomID := r.URL.Query().Get("room")
	password := r.URL.Query().Get("password")

	var sessionID string
	var resumed bool
	clientID := fmt.Sprintf("player-%d", time.Now().UnixNano())

	// Check for existing session via cookie
	cookie, err := r.Cookie("session_id")
	if err == nil {
		if sess, ok := hub.server.sessionManager.GetSessionByID(cookie.Value); ok {
			sessionID = sess.ID
			playerName = sess.Name
			// Use the stored clientID for session resumption to preserve game state
			if sess.ClientID != "" {
				clientID = sess.ClientID
			}
			if sess.RoomID != "" {
				roomID = sess.RoomID
			}
			resumed = true
			log.Printf("Session resumed for %s in room %s", playerName, roomID)
		}
	}

	// Default to continuous if no room specified
	if roomID == "" {
		roomID = "continuous"
	}

	// Validate room exists
	room := hub.server.GetRoom(roomID)
	if room == nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	// Check password (skip for resumed sessions)
	if !resumed && room.password != "" && password != room.password {
		http.Error(w, "Wrong password", http.StatusForbidden)
		return
	}

	// Check if player died and is banned from rejoining
	if hub.server.IsPlayerBanned(roomID, playerName) {
		http.Error(w, "Your party perished in this game. Wait for the game to reset before rejoining.", http.StatusForbidden)
		return
	}

	// Check max players
	if room.maxPlayers > 0 {
		room.mu.RLock()
		count := len(room.clients)
		room.mu.RUnlock()
		if count >= room.maxPlayers && !resumed {
			http.Error(w, "Room is full", http.StatusConflict)
			return
		}
	}

	// Preflight check — return OK without upgrading
	if r.URL.Query().Get("preflight") == "1" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if sessionID == "" {
		sessionID = hub.server.sessionManager.CreateSession(playerName, clientID, roomID)
	} else {
		hub.server.sessionManager.UpdateClient(sessionID, clientID)
		hub.server.sessionManager.UpdateRoomID(sessionID, roomID)
	}

	// Pass cookie via Upgrade's responseHeader
	sessionCookie := &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		MaxAge:   86400 * 30,
	}
	upgradeHeaders := http.Header{}
	upgradeHeaders.Add("Set-Cookie", sessionCookie.String())

	conn, err := upgrader.Upgrade(w, r, upgradeHeaders)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	client := &wsClient{
		hub:        hub,
		conn:       conn,
		send:       make(chan []byte, 256),
		clientID:   clientID,
		playerName: playerName,
		sessionID:  sessionID,
		roomID:     roomID,
		resumed:    resumed,
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *wsClient) readPump() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WebSocket readPump recovered from panic: %v", r)
		}
		c.hub.unregister <- c.conn
		c.conn.Close()
	}()

	c.conn.SetReadLimit(1024)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		msgType, ok := msg["type"].(string)
		if !ok {
			continue
		}

		roomID := c.roomID

		switch msgType {
		case "action":
			action, ok := msg["action"].(string)
			if !ok {
				break
			}
			result := c.hub.server.HandleAction(c.clientID, roomID, action)

			c.hub.BroadcastEventTo(roomID, c.playerName, action, result)
			c.hub.BroadcastStateTo(roomID)

		case "chat":
			message, ok := msg["message"].(string)
			if !ok {
				break
			}
			if len(message) > 200 {
				message = message[:200]
			}
			if message != "" {
				c.hub.BroadcastChatTo(roomID, c.playerName, message)
			}

		case "logout":
			c.hub.server.LogoutClient(c.clientID, c.sessionID, roomID)
			c.hub.BroadcastStateTo(roomID)
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return

		case "fort_enter":
			result := c.hub.server.HandleFortEnter(c.clientID, roomID)
			c.hub.BroadcastEventTo(roomID, c.playerName, "fort", result)
			c.hub.BroadcastStateTo(roomID)

		case "fort_buy":
			item, ok := msg["item"].(string)
			if !ok {
				break
			}
			qtyFloat, ok := msg["qty"].(float64)
			if !ok {
				break
			}
			qty := int(qtyFloat)
			result := c.hub.server.HandleFortBuy(c.clientID, roomID, item, qty)
			c.hub.BroadcastEventTo(roomID, c.playerName, "fort", result)
			c.hub.BroadcastStateTo(roomID)

		case "fort_sell":
			item, ok := msg["item"].(string)
			if !ok {
				break
			}
			qtyFloat, ok := msg["qty"].(float64)
			if !ok {
				break
			}
			qty := int(qtyFloat)
			result := c.hub.server.HandleFortSell(c.clientID, roomID, item, qty)
			c.hub.BroadcastEventTo(roomID, c.playerName, "fort", result)
			c.hub.BroadcastStateTo(roomID)

		case "fort_leave":
			result := c.hub.server.HandleFortLeave(c.clientID, roomID)
			c.hub.BroadcastEventTo(roomID, c.playerName, "fort", result)
			c.hub.BroadcastStateTo(roomID)

		case "loot_claim":
			lootSiteID, ok := msg["loot_site_id"].(string)
			if !ok {
				break
			}
			result := c.hub.server.HandleLootClaim(c.clientID, roomID, lootSiteID)
			c.hub.BroadcastEventTo(roomID, c.playerName, "loot", result)
			c.hub.BroadcastStateTo(roomID)

		case "reset":
			if c.hub.server.ResetGame(roomID) {
				c.hub.BroadcastEventTo(roomID, "System", "reset", "A new journey begins! The wagon train is restocked and ready.")
				c.hub.BroadcastStateTo(roomID)
			}

		case "hunt_shoot":
			timeFloat, ok := msg["time"].(float64)
			if !ok {
				break
			}
			reactionTimeMs := int(timeFloat)
			result := c.hub.server.HandleHuntShoot(c.clientID, roomID, reactionTimeMs)
			c.hub.BroadcastEventTo(roomID, c.playerName, "hunt", result)
			c.hub.BroadcastStateTo(roomID)

		case "rider_tactic":
			tacticFloat, ok := msg["tactic"].(float64)
			if !ok {
				break
			}
			tactic := int(tacticFloat)
			result := c.hub.server.HandleRiderTactic(c.clientID, roomID, tactic)
			c.hub.BroadcastEventTo(roomID, c.playerName, "continue", result)
			c.hub.BroadcastStateTo(roomID)

		case "kick":
			targetID, ok := msg["target_id"].(string)
			if !ok || targetID == "" {
				break
			}
			// Send kicked message to target before removing
			kickMsg, err := json.Marshal(map[string]interface{}{
				"type":   "kicked",
				"reason": "You have been removed from the game by the lobby owner.",
			})
			if err == nil {
				c.hub.SendToClient(targetID, kickMsg)
			}

			if c.hub.server.KickClient(roomID, c.clientID, targetID) {
				// Disconnect the kicked client
				c.hub.DisconnectClient(targetID)
				c.hub.BroadcastStateTo(roomID)
			}
		}
	}
}

func (c *wsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		if r := recover(); r != nil {
			log.Printf("WebSocket writePump recovered from panic: %v", r)
		}
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		http.ServeFile(w, r, "static/index.html")
		return
	}
	http.ServeFile(w, r, r.URL.Path[1:])
}
