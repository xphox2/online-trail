package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"online-trail/pkg/game"
)

type RoomType string

const (
	RoomTypeContinuous RoomType = "continuous"
	RoomTypeScheduled  RoomType = "scheduled"
)

type GameStatus string

const (
	StatusWaiting  GameStatus = "waiting"
	StatusPlaying  GameStatus = "playing"
	StatusFinished GameStatus = "finished"
)

type GameRoom struct {
	id           string
	name         string
	roomType     RoomType
	status       GameStatus
	password     string
	ownerID      string
	maxPlayers   int
	createdAt    time.Time
	game         *game.GameState
	clients      map[string]*Client
	deadPlayers  map[string]bool // names banned from rejoining until reset
	turnTimer    *time.Timer
	turnDeadline time.Time
	mu           sync.RWMutex
}

type LobbyInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RoomType      string `json:"room_type"`
	PlayerCount   int    `json:"player_count"`
	MaxPlayers    int    `json:"max_players"`
	HasPassword   bool   `json:"has_password"`
	Status        string `json:"status"`
	OwnerID       string `json:"owner_id"`
	LootSiteCount int    `json:"loot_site_count"`
}

type Server struct {
	rooms          map[string]*GameRoom
	roomsMu        sync.RWMutex
	sessionManager *SessionManager
	leaderboard    *Leaderboard
	hub            *Hub
}

type Client struct {
	ID        string
	Name      string
	Player    *game.Player
	SessionID string
	RoomID    string
}

const roomIDChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func generateRoomID() string {
	b := make([]byte, 6)
	for i := range b {
		b[i] = roomIDChars[rand.Intn(len(roomIDChars))]
	}
	return string(b)
}

func NewGameRoom(id, name string, roomType RoomType) *GameRoom {
	return &GameRoom{
		id:          id,
		name:        name,
		roomType:    roomType,
		status:      StatusWaiting,
		createdAt:   time.Now(),
		game:        game.NewGameState(),
		clients:     make(map[string]*Client),
		deadPlayers: make(map[string]bool),
	}
}

func NewServer(dataPath string) *Server {
	s := &Server{
		rooms:          make(map[string]*GameRoom),
		sessionManager: NewSessionManager(),
		leaderboard:    NewLeaderboard(dataPath),
	}
	// Create the permanent continuous room
	continuous := NewGameRoom("continuous", "The Open Trail", RoomTypeContinuous)
	s.rooms["continuous"] = continuous
	return s
}

func initRoomResources(room *GameRoom) {
	room.game.OxenCost = 220
	room.game.Food = 100
	room.game.Bullets = 50
	room.game.Clothing = 20
	room.game.MiscSupplies = 10
	room.game.Cash = 700
	room.game.GameOver = false
	room.game.Win = false
	room.game.CurrentPlayerIdx = 0
}

func (s *Server) GetRoom(roomID string) *GameRoom {
	s.roomsMu.RLock()
	defer s.roomsMu.RUnlock()
	room, ok := s.rooms[roomID]
	if !ok {
		return nil
	}
	return room
}

func (s *Server) IsPlayerBanned(roomID, name string) bool {
	room := s.GetRoom(roomID)
	if room == nil {
		return false
	}
	room.mu.RLock()
	defer room.mu.RUnlock()
	return room.deadPlayers[name]
}

func (s *Server) FindRoomForClient(clientID string) *GameRoom {
	s.roomsMu.RLock()
	defer s.roomsMu.RUnlock()
	for _, room := range s.rooms {
		room.mu.RLock()
		_, ok := room.clients[clientID]
		room.mu.RUnlock()
		if ok {
			return room
		}
	}
	return nil
}

func (s *Server) CreateRoom(name, password, ownerID string, maxPlayers int) *GameRoom {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()

	// Generate unique room ID
	var id string
	for {
		id = generateRoomID()
		if _, exists := s.rooms[id]; !exists {
			break
		}
	}

	room := NewGameRoom(id, name, RoomTypeScheduled)
	room.password = password
	room.ownerID = ownerID
	room.maxPlayers = maxPlayers
	s.rooms[id] = room
	log.Printf("Room created: %s (%s) by %s", name, id, ownerID)
	return room
}

