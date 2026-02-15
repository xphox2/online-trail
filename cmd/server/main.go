package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
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
	game         *game.GameState            // used for scheduled/private mode (shared game)
	playerGames  map[string]*game.GameState // continuous mode: each player has their own game state
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
	dataPath       string
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
		playerGames: make(map[string]*game.GameState),
		clients:     make(map[string]*Client),
		deadPlayers: make(map[string]bool),
	}
}

func NewServer(dataPath string) *Server {
	s := &Server{
		rooms:          make(map[string]*GameRoom),
		sessionManager: NewSessionManager(),
		leaderboard:    NewLeaderboard(dataPath),
		dataPath:       dataPath,
	}
	// Create the permanent continuous room
	continuous := NewGameRoom("continuous", "The Open Trail", RoomTypeContinuous)
	s.rooms["continuous"] = continuous
	// Load persisted game state if exists
	s.loadGameState()
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

type PersistedGameState struct {
	PlayerName       string          `json:"player_name"`
	TurnNumber       int             `json:"turn_number"`
	Mileage          float64         `json:"mileage"`
	DistanceTraveled int             `json:"distance_traveled"`
	Week             int             `json:"week"`
	Day              int             `json:"day"`
	Food             float64         `json:"food"`
	Bullets          float64         `json:"bullets"`
	Clothing         float64         `json:"clothing"`
	MiscSupplies     float64         `json:"misc_supplies"`
	Cash             float64         `json:"cash"`
	OxenCost         float64         `json:"oxen_cost"`
	TurnPhase        game.TurnPhase  `json:"turn_phase"`
	GameOver         bool            `json:"game_over"`
	Win              bool            `json:"win"`
	CurrentPlayerIdx int             `json:"current_player_idx"`
	LootSites        []game.LootSite `json:"loot_sites"`
	FortAvailable    bool            `json:"fort_available"`
}

// PersistedContinuousState saves the state for continuous mode (per-player games)
type PersistedContinuousState struct {
	LootSites      []game.LootSite               `json:"loot_sites"`
	PlayerGames    map[string]PersistedGameState `json:"player_games"`
	GameWon        bool                          `json:"game_won"`
	WinnerPlayerID string                        `json:"winner_player_id"`
}

func (s *Server) getGameStateFilePath() string {
	if s.dataPath == "" {
		s.dataPath = "."
	}
	return filepath.Join(s.dataPath, "game_state.json")
}

func (s *Server) loadGameState() {
	room := s.GetRoom("continuous")
	if room == nil {
		return
	}

	filePath := s.getGameStateFilePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("No saved game state found at %s (this is normal on first run)", filePath)
		return
	}

	var persisted PersistedContinuousState
	if err := json.Unmarshal(data, &persisted); err != nil {
		log.Printf("Failed to parse saved game state: %v", err)
		return
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	// Load loot sites
	room.game.LootSites = persisted.LootSites

	// Load each player's game state
	for playerID, playerData := range persisted.PlayerGames {
		playerGame := game.NewGameState()
		playerGame.TurnNumber = playerData.TurnNumber
		playerGame.Mileage = playerData.Mileage
		playerGame.DistanceTraveled = playerData.DistanceTraveled
		playerGame.Week = playerData.Week
		playerGame.Day = playerData.Day
		playerGame.Food = playerData.Food
		playerGame.Bullets = playerData.Bullets
		playerGame.Clothing = playerData.Clothing
		playerGame.MiscSupplies = playerData.MiscSupplies
		playerGame.Cash = playerData.Cash
		playerGame.OxenCost = playerData.OxenCost
		playerGame.TurnPhase = playerData.TurnPhase
		playerGame.GameOver = playerData.GameOver
		playerGame.Win = playerData.Win
		playerGame.CurrentPlayerIdx = playerData.CurrentPlayerIdx
		playerGame.FortAvailable = playerData.FortAvailable

		// Add player to the game
		player := playerGame.AddPlayer(playerData.PlayerName, game.PlayerTypeHuman)
		player.ID = playerID
		player.Alive = !playerData.GameOver

		room.playerGames[playerID] = playerGame

		log.Printf("Loaded game for player %s: Turn %d, Mileage %.0f, Week %d",
			playerID, playerData.TurnNumber, playerData.Mileage, playerData.Week)
	}

	if persisted.GameWon {
		room.status = StatusFinished
	} else if len(room.playerGames) > 0 {
		room.status = StatusPlaying
	}

	log.Printf("Game state loaded: %d players, %d loot sites, Status %s",
		len(room.playerGames), len(room.game.LootSites), room.status)
}

func (s *Server) saveGameState() {
	room := s.GetRoom("continuous")
	if room == nil {
		return
	}

	room.mu.RLock()
	defer room.mu.RUnlock()

	// Check if someone won
	gameWon := false
	winnerID := ""
	for playerID, playerGame := range room.playerGames {
		if playerGame.Win {
			gameWon = true
			winnerID = playerID
			break
		}
	}

	if gameWon {
		filePath := s.getGameStateFilePath()
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to delete saved game state: %v", err)
		} else {
			log.Printf("Game won by %s - saved game state deleted", winnerID)
		}
		return
	}

	// Save each player's game state
	playerGames := make(map[string]PersistedGameState)
	for playerID, playerGame := range room.playerGames {
		playerName := playerID
		for _, c := range room.clients {
			if c.ID == playerID {
				playerName = c.Name
				break
			}
		}

		playerGames[playerID] = PersistedGameState{
			PlayerName:       playerName,
			TurnNumber:       playerGame.TurnNumber,
			Mileage:          playerGame.Mileage,
			DistanceTraveled: playerGame.DistanceTraveled,
			Week:             playerGame.Week,
			Day:              playerGame.Day,
			Food:             playerGame.Food,
			Bullets:          playerGame.Bullets,
			Clothing:         playerGame.Clothing,
			MiscSupplies:     playerGame.MiscSupplies,
			Cash:             playerGame.Cash,
			OxenCost:         playerGame.OxenCost,
			TurnPhase:        playerGame.TurnPhase,
			GameOver:         playerGame.GameOver,
			Win:              playerGame.Win,
			CurrentPlayerIdx: playerGame.CurrentPlayerIdx,
			FortAvailable:    playerGame.FortAvailable,
		}
	}

	persisted := PersistedContinuousState{
		LootSites:   room.game.LootSites,
		PlayerGames: playerGames,
		GameWon:     gameWon,
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal game state: %v", err)
		return
	}

	filePath := s.getGameStateFilePath()
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Failed to create data directory: %v", err)
		return
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("Failed to save game state: %v", err)
	} else {
		log.Printf("Game state saved: %d players, %d loot sites", len(playerGames), len(room.game.LootSites))
	}
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

	if room.roomType == RoomTypeContinuous {
		// Continuous mode: each player has their own independent game
		if existingGame, ok := room.playerGames[c.ID]; ok {
			// Reconnecting player - find their player in the game
			c.Player = nil
			for _, p := range existingGame.Players {
				if p.ID == c.ID {
					c.Player = p
					break
				}
			}
			if c.Player == nil {
				// Player was fully removed, create new
				player := existingGame.AddPlayer(c.Name, game.PlayerTypeHuman)
				player.ID = c.ID
				c.Player = player
			}
			log.Printf("Player %s reconnected to continuous %s (ID: %s)", c.Name, roomID, c.ID)
		} else {
			// New player in continuous mode - create their own game state
			newGame := game.NewGameState()
			newGame.OxenCost = 220
			newGame.Food = 100
			newGame.Bullets = 50
			newGame.Clothing = 20
			newGame.MiscSupplies = 10
			newGame.Cash = 700
			newGame.GameOver = false
			newGame.Win = false
			newGame.CurrentPlayerIdx = 0
			newGame.TurnNumber = 1
			newGame.TurnPhase = game.PhaseMainMenu
			newGame.Week = 1
			newGame.Day = 1

			player := newGame.AddPlayer(c.Name, game.PlayerTypeHuman)
			player.ID = c.ID
			c.Player = player

			room.playerGames[c.ID] = newGame
			room.status = StatusPlaying
			log.Printf("Continuous room %s: player %s started fresh at Turn 1, Mileage 0",
				roomID, c.Name)
		}
	} else {
		// Scheduled/private mode: players share the room's game
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

			// If this is the first player in scheduled mode and waiting, auto-start
			if room.status == StatusWaiting && len(room.clients) >= 1 {
				initRoomResources(room)
				room.status = StatusPlaying
			}
		}

		if room.game.GetCurrentPlayer() == nil && len(room.game.Players) > 0 {
			room.game.CurrentPlayerIdx = 0
		}
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

