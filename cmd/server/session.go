package main

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type Session struct {
	ID        string
	Name      string
	ClientID  string
	RoomID    string
	CreatedAt time.Time
	LastSeen  time.Time
	Alive     bool
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (sm *SessionManager) CreateSession(name string, clientID string, roomID string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session already exists (by checking all sessions for matching ClientID)
	for _, s := range sm.sessions {
		if s.ClientID == clientID {
			// Restore existing session
			s.LastSeen = time.Now()
			s.Alive = true
			s.RoomID = roomID
			return s.ID
		}
	}

	// Check if player name already exists (for same player rejoining)
	for _, s := range sm.sessions {
		if s.Name == name && s.Alive {
			// Update existing session
			s.ClientID = clientID
			s.LastSeen = time.Now()
			s.Alive = true
			s.RoomID = roomID
			return s.ID
		}
	}

	// Create new session
	sessionID := GenerateSecureID()
	sm.sessions[sessionID] = &Session{
		ID:        sessionID,
		Name:      name,
		ClientID:  clientID,
		RoomID:    roomID,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		Alive:     true,
	}
	return sessionID
}

func (sm *SessionManager) GetSession(sessionID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[sessionID]
}

func (sm *SessionManager) UpdateClient(sessionID, clientID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[sessionID]; ok {
		s.ClientID = clientID
		s.LastSeen = time.Now()
	}
}

func (sm *SessionManager) UpdateRoomID(sessionID, roomID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[sessionID]; ok {
		s.RoomID = roomID
	}
}

func (sm *SessionManager) RemoveClient(clientID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, sess := range sm.sessions {
		if sess.ClientID == clientID {
			sess.Alive = false
		}
	}
}

func (sm *SessionManager) GetActiveSessions() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	active := make([]*Session, 0)
	for _, s := range sm.sessions {
		if s.Alive {
			active = append(active, s)
		}
	}
	return active
}

func (sm *SessionManager) GetSessionByID(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.sessions[sessionID]
	if !ok || !s.Alive {
		return nil, false
	}
	return s, true
}

func (sm *SessionManager) InvalidateSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[sessionID]; ok {
		s.Alive = false
	}
}

func GenerateSecureID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
