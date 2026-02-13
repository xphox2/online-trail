package game

import (
	"fmt"
	"math/rand"
	"time"
)

type PlayerType string

const (
	PlayerTypeHuman PlayerType = "human"
	PlayerTypeCPU   PlayerType = "cpu"
)

type PartyMember struct {
	Name    string
	Alive   bool
	Health  int
	Injured bool
}

type Player struct {
	ID           string
	Name         string
	Type         PlayerType
	Party        []PartyMember
	InputChan    chan string
	OutputChan   chan string
	Connected    bool
	ShootingRank int
	Alive        bool
}

type GameState struct {
	Players          []*Player
	CurrentPlayerIdx int
	TurnNumber       int
	Week             int
	Day              int
	Mileage          float64
	Food             float64
	Bullets          float64
	Clothing         float64
	MiscSupplies     float64
	Cash             float64
	OxenCost         float64
	DistanceTraveled int
	TurnPhase        TurnPhase
	EventLog         []string
	GameOver         bool
	Win              bool
	FinalDate        string
	Rand             *rand.Rand

	// Interactive phase fields
	PendingRiderHostile bool
	PendingEatingLevel  int
	PendingRiderCount   int
	HuntWord            string

	// Fort availability
	FortAvailable bool

	// Loot sites (abandoned wagons from dead players) - for 24/7 mode
	LootSites []LootSite
}

// LootSite represents an abandoned wagon from a dead player
type LootSite struct {
	ID           string    `json:"id"`
	Mileage      float64   `json:"mileage"`
	PlayerName   string    `json:"player_name"`
	Food         float64   `json:"food"`
	Bullets      float64   `json:"bullets"`
	Clothing     float64   `json:"clothing"`
	MiscSupplies float64   `json:"misc_supplies"`
	Cash         float64   `json:"cash"`
	OxenCost     float64   `json:"oxen_cost"`
	DateCreated  time.Time `json:"date_created"`
	IsLooted     bool      `json:"is_looted"`
	LootedBy     string    `json:"looted_by"`
	LootedAt     time.Time `json:"looted_at"`
}

type TurnPhase string

const (
	PhaseStart         TurnPhase = "start"
	PhaseMainMenu      TurnPhase = "main_menu"
	PhaseFort          TurnPhase = "fort"
	PhaseHunting       TurnPhase = "hunting"
	PhaseEating        TurnPhase = "eating"
	PhaseRiders        TurnPhase = "riders"
	PhaseEvents        TurnPhase = "events"
	PhaseMountains     TurnPhase = "mountains"
	PhaseFinalTurn     TurnPhase = "final_turn"
	PhaseGameOver      TurnPhase = "game_over"
	PhaseShooting      TurnPhase = "shooting"
	PhaseIllness       TurnPhase = "illness"
	PhaseRiverCrossing TurnPhase = "river_crossing"
)

type Event struct {
	ID          int
	Name        string
	Description string
	Handler     func(g *GameState, p *Player) string
}

var shootingWords = []string{"BANG", "BLAM", "POW", "WHAM"}

func NewGameState() *GameState {
	return &GameState{
		Players:          make([]*Player, 0),
		CurrentPlayerIdx: 0,
		TurnNumber:       0,
		Week:             1,
		Day:              1,
		Mileage:          0,
		Food:             0,
		Bullets:          0,
		Clothing:         0,
		MiscSupplies:     0,
		Cash:             0,
		OxenCost:         0,
		DistanceTraveled: 0,
		TurnPhase:        PhaseStart,
		EventLog:         make([]string, 0),
		GameOver:         false,
		Win:              false,
		Rand:             rand.New(rand.NewSource(time.Now().UnixNano())),
		LootSites:        make([]LootSite, 0),
	}
}

func (g *GameState) GetCurrentPlayer() *Player {
	if g.CurrentPlayerIdx >= len(g.Players) {
		return nil
	}
	return g.Players[g.CurrentPlayerIdx]
}

func (g *GameState) GetHumanPlayers() []*Player {
	humans := make([]*Player, 0)
	for _, p := range g.Players {
		if p.Type == PlayerTypeHuman && p.Alive {
			humans = append(humans, p)
		}
	}
	return humans
}

// GetAllHumanPlayers returns all human players including dead ones.
func (g *GameState) GetAllHumanPlayers() []*Player {
	humans := make([]*Player, 0)
	for _, p := range g.Players {
		if p.Type == PlayerTypeHuman {
			humans = append(humans, p)
		}
	}
	return humans
}

func (g *GameState) NextTurn() {
	humans := g.GetHumanPlayers()
	if len(humans) == 0 {
		g.GameOver = true
		return
	}

	g.TurnNumber++
	if g.TurnNumber%4 == 0 {
		g.Week++
	}

	// Single alive player — ensure index points to them
	if len(humans) == 1 {
		for idx, p := range g.Players {
			if p == humans[0] {
				g.CurrentPlayerIdx = idx
				break
			}
		}
		return
	}

	currentHuman := g.GetCurrentPlayer()
	found := false
	for i, h := range humans {
		if h == currentHuman {
			nextIdx := (i + 1) % len(humans)
			for idx, p := range g.Players {
				if p == humans[nextIdx] {
					g.CurrentPlayerIdx = idx
					found = true
					break
				}
			}
			break
		}
	}
	if !found {
		// Fallback: pick the first alive human
		for idx, p := range g.Players {
			if p.Type == PlayerTypeHuman && p.Alive {
				g.CurrentPlayerIdx = idx
				break
			}
		}
	}
}