// createLootSiteFromPlayer creates a loot site from a player's individual game state (continuous mode)
func (s *Server) createLootSiteFromPlayer(room *GameRoom, player *game.Player, playerGame *game.GameState) {
	if player == nil || playerGame == nil {
		return
	}

	// Find the client name
	clientName := player.Name
	for _, c := range room.clients {
		if c.ID == player.ID {
			clientName = c.Name
			break
		}
	}

	lootSite := game.LootSite{
		ID:           fmt.Sprintf("loot-%s-%d", player.ID, time.Now().Unix()),
		Mileage:      playerGame.Mileage,
		PlayerName:   clientName,
		Food:         playerGame.Food,
		Bullets:      playerGame.Bullets,
		Clothing:     playerGame.Clothing,
		MiscSupplies: playerGame.MiscSupplies,
		Cash:         playerGame.Cash,
		OxenCost:     playerGame.OxenCost,
		DateCreated:  time.Now(),
		IsLooted:     false,
	}

	room.game.LootSites = append(room.game.LootSites, lootSite)
	log.Printf("Loot site created at mile %.0f for dead player %s in continuous room",
		playerGame.Mileage, clientName)
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

	// In continuous mode, never end the game on a loss
	if room.roomType == RoomTypeContinuous && room.game.GameOver && !room.game.Win {
		room.game.GameOver = false
	}

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

// saveGameStateAfterTurn saves the game state after a turn is completed.
// Should be called outside the room lock to avoid deadlock.
func (s *Server) saveGameStateAfterTurn(roomID string) {
	if roomID == "continuous" {
		go s.saveGameState()
	}
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
		// In continuous mode, the game only truly ends on a win
		if room.roomType == RoomTypeContinuous && !room.game.Win {
			room.game.GameOver = false
			log.Printf("Continuous room: player timed out and all dead, game continues waiting for new players")
		} else {
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
		}
	} else {
		s.advanceTurnAndCheckFort(room)
	}

	room.mu.Unlock()

	// Phase 2: broadcast outside of room lock to avoid deadlock
	if s.hub != nil {
		s.hub.BroadcastEventTo(roomID, playerName, "continue", result)
		s.hub.BroadcastStateTo(roomID)
	}

	// Save game state for persistence
	s.saveGameStateAfterTurn(roomID)
}

