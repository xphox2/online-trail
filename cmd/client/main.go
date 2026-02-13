package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"online-trail/pkg/game"
	"online-trail/pkg/network"
)

type Client struct {
	conn     net.Conn
	encoder  *json.Encoder
	decoder  *json.Decoder
	game     *game.GameState
	playerID string
	name     string
	input    *bufio.Reader
}

func NewClient(addr, name string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	c := &Client{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
		game:    game.NewGameState(),
		name:    name,
		input:   bufio.NewReader(os.Stdin),
	}

	if err := c.encoder.Encode(network.Message{
		Type:    network.MsgJoin,
		Payload: []byte(fmt.Sprintf(`{"name":%q}`, name)),
	}); err != nil {
		conn.Close()
		return nil, err
	}

	return c, nil
}

func (c *Client) Run() error {
	fmt.Println("Connected to Online Trail server!")
	fmt.Println("Waiting for game to start...")

	go c.readFromServer()

	for {
		if c.game.TurnPhase == game.PhaseGameOver {
			c.printGameOver()
			break
		}

		if c.game.Players == nil || len(c.game.Players) == 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		currentPlayer := c.game.GetCurrentPlayer()
		if currentPlayer == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if currentPlayer.ID != c.playerID {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		c.printGameState()
		c.handleTurn()
	}

	return nil
}

func (c *Client) readFromServer() {
	for {
		var msg network.Message
		if err := c.decoder.Decode(&msg); err != nil {
			break
		}

		switch msg.Type {
		case network.MsgJoin:
			var payload struct {
				ID string `json:"id"`
			}
			json.Unmarshal(msg.Payload, &payload)
			c.playerID = payload.ID
			fmt.Println("Your player ID:", c.playerID)

		case network.MsgGameState:
			json.Unmarshal(msg.Payload, c.game)
			if c.game.TurnPhase == game.PhaseGameOver {
				return
			}

		case network.MsgChat:
			var payload struct {
				Sender  string `json:"sender"`
				Message string `json:"message"`
			}
			json.Unmarshal(msg.Payload, &payload)
			fmt.Printf("[%s]: %s\n", payload.Sender, payload.Message)

		case network.MsgPlayerList:
			var players []network.Player
			json.Unmarshal(msg.Payload, &players)
			fmt.Println("Players in game:")
			for _, p := range players {
				fmt.Printf("  - %s\n", p.Name)
			}
		}
	}
}

func (c *Client) printGameState() {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Printf("Turn: %d | Mileage: %.0f\n", c.game.TurnNumber, c.game.Mileage)
	fmt.Println("-" + strings.Repeat("-", 49))
	fmt.Printf("FOOD: %.0f  BULLETS: %.0f  CLOTHING: %.0f\n",
		c.game.Food, c.game.Bullets, c.game.Clothing)
	fmt.Printf("MISC: %.0f  CASH: $%.0f\n", c.game.MiscSupplies, c.game.Cash)
	fmt.Println(strings.Repeat("=", 50))
}

func (c *Client) handleTurn() {
	p := c.game.GetCurrentPlayer()
	if p == nil {
		return
	}

	fmt.Println("\nYour turn! Choose an action:")

	hasFort := c.game.TurnNumber > 0 && c.game.TurnNumber%2 == 1
	_ = hasFort

	if hasFort {
		fmt.Println("  (1) Stop at the next fort")
		fmt.Println("  (2) Hunt")
		fmt.Println("  (3) Continue")
	} else {
		fmt.Println("  (1) Hunt")
		fmt.Println("  (2) Continue")
	}

	fmt.Print("\n> ")

	line, _ := c.input.ReadString('\n')
	line = strings.TrimSpace(line)

	action := "continue"
	switch line {
	case "1":
		if hasFort {
			action = "fort"
		} else {
			action = "hunt"
		}
	case "2":
		if hasFort {
			action = "hunt"
		} else {
			action = "continue"
		}
	case "3":
		action = "continue"
	}

	c.encoder.Encode(network.Message{
		Type: network.MsgAction,
		Payload: []byte(fmt.Sprintf(`{"player_id":%q,"action":%q,"value":""}`,
			c.playerID, action)),
	})

	time.Sleep(200 * time.Millisecond)
}

func (c *Client) printGameOver() {
	fmt.Println("\n" + strings.Repeat("*", 50))
	if c.game.Win {
		fmt.Println("       CONGRATULATIONS!")
		fmt.Println("   YOU MADE IT TO ONLINE CITY!")
		fmt.Printf("   Arrival date: %s\n", c.game.FinalDate)
	} else {
		fmt.Println("         GAME OVER")
		fmt.Println("     Your party did not survive...")
	}
	fmt.Println(strings.Repeat("*", 50))

	fmt.Println("\nFINAL INVENTORY:")
	fmt.Printf("  Food: %.0f\n", c.game.Food)
	fmt.Printf("  Bullets: %.0f\n", c.game.Bullets)
	fmt.Printf("  Clothing: %.0f\n", c.game.Clothing)
	fmt.Printf("  Misc Supplies: %.0f\n", c.game.MiscSupplies)
	fmt.Printf("  Cash: $%.0f\n", c.game.Cash)
}

func main() {
	addr := flag.String("addr", "localhost:5555", "Server address")
	name := flag.String("name", "Player", "Your name")
	flag.Parse()

	c, err := NewClient(*addr, *name)
	if err != nil {
		log.Fatal(err)
	}
	defer c.conn.Close()

	if err := c.Run(); err != nil {
		log.Fatal(err)
	}
}