func (g *GameState) AddPlayer(name string, pType PlayerType) *Player {
	party := make([]PartyMember, 5)
	names := []string{"You", "Wife", "Son", "Daughter", "Baby"}
	for i := 0; i < 5; i++ {
		party[i] = PartyMember{
			Name:    names[i],
			Alive:   true,
			Health:  100,
			Injured: false,
		}
	}

	player := &Player{
		ID:           generateID(),
		Name:         name,
		Type:         pType,
		Party:        party,
		InputChan:    make(chan string, 10),
		OutputChan:   make(chan string, 100),
		Connected:    true,
		ShootingRank: 3,
		Alive:        true,
	}

	g.Players = append(g.Players, player)
	return player
}

func generateID() string {
	return fmt.Sprintf("player-%d", time.Now().UnixNano())
}

func (g *GameState) ResetGame() {
	g.Players = make([]*Player, 0)
	g.CurrentPlayerIdx = 0
	g.TurnNumber = 0
	g.Week = 1
	g.Day = 1
	g.Mileage = 0
	g.Food = 0
	g.Bullets = 0
	g.Clothing = 0
	g.MiscSupplies = 0
	g.Cash = 0
	g.OxenCost = 0
	g.DistanceTraveled = 0
	g.TurnPhase = PhaseStart
	g.EventLog = make([]string, 0)
	g.GameOver = false
	g.Win = false
	g.FinalDate = ""
	g.PendingRiderHostile = false
	g.PendingEatingLevel = 0
	g.PendingRiderCount = 0
	g.HuntWord = ""
	g.FortAvailable = false
	g.LootSites = make([]LootSite, 0)
}

// TrailLength is the total trail distance in miles.
const TrailLength = 4500

// MountainThreshold is the mileage at which mountains begin.
const MountainThreshold = 2500

// GetPartyHealth returns party health info for the current player.
type PartyHealthInfo struct {
	Name    string `json:"name"`
	Health  int    `json:"health"`
	Alive   bool   `json:"alive"`
	Injured bool   `json:"injured"`
}

func (g *GameState) GetPartyHealth(p *Player) []PartyHealthInfo {
	if p == nil {
		return nil
	}
	info := make([]PartyHealthInfo, len(p.Party))
	for i, m := range p.Party {
		info[i] = PartyHealthInfo{
			Name:    m.Name,
			Health:  m.Health,
			Alive:   m.Alive,
			Injured: m.Injured,
		}
	}
	return info
}

// DamagePartyMember reduces HP of a specific party member and checks for death.
func (g *GameState) DamagePartyMember(p *Player, memberIdx int, amount int) string {
	if p == nil || memberIdx < 0 || memberIdx >= len(p.Party) {
		return ""
	}
	m := &p.Party[memberIdx]
	if !m.Alive {
		return ""
	}
	m.Health -= amount
	if m.Health <= 0 {
		m.Health = 0
		m.Alive = false
		msg := fmt.Sprintf("%s has died!\n", m.Name)
		if memberIdx == 0 {
			p.Alive = false
			msg += fmt.Sprintf("%s's party leader has fallen! They are out of the game.\n", p.Name)
			g.CheckAllPlayersDead()
		}
		return msg
	}
	m.Injured = true
	return fmt.Sprintf("%s took %d damage! (HP: %d)\n", m.Name, amount, m.Health)
}

// CheckAllPlayersDead sets GameOver if all human players are dead.
func (g *GameState) CheckAllPlayersDead() bool {
	for _, p := range g.Players {
		if p.Type == PlayerTypeHuman && p.Alive {
			return false
		}
	}
	g.GameOver = true
	return true
}

// DamageRandomMember picks a random alive member and damages them.
func (g *GameState) DamageRandomMember(p *Player, amount int) string {
	if p == nil {
		return ""
	}
	alive := make([]int, 0)
	for i, m := range p.Party {
		if m.Alive {
			alive = append(alive, i)
		}
	}
	if len(alive) == 0 {
		return ""
	}
	idx := alive[g.Rand.Intn(len(alive))]
	return g.DamagePartyMember(p, idx, amount)
}

// ClampResources ensures no resource goes below zero.
func (g *GameState) ClampResources() {
	if g.Food < 0 {
		g.Food = 0
	}
	if g.Bullets < 0 {
		g.Bullets = 0
	}
	if g.Clothing < 0 {
		g.Clothing = 0
	}
	if g.MiscSupplies < 0 {
		g.MiscSupplies = 0
	}
	if g.Cash < 0 {
		g.Cash = 0
	}
	if g.Mileage < 0 {
		g.Mileage = 0
	}
	if g.OxenCost < 0 {
		g.OxenCost = 0
	}
}