func (s *Server) GetState(roomID string) interface{} {
	room := s.GetRoom(roomID)
	if room == nil {
		return map[string]interface{}{"error": "room not found"}
	}
	room.mu.RLock()
	defer room.mu.RUnlock()

	// For continuous mode, build per-player states
	if room.roomType == RoomTypeContinuous {
		return s.getContinuousState(room)
	}

	// Scheduled/private mode: shared game state
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

// getContinuousState returns the state for continuous mode where each player has their own game
func (s *Server) getContinuousState(room *GameRoom) map[string]interface{} {
	state := map[string]interface{}{
		"turn_number":   0, // Not used in continuous mode
		"mileage":       0, // Not used - each player has own mileage
		"food":          0,
		"bullets":       0,
		"clothing":      0,
		"misc_supplies": 0,
		"cash":          0,
		"game_over":     false,
		"win":           false,
		"turn_phase":    game.PhaseMainMenu,
		"room_id":       room.id,
		"room_name":     room.name,
		"room_type":     room.roomType,
		"game_status":   room.status,
		"loot_sites":    room.game.LootSites,
	}

	// Build player states - each player has their own independent game
	playerStates := make(map[string]map[string]interface{})
	playersInfo := make([]map[string]interface{}, 0)

	for _, c := range room.clients {
		playerGame, hasGame := room.playerGames[c.ID]
		playerAlive := true

		if hasGame && playerGame != nil && len(playerGame.Players) > 0 {
			// Find the player's player struct
			var player *game.Player
			for _, p := range playerGame.Players {
				if p.ID == c.ID {
					player = p
					break
				}
			}
			if player != nil {
				playerAlive = player.Alive
			}

			playerStates[c.ID] = map[string]interface{}{
				"turn_number":       playerGame.TurnNumber,
				"mileage":           playerGame.Mileage,
				"distance_traveled": playerGame.DistanceTraveled,
				"week":              playerGame.Week,
				"day":               playerGame.Day,
				"food":              playerGame.Food,
				"bullets":           playerGame.Bullets,
				"clothing":          playerGame.Clothing,
				"misc_supplies":     playerGame.MiscSupplies,
				"cash":              playerGame.Cash,
				"oxen_cost":         playerGame.OxenCost,
				"game_over":         playerGame.GameOver,
				"win":               playerGame.Win,
				"turn_phase":        playerGame.TurnPhase,
				"fort_available":    playerGame.FortAvailable,
				"hunt_word":         playerGame.HuntWord,
				"rider_hostile":     playerGame.PendingRiderHostile,
				"rider_count":       playerGame.PendingRiderCount,
				"alive":             playerAlive,
				"player_alive":      playerAlive,
			}

			// Add party health
			if player != nil {
				playerStates[c.ID]["party_health"] = playerGame.GetPartyHealth(player)
			}
		} else {
			// Player has no game yet (just joined)
			playerStates[c.ID] = map[string]interface{}{
				"turn_number":       0,
				"mileage":           0,
				"distance_traveled": 0,
				"week":              1,
				"day":               1,
				"food":              0,
				"bullets":           0,
				"clothing":          0,
				"misc_supplies":     0,
				"cash":              0,
				"game_over":         false,
				"win":               false,
				"turn_phase":        game.PhaseMainMenu,
				"alive":             true,
				"player_alive":      true,
			}
		}

		// Player list info
		playersInfo = append(playersInfo, map[string]interface{}{
			"id":           c.ID,
			"name":         c.Name,
			"alive":        playerAlive,
			"player_alive": playerAlive,
			"score":        0, // Will be filled from playerStates
		})
	}

	// Update scores from player states
	for i, p := range playersInfo {
		if ps, ok := playerStates[p["id"].(string)]; ok {
			playersInfo[i]["score"] = int(ps["mileage"].(float64))
		}
	}

	state["player_states"] = playerStates
	state["players"] = playersInfo

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

	// For continuous mode, each player has their own game
	if room.roomType == RoomTypeContinuous {
		log.Printf("DEBUG HandleAction: routing to handleContinuousAction, clientID=%s, action=%s", clientID, action)
		return s.handleContinuousAction(room, clientID, action)
	}

	// Scheduled/private mode: shared game state
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
		// In continuous mode, the game only truly ends on a win
		if room.roomType == RoomTypeContinuous && !room.game.Win {
			room.game.GameOver = false
			log.Printf("Continuous room: all players dead, game continues waiting for new players")
		} else {
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
		}
	} else if room.game.TurnPhase != game.PhaseFort &&
		room.game.TurnPhase != game.PhaseHunting &&
		room.game.TurnPhase != game.PhaseRiders {
		s.advanceTurnAndCheckFort(room)
	}

	// Save game state for persistence (defer will unlock)
	s.saveGameStateAfterTurn(roomID)

	return result
}