func (s *Server) ListLobbies() []LobbyInfo {
	s.roomsMu.RLock()
	defer s.roomsMu.RUnlock()

	lobbies := make([]LobbyInfo, 0, len(s.rooms))
	for _, room := range s.rooms {
		room.mu.RLock()
		var lootCount int
		if room.roomType == RoomTypeContinuous && room.game != nil {
			lootCount = len(room.game.LootSites)
		}
		info := LobbyInfo{
			ID:            room.id,
			Name:          room.name,
			RoomType:      string(room.roomType),
			PlayerCount:   len(room.clients),
			MaxPlayers:    room.maxPlayers,
			HasPassword:   room.password != "",
			Status:        string(room.status),
			OwnerID:       room.ownerID,
			LootSiteCount: lootCount,
		}
		room.mu.RUnlock()
		lobbies = append(lobbies, info)
	}
	return lobbies
}

func (s *Server) AddClient(c *Client, roomID string) {
	room := s.GetRoom(roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c.RoomID = roomID
	room.clients[c.ID] = c

	if c.SessionID != "" {
		s.sessionManager.UpdateClient(c.SessionID, c.ID)
	}

	// In scheduled mode, only add player if game is waiting
	if room.roomType == RoomTypeScheduled && room.status != StatusWaiting {
		log.Printf("Player %s tried to join %s but game already started", c.Name, roomID)
		return
	}

	// Check if player already exists in game (for reconnections by ID or name)
	var existingPlayer *game.Player
	for _, p := range room.game.Players {
		if p.ID == c.ID || p.Name == c.Name {
			existingPlayer = p
			break
		}
	}

	if existingPlayer != nil {
		existingPlayer.ID = c.ID
		c.Player = existingPlayer
		log.Printf("Player %s reconnected to %s (ID: %s)", c.Name, roomID, c.ID)
	} else {
		player := room.game.AddPlayer(c.Name, game.PlayerTypeHuman)
		player.ID = c.ID
		c.Player = player

		// If this is the first player in continuous mode, auto-start
		if room.roomType == RoomTypeContinuous && room.game.TurnNumber == 0 && len(room.clients) == 1 {
			initRoomResources(room)
			room.status = StatusPlaying
		}
	}

	if room.game.GetCurrentPlayer() == nil && len(room.game.Players) > 0 {
		room.game.CurrentPlayerIdx = 0
	}

	log.Printf("Player %s joined %s (ID: %s)", c.Name, roomID, c.ID)
}

func (s *Server) RemoveClient(clientID string, roomID string) {
	room := s.GetRoom(roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	if c, ok := room.clients[clientID]; ok {
		delete(room.clients, clientID)
		// Transfer ownership if the leaving client is the owner
		if room.ownerID == clientID && len(room.clients) > 0 {
			for _, next := range room.clients {
				room.ownerID = next.ID
				log.Printf("Ownership of room %s transferred to %s", roomID, next.Name)
				break
			}
		}
		log.Printf("Player %s disconnected from %s", c.Name, roomID)
	}
}

func (s *Server) LogoutClient(clientID, sessionID string, roomID string) {
	room := s.GetRoom(roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	if c, ok := room.clients[clientID]; ok {
		wasCurrentPlayer := false
		if cp := room.game.GetCurrentPlayer(); cp != nil && cp.ID == clientID {
			wasCurrentPlayer = true
		}
		for i, p := range room.game.Players {
			if p.ID == clientID {
				// If the player is dead and the game is in progress, reduce maxPlayers
				// In continuous mode, allow dead players to rejoin as fresh players
				// In private mode, ban them until the game resets
				if !p.Alive && room.status == StatusPlaying {
					if room.maxPlayers > 0 {
						room.maxPlayers--
					}
					if room.roomType != RoomTypeContinuous {
						room.deadPlayers[c.Name] = true
						log.Printf("Dead player %s logged out, banned from rejoining room %s until reset", c.Name, roomID)
					} else {
						log.Printf("Dead player %s logged out of continuous room %s, can rejoin as fresh player", c.Name, roomID)
					}
				}
				room.game.Players = append(room.game.Players[:i], room.game.Players[i+1:]...)
				if room.game.CurrentPlayerIdx >= len(room.game.Players) && len(room.game.Players) > 0 {
					room.game.CurrentPlayerIdx = 0
				}
				break
			}
		}
		delete(room.clients, clientID)
		// Transfer ownership if the leaving client is the owner
		if room.ownerID == clientID && len(room.clients) > 0 {
			for _, next := range room.clients {
				room.ownerID = next.ID
				log.Printf("Ownership of room %s transferred to %s", roomID, next.Name)
				break
			}
		}
		// If the leaving player was the current turn holder, reset phase and start timer for new current player
		if wasCurrentPlayer && room.status == StatusPlaying && !room.game.GameOver {
			room.game.TurnPhase = game.PhaseMainMenu
			if np := room.game.GetCurrentPlayer(); np != nil && np.Alive {
				s.StartTurnTimer(room, np.ID)
			}
		}
		log.Printf("Player %s logged out of %s", c.Name, roomID)
	}

	s.sessionManager.InvalidateSession(sessionID)
}

func (s *Server) KickClient(roomID, requesterID, targetID string) bool {
	room := s.GetRoom(roomID)
	if room == nil {
		return false
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	// Only the owner can kick
	if room.ownerID != requesterID {
		return false
	}
	// Can't kick yourself
	if requesterID == targetID {
		return false
	}

	if c, ok := room.clients[targetID]; ok {
		wasCurrentPlayer := false
		if cp := room.game.GetCurrentPlayer(); cp != nil && cp.ID == targetID {
			wasCurrentPlayer = true
		}
		for i, p := range room.game.Players {
			if p.ID == targetID {
				room.game.Players = append(room.game.Players[:i], room.game.Players[i+1:]...)
				if room.game.CurrentPlayerIdx >= len(room.game.Players) && len(room.game.Players) > 0 {
					room.game.CurrentPlayerIdx = 0
				}
				break
			}
		}
		delete(room.clients, targetID)
		// If kicked player was the current turn holder, reset phase and start timer for new current player
		if wasCurrentPlayer && room.status == StatusPlaying && !room.game.GameOver {
			room.game.TurnPhase = game.PhaseMainMenu
			if np := room.game.GetCurrentPlayer(); np != nil && np.Alive {
				s.StartTurnTimer(room, np.ID)
			}
		}
		log.Printf("Player %s kicked from room %s by owner", c.Name, roomID)
		return true
	}
	return false
}

func (s *Server) CleanupRoomIfEmpty(roomID string) {
	if roomID == "continuous" {
		return
	}
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	room, ok := s.rooms[roomID]
	if !ok {
		return
	}
	room.mu.RLock()
	empty := len(room.clients) == 0
	room.mu.RUnlock()
	if empty {
		delete(s.rooms, roomID)
		log.Printf("Room %s (%s) cleaned up (empty)", room.name, roomID)
	}
}

func (s *Server) CleanupStaleRooms() {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	now := time.Now()
	for id, room := range s.rooms {
		if id == "continuous" {
			continue
		}
		room.mu.RLock()
		empty := len(room.clients) == 0
		status := room.status
		created := room.createdAt
		room.mu.RUnlock()

		// Remove empty rooms
		if empty {
			delete(s.rooms, id)
			log.Printf("Stale room %s (%s) cleaned up (empty)", room.name, id)
			continue
		}
		// Remove finished rooms older than 10 minutes
		if status == StatusFinished && now.Sub(created) > 10*time.Minute {
			delete(s.rooms, id)
			log.Printf("Stale room %s (%s) cleaned up (finished)", room.name, id)
			continue
		}
		// Remove waiting rooms older than 24 hours
		if status == StatusWaiting && now.Sub(created) > 24*time.Hour {
			delete(s.rooms, id)
			log.Printf("Stale room %s (%s) cleaned up (stale waiting)", room.name, id)
			continue
		}
	}
}

func (s *Server) ResetGame(roomID string) bool {
	room := s.GetRoom(roomID)
	if room == nil {
		return false
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	if !room.game.GameOver {
		return false
	}

	if room.roomType == RoomTypeContinuous && !room.game.Win {
		return false
	}

	s.CancelTurnTimer(room)
	room.game.ResetGame()
	room.status = StatusWaiting
	room.deadPlayers = make(map[string]bool)

	for _, c := range room.clients {
		player := room.game.AddPlayer(c.Name, game.PlayerTypeHuman)
		player.ID = c.ID
		c.Player = player
	}

	// Reset current player index to ensure it points to a valid player
	room.game.CurrentPlayerIdx = 0

	if len(room.clients) > 0 {
		initRoomResources(room)
		room.status = StatusPlaying
	}

	log.Printf("Game reset in %s - new journey starting", roomID)
	return true
}

// createLootSite creates a loot site when a player dies in 24/7 continuous mode
func (s *Server) createLootSite(room *GameRoom, c *Client) {
	if c.Player == nil {
		return
	}
	if room == nil || room.game == nil {
		return
	}

	lootSite := game.LootSite{
		ID:           fmt.Sprintf("loot-%s-%d", c.ID, time.Now().Unix()),
		Mileage:      room.game.Mileage,
		PlayerName:   c.Name,
		Food:         room.game.Food,
		Bullets:      room.game.Bullets,
		Clothing:     room.game.Clothing,
		MiscSupplies: room.game.MiscSupplies,
		Cash:         room.game.Cash,
		OxenCost:     room.game.OxenCost,
		DateCreated:  time.Now(),
		IsLooted:     false,
	}

	room.game.LootSites = append(room.game.LootSites, lootSite)
	log.Printf("Loot site created at mile %.0f for dead player %s in room %s", room.game.Mileage, c.Name, room.id)
}

// deteriorateLootSites applies decay to unlooted sites every 24 hours
func (s *Server) deteriorateLootSites() {
	s.roomsMu.RLock()
	rooms := make([]*GameRoom, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room)
	}
	s.roomsMu.RUnlock()

	for _, room := range rooms {
		if room == nil || room.game == nil {
			continue
		}
		if room.roomType != RoomTypeContinuous {
			continue
		}

		room.mu.Lock()
		for i := range room.game.LootSites {
			site := &room.game.LootSites[i]
			if site.IsLooted {
				continue
			}

			// Apply deterioration (24 hours have passed)
			site.Food *= 0.90     // 10% rot
			site.Bullets *= 0.95  // 5% damage
			site.Clothing *= 0.97 // 3% weather wear
			site.MiscSupplies *= 0.95
			site.OxenCost *= 0.98 // 2% wagon part decay
			// Cash doesn't decay
		}
		room.mu.Unlock()
	}
}

const turnTimeLimit = 20 * time.Second
const fortInterval = 3 // trade post appears every N turns

// advanceTurnAndCheckFort calls NextTurn and auto-enters fort every fortInterval turns.
// Pauses the turn timer during fort; starts it otherwise.
// Returns true if the fort was auto-triggered.
// NOTE: caller must hold room.mu.
func (s *Server) advanceTurnAndCheckFort(room *GameRoom) bool {
	room.game.NextTurn()
	np := room.game.GetCurrentPlayer()
	if np == nil || !np.Alive || room.game.GameOver {
		return false
	}
	fortTriggered := false
	if room.game.TurnNumber > 0 && room.game.TurnNumber%fortInterval == 0 {
		// Make fort available for this turn
		room.game.FortAvailable = true
		fortTriggered = true
	}
	s.StartTurnTimer(room, np.ID)
	return fortTriggered
}

// StartTurnTimer starts a turn timer for the given player.
// NOTE: caller must hold room.mu.
func (s *Server) StartTurnTimer(room *GameRoom, playerID string) {
	if room.turnTimer != nil {
		room.turnTimer.Stop()
	}
	room.turnDeadline = time.Now().Add(turnTimeLimit)
	room.turnTimer = time.AfterFunc(turnTimeLimit, func() {
		s.handleTurnTimeout(room, playerID)
	})
}

// CancelTurnTimer stops the turn timer.
// NOTE: caller must hold room.mu.
func (s *Server) CancelTurnTimer(room *GameRoom) {
	if room.turnTimer != nil {
		room.turnTimer.Stop()
		room.turnTimer = nil
	}
	room.turnDeadline = time.Time{}
}

func (s *Server) handleTurnTimeout(room *GameRoom, expectedPlayerID string) {
	// Phase 1: game logic under room lock
	room.mu.Lock()

	current := room.game.GetCurrentPlayer()
	if current == nil || current.ID != expectedPlayerID ||
		room.game.GameOver || room.status != StatusPlaying || !current.Alive {
		room.mu.Unlock()
		return
	}

	result := "Time's up! Dysentery strikes the party while they dawdle!\n"
	result += room.game.DamageRandomMember(current, 999)

	playerName := current.Name
	roomID := room.id

	// Check if player died from timeout damage (for 24/7 continuous mode)
	if !current.Alive && room.roomType == RoomTypeContinuous {
		// Find the client for this player
		for _, cl := range room.clients {
			if cl.Player != nil && cl.Player.ID == current.ID {
				s.createLootSite(room, cl)
				break
			}
		}
	}

	// Reset phase in case they timed out during fort/riders
	room.game.TurnPhase = game.PhaseMainMenu

	if room.game.GameOver {
		modeLabel := "continuous"
		if room.roomType == RoomTypeScheduled {
			modeLabel = "party"
		}
		// Add all players to leaderboard
		for _, cl := range room.clients {
			if cl.Player != nil {
				s.leaderboard.AddEntry(cl.Name, room.game.Win, room.game.Mileage, room.game.TurnNumber, modeLabel)
			}
		}
		room.status = StatusFinished
		room.turnDeadline = time.Time{}
	} else {
		s.advanceTurnAndCheckFort(room)
	}

	room.mu.Unlock()

	// Phase 2: broadcast outside of room lock to avoid deadlock
	if s.hub != nil {
		s.hub.BroadcastEventTo(roomID, playerName, "continue", result)
		s.hub.BroadcastStateTo(roomID)
	}
}

func (s *Server) GetState(roomID string) interface{} {
	room := s.GetRoom(roomID)
	if room == nil {
		return map[string]interface{}{"error": "room not found"}
	}
	room.mu.RLock()
	defer room.mu.RUnlock()

	currentPlayer := room.game.GetCurrentPlayer()
	currentPlayerID := ""
	if currentPlayer != nil {
		currentPlayerID = currentPlayer.ID
	}

	state := map[string]interface{}{
		"turn_number":       room.game.TurnNumber,
		"mileage":           room.game.Mileage,
		"food":              room.game.Food,
		"bullets":           room.game.Bullets,
		"clothing":          room.game.Clothing,
		"misc_supplies":     room.game.MiscSupplies,
		"cash":              room.game.Cash,
		"game_over":         room.game.GameOver,
		"win":               room.game.Win,
		"final_date":        room.game.FinalDate,
		"turn_phase":        room.game.TurnPhase,
		"current_player_id": currentPlayerID,
		"players":           s.getPlayerInfo(room),
		"room_id":           room.id,
		"room_name":         room.name,
		"room_type":         room.roomType,
		"owner_id":          room.ownerID,
		"game_status":       room.status,
	}

	if room.game.TurnPhase == game.PhaseFort {
		state["fort_prices"] = game.GetFortPrices()
	}

	// Always include fort availability and prices when fort is available
	if room.game.FortAvailable {
		state["fort_available"] = true
		state["fort_prices"] = game.GetFortPrices()
	}

	if room.game.TurnPhase == game.PhaseHunting {
		state["hunt_word"] = room.game.HuntWord
	}

	if room.game.TurnPhase == game.PhaseRiders {
		state["rider_hostile"] = room.game.PendingRiderHostile
		state["rider_count"] = room.game.PendingRiderCount
	}

	// Turn deadline for countdown timer
	if !room.turnDeadline.IsZero() && room.status == StatusPlaying && !room.game.GameOver {
		state["turn_deadline"] = room.turnDeadline.UnixMilli()
	}

	// Party health for current player (backwards compat)
	if currentPlayer != nil {
		state["party_health"] = room.game.GetPartyHealth(currentPlayer)
	}

	// Party health for all players, keyed by player ID
	partyHealthMap := make(map[string]interface{})
	for _, c := range room.clients {
		if c.Player != nil {
			partyHealthMap[c.ID] = room.game.GetPartyHealth(c.Player)
		}
	}
	state["party_health_map"] = partyHealthMap

	// Include loot sites for continuous mode
	if room.roomType == RoomTypeContinuous {
		state["loot_sites"] = room.game.LootSites
	}

	return state
}

func (s *Server) getPlayerInfo(room *GameRoom) []map[string]interface{} {
	// NOTE: caller must already hold room.mu
	players := make([]map[string]interface{}, 0)
	for _, c := range room.clients {
		playerAlive := true
		if c.Player != nil {
			playerAlive = c.Player.Alive
		}
		players = append(players, map[string]interface{}{
			"id":           c.ID,
			"name":         c.Name,
			"alive":        playerAlive,
			"player_alive": playerAlive,
			"score":        int(room.game.Mileage),
		})
	}
	return players
}

func (s *Server) HandleAction(clientID string, roomID string, action string) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	if action == "start" {
		initRoomResources(room)
		room.game.TurnNumber = 1
		room.game.GameOver = false
		room.status = StatusPlaying
		if cp := room.game.GetCurrentPlayer(); cp != nil {
			s.StartTurnTimer(room, cp.ID)
		}
		return "The journey begins! Head west on the Online Trail!"
	}

	if action == "start_game" {
		room.status = StatusPlaying
		room.game.TurnNumber = 1
		room.game.GameOver = false
		initRoomResources(room)
		if cp := room.game.GetCurrentPlayer(); cp != nil {
			s.StartTurnTimer(room, cp.ID)
		}
		return "All players ready! The wagon train departs!"
	}

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	// Dead players cannot take actions
	if c.Player == nil {
		return "Error: Player not found.\n"
	}
	if !c.Player.Alive {
		return "Your party has perished. You are spectating.\n"
	}

	result := room.game.ProcessTurn(c.Player, action)

	// Check if player died during this turn (for 24/7 continuous mode)
	if !c.Player.Alive && room.roomType == RoomTypeContinuous {
		s.createLootSite(room, c)
	}

	if room.game.GameOver {
		modeLabel := "continuous"
		if room.roomType == RoomTypeScheduled {
			modeLabel = "party"
		}
		// Add all players to leaderboard
		for _, cl := range room.clients {
			if cl.Player != nil {
				s.leaderboard.AddEntry(cl.Name, room.game.Win, room.game.Mileage, room.game.TurnNumber, modeLabel)
			}
		}

		// Create loot sites for all dead players in continuous mode
		if room.roomType == RoomTypeContinuous {
			for _, cl := range room.clients {
				if cl.Player != nil && !cl.Player.Alive {
					// Check if loot already exists for this player
					hasLoot := false
					for _, site := range room.game.LootSites {
						if site.PlayerName == cl.Name && !site.IsLooted {
							hasLoot = true
							break
						}
					}
					if !hasLoot {
						s.createLootSite(room, cl)
					}
				}
			}
		}

		room.status = StatusFinished
		s.CancelTurnTimer(room)
	} else if room.game.TurnPhase != game.PhaseFort &&
		room.game.TurnPhase != game.PhaseHunting &&
		room.game.TurnPhase != game.PhaseRiders {
		s.advanceTurnAndCheckFort(room)
	}

	return result
}

func (s *Server) HandleFortBuy(clientID string, roomID string, item string, qty int) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	currentPlayer := room.game.GetCurrentPlayer()
	if currentPlayer == nil || currentPlayer.ID != c.ID {
		return "It's not your turn.\n"
	}

	return room.game.HandleFortBuy(item, qty)
}

func (s *Server) HandleFortSell(clientID string, roomID string, item string, qty int) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	currentPlayer := room.game.GetCurrentPlayer()
	if currentPlayer == nil || currentPlayer.ID != c.ID {
		return "It's not your turn.\n"
	}

	return room.game.HandleFortSell(item, qty)
}

func (s *Server) HandleFortEnter(clientID string, roomID string) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	currentPlayer := room.game.GetCurrentPlayer()
	if currentPlayer == nil || currentPlayer.ID != c.ID {
		return "It's not your turn.\n"
	}

	if !room.game.FortAvailable {
		return "No fort is available at this location.\n"
	}

	// Enter the fort
	room.game.TurnPhase = game.PhaseFort
	room.game.Mileage -= 45
	room.game.ClampResources()
	s.CancelTurnTimer(room)
	return "You arrive at a fort. You can buy supplies here.\n"
}

