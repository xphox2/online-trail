package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type LeaderboardEntry struct {
	PlayerName string  `json:"player_name"`
	Won        bool    `json:"won"`
	Miles      float64 `json:"miles"`
	TurnCount  int     `json:"turn_count"`
	Date       string  `json:"date"`
	GameMode   string  `json:"game_mode"`
}

type Leaderboard struct {
	entries  []LeaderboardEntry
	filePath string
	mu       sync.RWMutex
}

func NewLeaderboard(dataPath string) *Leaderboard {
	if dataPath == "" {
		dataPath = "."
	}
	lb := &Leaderboard{
		entries:  make([]LeaderboardEntry, 0),
		filePath: filepath.Join(dataPath, "leaderboard.json"),
	}
	lb.Load()
	return lb
}

func (lb *Leaderboard) Load() {
	data, err := os.ReadFile(lb.filePath)
	if err != nil {
		log.Printf("Leaderboard file not found at %s (this is normal on first run)", lb.filePath)
		return
	}
	var entries []LeaderboardEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("Failed to parse leaderboard: %v", err)
		return
	}
	lb.entries = entries
	log.Printf("Leaderboard loaded %d entries from %s", len(entries), lb.filePath)
}

func (lb *Leaderboard) Save() {
	data, err := json.MarshalIndent(lb.entries, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal leaderboard: %v", err)
		return
	}
	dir := filepath.Dir(lb.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Failed to create leaderboard directory: %v", err)
		return
	}
	if err := os.WriteFile(lb.filePath, data, 0644); err != nil {
		log.Printf("Failed to save leaderboard to %s: %v", lb.filePath, err)
	} else {
		log.Printf("Leaderboard saved (%d entries)", len(lb.entries))
	}
}

func (lb *Leaderboard) AddEntry(name string, won bool, miles float64, turns int, mode string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	entry := LeaderboardEntry{
		PlayerName: name,
		Won:        won,
		Miles:      miles,
		TurnCount:  turns,
		Date:       time.Now().Format("2006-01-02"),
		GameMode:   mode,
	}
	lb.entries = append(lb.entries, entry)

	// Sort: wins first, then by miles descending
	sort.Slice(lb.entries, func(i, j int) bool {
		if lb.entries[i].Won != lb.entries[j].Won {
			return lb.entries[i].Won
		}
		return lb.entries[i].Miles > lb.entries[j].Miles
	})

	// Keep top 500 entries per mode (1000 total max)
	continuous := make([]LeaderboardEntry, 0)
	party := make([]LeaderboardEntry, 0)
	for _, e := range lb.entries {
		m := e.GameMode
		if m == "" {
			m = "continuous"
		}
		if m == "party" && len(party) < 500 {
			party = append(party, e)
		} else if m != "party" && len(continuous) < 500 {
			continuous = append(continuous, e)
		}
	}
	lb.entries = append(continuous, party...)
	// Re-sort after merge
	sort.Slice(lb.entries, func(i, j int) bool {
		if lb.entries[i].Won != lb.entries[j].Won {
			return lb.entries[i].Won
		}
		return lb.entries[i].Miles > lb.entries[j].Miles
	})

	lb.Save()
}

func (lb *Leaderboard) GetTop(n int) []LeaderboardEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if n > len(lb.entries) {
		n = len(lb.entries)
	}
	result := make([]LeaderboardEntry, n)
	copy(result, lb.entries[:n])
	return result
}

func (lb *Leaderboard) GetTopByMode(n int, mode string) []LeaderboardEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	result := make([]LeaderboardEntry, 0, n)
	for _, e := range lb.entries {
		entryMode := e.GameMode
		if entryMode == "" {
			entryMode = "continuous" // legacy entries default to public trail
		}
		if entryMode == mode {
			result = append(result, e)
			if len(result) >= n {
				break
			}
		}
	}
	return result
}