// getPlayerGame returns the player's game state for the given room and client
// Returns nil if not found or not continuous mode
func (s *Server) getPlayerGame(room *GameRoom, clientID string) (*game.GameState, *game.Player) {
	if room.roomType != RoomTypeContinuous {
		return nil, nil
	}

	playerGame, ok := room.playerGames[clientID]
	if !ok || playerGame == nil {
		return nil, nil
	}

	// Find the player's player struct
	var player *game.Player
	for _, p := range playerGame.Players {
		if p.ID == clientID {
			player = p
			break
		}
	}

	return playerGame, player
}

// handleContinuousAction handles player actions in continuous mode.
// Each player has their own independent game state.
func (s *Server) handleContinuousAction(room *GameRoom, clientID string, action string) string {
	log.Printf("DEBUG handleContinuousAction: clientID=%s, action=%s, playerGames count=%d", clientID, action, len(room.playerGames))

	playerGame, ok := room.playerGames[clientID]
	if !ok || playerGame == nil {
		log.Printf("DEBUG: playerGame not found for clientID=%s", clientID)
		return "Error: Your game state not found. Please rejoin.\n"
	}

	// Get the player's player from their game
	var player *game.Player
	for _, p := range playerGame.Players {
		if p.ID == clientID {
			player = p
			break
		}
	}
	if player == nil {
		log.Printf("DEBUG: player not found in playerGame for clientID=%s", clientID)
		return "Error: Player not found in game state.\n"
	}

	log.Printf("DEBUG: processing action=%s for player=%s, TurnPhase=%s", action, player.Name, playerGame.TurnPhase)

	// Handle start action - reset player's game
	if action == "start" {
		playerGame.OxenCost = 220
		playerGame.Food = 100
		playerGame.Bullets = 50
		playerGame.Clothing = 20
		playerGame.MiscSupplies = 10
		playerGame.Cash = 700
		playerGame.GameOver = false
		playerGame.Win = false
		playerGame.TurnNumber = 1
		playerGame.Mileage = 0
		playerGame.DistanceTraveled = 0
		playerGame.Week = 1
		playerGame.Day = 1
		playerGame.TurnPhase = game.PhaseMainMenu

		// Reset player party
		names := []string{"You", "Wife", "Son", "Daughter", "Baby"}
		for i := range player.Party {
			player.Party[i].Alive = true
			player.Party[i].Health = 100
			player.Party[i].Injured = false
			if i < len(names) {
				player.Party[i].Name = names[i]
			}
		}
		player.Alive = true

		log.Printf("Continuous: player %s started fresh at Turn 1")
		s.saveGameState()
		return "Your journey begins! Head west on the Online Trail!"
	}

	// Dead players cannot take actions
	if !player.Alive {
		return "Your party has perished. You are spectating.\n"
	}

	// Process the turn using player's own game state
	result := playerGame.ProcessTurn(player, action)

	// Check if player died during this turn
	if !player.Alive {
		s.createLootSiteFromPlayer(room, player, playerGame)
		log.Printf("Continuous: player %s died at Mileage %.0f, Week %d",
			player.Name, playerGame.Mileage, playerGame.Week)
	}

	// In continuous mode, each player has their own turn - increment TurnNumber after action
	// Only if not in an interactive phase (hunt/fort/riders will increment when they complete)
	if playerGame.TurnPhase == game.PhaseMainMenu {
		playerGame.NextTurn()
	}

	// Check for win
	if playerGame.Win {
		log.Printf("Continuous: player %s WON at Mileage %.0f!", player.Name, playerGame.Mileage)
		room.status = StatusFinished
	}

	// Save state after each action
	s.saveGameState()

	return result
}