func (s *Server) HandleFortLeave(clientID string, roomID string) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	currentPlayer := room.game.GetCurrentPlayer()
	if currentPlayer == nil || currentPlayer.ID != c.ID {
		return "It's not your turn.\n"
	}

	result := room.game.HandleFortLeave()
	room.game.FortAvailable = false // Reset fort availability
	s.advanceTurnAndCheckFort(room)
	return result
}

// HandleLootClaim attempts to claim loot from a loot site within 50 miles
func (s *Server) HandleLootClaim(clientID string, roomID string, lootSiteID string) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	currentPlayer := room.game.GetCurrentPlayer()
	if currentPlayer == nil || currentPlayer.ID != c.ID {
		return "It's not your turn.\n"
	}

	// Only available in continuous mode
	if room.roomType != RoomTypeContinuous {
		return "Loot sites are only available in 24/7 mode.\n"
	}

	// Find the loot site
	for i := range room.game.LootSites {
		site := &room.game.LootSites[i]
		if site.ID == lootSiteID {
			// Check if within 50 miles
			if room.game.Mileage < site.Mileage-50 || room.game.Mileage > site.Mileage+50 {
				return "You're too far from that loot site.\n"
			}

			if site.IsLooted {
				return "This wagon has already been scavenged by " + site.LootedBy + ".\n"
			}

			// Claim the loot
			room.game.Food += site.Food
			room.game.Bullets += site.Bullets
			room.game.Clothing += site.Clothing
			room.game.MiscSupplies += site.MiscSupplies
			room.game.Cash += site.Cash
			room.game.OxenCost += site.OxenCost

			// Mark as looted
			site.IsLooted = true
			site.LootedBy = c.Name
			site.LootedAt = time.Now()

			room.game.ClampResources()

			return fmt.Sprintf("You looted %s's wagon and found: $%.0f cash, %.0f food, %.0f bullets, %.0f clothing, %.0f misc supplies!\n",
				site.PlayerName, site.Cash, site.Food, site.Bullets, site.Clothing, site.MiscSupplies)
		}
	}

	return "Loot site not found.\n"
}

