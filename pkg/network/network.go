package network

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type MessageType string

const (
	MsgJoin       MessageType = "join"
	MsgLeave      MessageType = "leave"
	MsgGameState  MessageType = "game_state"
	MsgAction     MessageType = "action"
	MsgChat       MessageType = "chat"
	MsgPlayerList MessageType = "player_list"
	MsgError      MessageType = "error"
	MsgTurn       MessageType = "turn"
	MsgStart      MessageType = "start"
)

type Message struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
	Sender  string          `json:"sender"`
	Time    time.Time       `json:"time"`
}

type JoinPayload struct {
	Name string `json:"name"`
}

type ActionPayload struct {
	PlayerID string `json:"player_id"`
	Action   string `json:"action"`
	Value    string `json:"value"`
}

type ChatPayload struct {
	Message string `json:"message"`
}

type GameStatePayload struct {
	State     interface{} `json:"state"`
	TurnIndex int         `json:"turn_index"`
	Phase     string      `json:"phase"`
}

type Player struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Client struct {
	Conn     net.Conn
	PlayerID string
	Name     string
	Input    chan []byte
	Output   chan string
}

type Server struct {
	clients    map[string]*Client
	gameState  interface{}
	playerList []Player
	mu         sync.RWMutex
	listener   net.Listener
	addChan    chan *Client
	removeChan chan string
	broadcast  chan string
}

func NewServer() *Server {
	return &Server{
		clients:    make(map[string]*Client),
		playerList: make([]Player, 0),
		addChan:    make(chan *Client),
		removeChan: make(chan string),
		broadcast:  make(chan string, 100),
	}
}

func (s *Server) AddClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.PlayerID] = c
	s.playerList = append(s.playerList, Player{
		ID:   c.PlayerID,
		Name: c.Name,
	})
}

func (s *Server) RemoveClient(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[playerID]; ok {
		c.Conn.Close()
		delete(s.clients, playerID)
		for i, p := range s.playerList {
			if p.ID == playerID {
				s.playerList = append(s.playerList[:i], s.playerList[i+1:]...)
				break
			}
		}
	}
}

func (s *Server) GetClients() map[string]*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := make(map[string]*Client, len(s.clients))
	for k, v := range s.clients {
		copy[k] = v
	}
	return copy
}

func (s *Server) GetPlayerList() []Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]Player, len(s.playerList))
	copy(list, s.playerList)
	return list
}

func (s *Server) Broadcast(msg string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		select {
		case c.Output <- msg:
		default:
		}
	}
}

func (s *Server) SendTo(playerID string, msg string) bool {
	s.mu.RLock()
	c, ok := s.clients[playerID]
	s.mu.RUnlock()
	if ok {
		select {
		case c.Output <- msg:
			return true
		default:
			return false
		}
	}
	return false
}

func (c *Client) ReadLoop() {
	decoder := json.NewDecoder(c.Conn)
	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			break
		}
		c.Input <- msg.Payload
	}
	close(c.Input)
}

func (c *Client) WriteLoop() {
	encoder := json.NewEncoder(c.Conn)
	for msg := range c.Output {
		if err := encoder.Encode(msg); err != nil {
			break
		}
	}
}

func SendMessage(conn net.Conn, msgType MessageType, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := Message{
		Type:    msgType,
		Payload: payloadBytes,
		Time:    time.Now(),
	}
	return json.NewEncoder(conn).Encode(msg)
}

func ReceiveMessage(conn net.Conn) (*Message, error) {
	var msg Message
	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func HandleConnection(conn net.Conn, server *Server) {
	defer conn.Close()

	var joinMsg Message
	if err := json.NewDecoder(conn).Decode(&joinMsg); err != nil {
		fmt.Println("Failed to read join message:", err)
		return
	}

	var payload JoinPayload
	if err := json.Unmarshal(joinMsg.Payload, &payload); err != nil {
		fmt.Println("Failed to parse join payload:", err)
		return
	}

	client := &Client{
		Conn:     conn,
		PlayerID: generateID(),
		Name:     payload.Name,
		Input:    make(chan []byte, 10),
		Output:   make(chan string, 100),
	}

	server.AddClient(client)

	playerList := server.GetPlayerList()
	SendMessage(conn, MsgPlayerList, playerList)

	go client.ReadLoop()
	go client.WriteLoop()

	for {
		select {
		case input, ok := <-client.Input:
			if !ok {
				server.RemoveClient(client.PlayerID)
				return
			}
			server.Broadcast(fmt.Sprintf("[%s]: %s", client.Name, input))
		}
	}
}

func generateID() string {
	return fmt.Sprintf("player-%d", time.Now().UnixNano())
}

func StartServer(addr string) (*Server, error) {
	server := NewServer()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	server.listener = ln

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				continue
			}
			go HandleConnection(conn, server)
		}
	}()

	return server, nil
}

func DialServer(addr, playerName string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	if err := SendMessage(conn, MsgJoin, JoinPayload{Name: playerName}); err != nil {
		return nil, err
	}

	client := &Client{
		Conn:     conn,
		PlayerID: "",
		Name:     playerName,
		Input:    make(chan []byte, 10),
		Output:   make(chan string, 100),
	}

	go client.ReadLoop()
	go client.WriteLoop()

	return client, nil
}