func (s *Server) HandleFortBuy(clientID string, roomID string, item string, qty int) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	// Continuous mode: get player's own game
	if room.roomType == RoomTypeContinuous {
		playerGame, player := s.getPlayerGame(room, clientID)
		if playerGame == nil || player == nil {
			return "Error: Your game state not found. Please rejoin.\n"
		}
		result := playerGame.HandleFortBuy(item, qty)
		s.saveGameState()
		return result
	}

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

	// Continuous mode: get player's own game
	if room.roomType == RoomTypeContinuous {
		playerGame, player := s.getPlayerGame(room, clientID)
		if playerGame == nil || player == nil {
			return "Error: Your game state not found. Please rejoin.\n"
		}
		result := playerGame.HandleFortSell(item, qty)
		s.saveGameState()
		return result
	}

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

	// Continuous mode: get player's own game
	if room.roomType == RoomTypeContinuous {
		playerGame, player := s.getPlayerGame(room, clientID)
		if playerGame == nil || player == nil {
			return "Error: Your game state not found. Please rejoin.\n"
		}
		if !playerGame.FortAvailable {
			return "No fort is available at this location.\n"
		}
		playerGame.TurnPhase = game.PhaseFort
		playerGame.Mileage -= 45
		playerGame.ClampResources()
		s.saveGameState()
		return "You arrive at a fort. You can buy supplies here.\n"
	}

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

	// Continuous mode: get player's own game
	if room.roomType == RoomTypeContinuous {
		playerGame, player := s.getPlayerGame(room, clientID)
		if playerGame == nil || player == nil {
			return "Error: Your game state not found. Please rejoin.\n"
		}
		result := playerGame.HandleFortLeave()
		playerGame.FortAvailable = false
		// Increment turn after leaving fort
		playerGame.NextTurn()
		s.saveGameState()
		return result
	}

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

	// Save game state for persistence
	s.saveGameStateAfterTurn(roomID)

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

	// Only available in continuous mode
	if room.roomType != RoomTypeContinuous {
		return "Loot sites are only available in continuous mode.\n"
	}

	// Get player's own game
	playerGame, player := s.getPlayerGame(room, clientID)
	if playerGame == nil || player == nil {
		return "Error: Your game state not found. Please rejoin.\n"
	}

	// Find the loot site
	for i := range room.game.LootSites {
		site := &room.game.LootSites[i]
		if site.ID == lootSiteID {
			// Check if within 50 miles
			if playerGame.Mileage < site.Mileage-50 || playerGame.Mileage > site.Mileage+50 {
				return "You're too far from that loot site.\n"
			}

			if site.IsLooted {
				return "This wagon has already been scavenged by " + site.LootedBy + ".\n"
			}

			// Claim the loot - add to player's resources
			playerGame.Food += site.Food
			playerGame.Bullets += site.Bullets
			playerGame.Clothing += site.Clothing
			playerGame.MiscSupplies += site.MiscSupplies
			playerGame.Cash += site.Cash

			// Mark as looted
			site.IsLooted = true
			site.LootedBy = player.Name

			s.saveGameState()
			return fmt.Sprintf("You scavenged the abandoned wagon of %s!\n", site.PlayerName)
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

	// Continuous mode: get player's own game
	if room.roomType == RoomTypeContinuous {
		playerGame, player := s.getPlayerGame(room, clientID)
		if playerGame == nil || player == nil {
			return "Error: Your game state not found. Please rejoin.\n"
		}
		if playerGame.TurnPhase != game.PhaseHunting {
			return "You're not hunting right now.\n"
		}
		result := playerGame.HandleHuntShoot(player, reactionTimeMs)

		// Check for death
		if !player.Alive {
			s.createLootSiteFromPlayer(room, player, playerGame)
		}

		// Increment turn after hunt completes
		playerGame.NextTurn()

		// Check for win
		if playerGame.Win {
			room.status = StatusFinished
		}

		s.saveGameState()
		return result
	}

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

	// Save game state for persistence
	s.saveGameStateAfterTurn(roomID)

	return result
}

func (s *Server) HandleRiderTactic(clientID string, roomID string, tactic int) string {
	room := s.GetRoom(roomID)
	if room == nil {
		return ""
	}
	room.mu.Lock()
	defer room.mu.Unlock()

	// Continuous mode: get player's own game
	if room.roomType == RoomTypeContinuous {
		playerGame, player := s.getPlayerGame(room, clientID)
		if playerGame == nil || player == nil {
			return "Error: Your game state not found. Please rejoin.\n"
		}
		if playerGame.TurnPhase != game.PhaseRiders {
			return "There are no riders right now.\n"
		}
		if tactic < 1 || tactic > 4 {
			tactic = 3
		}
		result := playerGame.HandleRiderTactic(player, tactic)

		// Check for death
		if !player.Alive {
			s.createLootSiteFromPlayer(room, player, playerGame)
		}

		// Increment turn after rider tactic is resolved
		playerGame.NextTurn()

		// Check for win
		if playerGame.Win {
			room.status = StatusFinished
		}

		s.saveGameState()
		return result
	}

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

	// Save game state for persistence
	s.saveGameStateAfterTurn(roomID)

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