func (s *Server) HandleHuntShoot(clientID string, roomID string, reactionTimeMs int) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	currentPlayer := room.game.GetCurrentPlayer()
	if currentPlayer == nil || currentPlayer.ID != c.ID {
		return "It's not your turn.\n"
	}

	if room.game.TurnPhase != game.PhaseHunting {
		return "You're not hunting right now.\n"
	}

	if c.Player == nil {
		return "Error: Player not found.\n"
	}

	result := room.game.HandleHuntShoot(c.Player, reactionTimeMs)

	if room.game.GameOver {
		modeLabel := "continuous"
		if room.roomType == RoomTypeScheduled {
			modeLabel = "party"
		}
		// Add all players to leaderboard
		for _, cl := range room.clients {
			if cl.Player != nil {
				s.leaderboard.AddEntry(cl.Name, room.game.Win, room.game.Mileage, room.game.TurnNumber, modeLabel)
			}
		}
		room.status = StatusFinished
		s.CancelTurnTimer(room)
	} else {
		s.advanceTurnAndCheckFort(room)
	}

	return result
}

func (s *Server) HandleRiderTactic(clientID string, roomID string, tactic int) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	c, ok := room.clients[clientID]
	if !ok {
		return ""
	}

	currentPlayer := room.game.GetCurrentPlayer()
	if currentPlayer == nil || currentPlayer.ID != c.ID {
		return "It's not your turn.\n"
	}

	if room.game.TurnPhase != game.PhaseRiders {
		return "There are no riders right now.\n"
	}

	if c.Player == nil {
		return "Error: Player not found.\n"
	}

	if tactic < 1 || tactic > 4 {
		tactic = 3
	}

	result := room.game.HandleRiderTactic(c.Player, tactic)

	if room.game.GameOver {
		modeLabel := "continuous"
		if room.roomType == RoomTypeScheduled {
			modeLabel = "party"
		}
		// Add all players to leaderboard
		for _, cl := range room.clients {
			if cl.Player != nil {
				s.leaderboard.AddEntry(cl.Name, room.game.Win, room.game.Mileage, room.game.TurnNumber, modeLabel)
			}
		}
		room.status = StatusFinished
		s.CancelTurnTimer(room)
	} else {
		s.advanceTurnAndCheckFort(room)
	}

	return result
}

func main() {
	httpPort := flag.String("http", "8080", "HTTP server port")
	flag.Parse()

	if httpPortEnv := os.Getenv("HTTP_PORT"); httpPortEnv != "" {
		*httpPort = httpPortEnv
	}

	dataPath := os.Getenv("DATA_PATH")
	if dataPath == "" {
		dataPath = "./data"
	}

	s := NewServer(dataPath)

	hub := NewHub(s)
	s.hub = hub
	go hub.Run()

	// Periodic cleanup of stale rooms
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.CleanupStaleRooms()
		}
	}()

	// Periodic loot deterioration (every 24 hours)
	go func() {
		// Run immediately on startup, then every 24 hours
		s.deteriorateLootSites()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			s.deteriorateLootSites()
		}
	}()

	http.HandleFunc("/", serveStatic)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})
	http.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cookie, err := r.Cookie("session_id")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"valid": false})
			return
		}
		sess, ok := s.sessionManager.GetSessionByID(cookie.Value)
		if !ok {
			json.NewEncoder(w).Encode(map[string]interface{}{"valid": false})
			return
		}
		// Check if the room still exists
		roomExists := false
		if sess.RoomID != "" {
			s.roomsMu.RLock()
			_, roomExists = s.rooms[sess.RoomID]
			s.roomsMu.RUnlock()
		}
		if !roomExists {
			json.NewEncoder(w).Encode(map[string]interface{}{"valid": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":   true,
			"name":    sess.Name,
			"room_id": sess.RoomID,
		})
	})
	http.HandleFunc("/api/lobbies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			lobbies := s.ListLobbies()
			json.NewEncoder(w).Encode(lobbies)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})
	http.HandleFunc("/api/lobbies/create", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name       string `json:"name"`
			Password   string `json:"password"`
			MaxPlayers int    `json:"max_players"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			req.Name = "Pioneer Party"
		}
		// Owner ID will be set when they connect via WebSocket
		room := s.CreateRoom(req.Name, req.Password, "", req.MaxPlayers)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   room.id,
			"name": room.name,
		})
	})
	http.HandleFunc("/api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		mode := r.URL.Query().Get("mode")
		if mode != "" {
			entries := s.leaderboard.GetTopByMode(10, mode)
			log.Printf("Leaderboard API: mode=%s, entries=%d", mode, len(entries))
			json.NewEncoder(w).Encode(entries)
		} else {
			continuous := s.leaderboard.GetTopByMode(10, "continuous")
			party := s.leaderboard.GetTopByMode(10, "party")
			log.Printf("Leaderboard API: continuous=%d, party=%d", len(continuous), len(party))
			result := map[string][]LeaderboardEntry{
				"continuous": continuous,
				"party":      party,
			}
			json.NewEncoder(w).Encode(result)
		}
	})

	log.Printf("HTTP server listening on :%s", *httpPort)

	// Create HTTP server with timeouts
	httpServer := &http.Server{
		Addr:         ":" + *httpPort,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	log.Println("Online Trail server running!")
	select {}
}
