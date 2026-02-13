package game

import (
	"fmt"
	"strings"
	"time"
)

func (g *GameState) ProcessTurn(p *Player, action string) string {
	if p == nil {
		return "Error: Player not found.\n"
	}
	if !p.Alive {
		return "Your party has perished. You are spectating.\n"
	}

	result := &strings.Builder{}

	g.TurnPhase = PhaseMainMenu

	switch action {
	case "hunt":
		if g.Bullets >= 50 {
			if p.Type == PlayerTypeCPU {
				// CPU auto-resolves hunting
				result.WriteString(g.HandleHunting(p))
			} else {
				// Interactive: set phase and return prompt
				g.TurnPhase = PhaseHunting
				g.HuntWord = g.GetShootingPrompt()
				g.Bullets -= 50
				result.WriteString("Get ready to shoot...\n")
				return result.String() // Return early — waiting for hunt_shoot
			}
		} else {
			result.WriteString("Not enough bullets to hunt!\n")
			result.WriteString(g.ContinueTravel(p))
		}
	default:
		result.WriteString(g.ContinueTravel(p))
	}

	return result.String()
}

type FortItem struct {
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
	Label string  `json:"label"`
}

func GetFortPrices() map[string]FortItem {
	return map[string]FortItem{
		"food":     {Price: 10, Qty: 25, Label: "Food Pack (25 lbs)"},
		"bullets":  {Price: 5, Qty: 50, Label: "Ammo Box (50 rounds)"},
		"clothing": {Price: 5, Qty: 5, Label: "Clothing (5 sets)"},
		"misc":     {Price: 5, Qty: 5, Label: "Supply Kit (5 kits)"},
	}
}

func (g *GameState) HandleFort(p *Player) string {
	result := &strings.Builder{}

	g.Mileage -= 45
	g.ClampResources()

	if p.Type == PlayerTypeCPU {
		result.WriteString("AT THE FORT - Prices are 50% higher\n")
		if g.Food < 100 && g.Cash >= 10 {
			bundles := int(g.Cash * 0.3 / 10)
			if bundles > 0 {
				cost := float64(bundles) * 10
				g.Food += float64(bundles) * 25
				g.Cash -= cost
			}
		}
		if g.Bullets < 200 && g.Cash >= 5 {
			bundles := int(g.Cash * 0.2 / 5)
			if bundles > 0 {
				cost := float64(bundles) * 5
				g.Bullets += float64(bundles) * 50
				g.Cash -= cost
			}
		}
		if g.Clothing < 30 && g.Cash >= 5 {
			bundles := int(g.Cash * 0.15 / 5)
			if bundles > 0 {
				cost := float64(bundles) * 5
				g.Clothing += float64(bundles) * 5
				g.Cash -= cost
			}
		}
		g.ClampResources()
		result.WriteString("CPU purchased supplies at the fort.\n")
	} else {
		g.TurnPhase = PhaseFort
		result.WriteString("You arrived at the fort. Browse the trading post!\n")
	}

	return result.String()
}

func (g *GameState) HandleFortBuy(item string, qty int) string {
	if g.TurnPhase != PhaseFort {
		return "You're not at a fort!\n"
	}
	if qty <= 0 {
		return "Invalid quantity.\n"
	}

	prices := GetFortPrices()
	fi, ok := prices[item]
	if !ok {
		return "Unknown item.\n"
	}

	cost := fi.Price * float64(qty)
	if cost > g.Cash {
		return fmt.Sprintf("Not enough cash! Need $%.0f but only have $%.0f\n", cost, g.Cash)
	}

	g.Cash -= cost
	gained := fi.Qty * float64(qty)

	switch item {
	case "food":
		g.Food += gained
	case "bullets":
		g.Bullets += gained
	case "clothing":
		g.Clothing += gained
	case "misc":
		g.MiscSupplies += gained
	}

	g.ClampResources()
	return fmt.Sprintf("Bought %.0f %s for $%.0f\n", gained, item, cost)
}

func (g *GameState) HandleFortSell(item string, qty int) string {
	if g.TurnPhase != PhaseFort {
		return "You're not at a fort!\n"
	}
	if qty <= 0 {
		return "Invalid quantity.\n"
	}

	prices := GetFortPrices()
	fi, ok := prices[item]
	if !ok {
		return "Unknown item.\n"
	}

	// Sell at 50% of buy price
	sellPrice := fi.Price * 0.5
	amount := fi.Qty * float64(qty)
	earnings := sellPrice * float64(qty)

	// Check if player has enough to sell
	switch item {
	case "food":
		if g.Food < amount {
			return fmt.Sprintf("Not enough food to sell! Have %.0f, need %.0f\n", g.Food, amount)
		}
		g.Food -= amount
	case "bullets":
		if g.Bullets < amount {
			return fmt.Sprintf("Not enough bullets to sell! Have %.0f, need %.0f\n", g.Bullets, amount)
		}
		g.Bullets -= amount
	case "clothing":
		if g.Clothing < amount {
			return fmt.Sprintf("Not enough clothing to sell! Have %.0f, need %.0f\n", g.Clothing, amount)
		}
		g.Clothing -= amount
	case "misc":
		if g.MiscSupplies < amount {
			return fmt.Sprintf("Not enough supplies to sell! Have %.0f, need %.0f\n", g.MiscSupplies, amount)
		}
		g.MiscSupplies -= amount
	}

	g.Cash += earnings
	g.ClampResources()
	return fmt.Sprintf("Sold %.0f %s for $%.0f\n", amount, item, earnings)
}

func (g *GameState) HandleFortLeave() string {
	g.TurnPhase = PhaseMainMenu
	return "You leave the fort and continue on the trail.\n"
}

func (g *GameState) ContinueTravel(p *Player) string {
	result := &strings.Builder{}

	// Starvation deals HP damage instead of instant death
	if g.Food < 13 {
		result.WriteString("FOOD IS CRITICALLY LOW! Your party is starving!\n")
		// Deal 20 HP damage to all alive members
		for i := range p.Party {
			if p.Party[i].Alive {
				result.WriteString(g.DamagePartyMember(p, i, 20))
			}
		}
		if !p.Alive {
			return result.String()
		}
	}

	eatingLevel := 2
	if p.Type == PlayerTypeCPU {
		if g.Food > 200 {
			eatingLevel = 3
		} else if g.Food > 100 {
			eatingLevel = 2
		} else {
			eatingLevel = 1
		}
	}

	foodConsumed := 8 + 5*eatingLevel
	g.Food -= float64(foodConsumed)

	// Adjusted travel: ~80-95 miles/turn for 4500 mile trail
	baseTravel := 80.0 + (g.OxenCost-220)/5 + g.Rand.Float64()*15
	g.Mileage += baseTravel

	result.WriteString(fmt.Sprintf("\nYou traveled %.0f miles this week.\n", baseTravel))

	result.WriteString(g.HandleRiverCrossing(p))

	// Check for riders — interactive for humans, auto for CPU
	if baseTravel > 1 && !g.GameOver && p.Alive {
		if g.CheckRiders() {
			if p.Type == PlayerTypeHuman {
				// Store state and pause for player choice
				g.TurnPhase = PhaseRiders
				g.PendingEatingLevel = eatingLevel
				if g.PendingRiderHostile {
					result.WriteString(fmt.Sprintf("\nRIDERS AHEAD! %d hostile riders approaching!\n", g.PendingRiderCount))
				} else {
					result.WriteString(fmt.Sprintf("\nRIDERS AHEAD. %d riders, they don't look hostile.\n", g.PendingRiderCount))
				}
				return result.String() // Pause — waiting for rider_tactic
			}
			// CPU auto-resolves
			tactic := g.cpuChooseTactic(g.PendingRiderHostile)
			result.WriteString(g.ResolveRiderTactic(p, tactic))
		}
	}

	if !g.GameOver && p.Alive {
		result.WriteString(g.FinishTurn(p, eatingLevel))
	}

	return result.String()
}

// FinishTurn completes the rest of a turn after riders are resolved.
func (g *GameState) FinishTurn(p *Player, eatingLevel int) string {
	result := &strings.Builder{}

	if !g.GameOver && p.Alive {
		result.WriteString(g.HandleRandomEvent(p))
	}

	if !g.GameOver && p.Alive && g.Mileage > float64(MountainThreshold) {
		result.WriteString(g.HandleMountains(p))
	}

	g.HandleEatingResult(p, eatingLevel)

	g.ClampResources()

	if !g.GameOver && g.Mileage >= float64(TrailLength) {
		g.HandleFinalTurn(p)
	}

	return result.String()
}

// HandleHuntShoot resolves a hunting attempt based on player reaction time.
func (g *GameState) HandleHuntShoot(p *Player, reactionTimeMs int) string {
	if p == nil {
		return "Error: Player not found.\n"
	}
	result := &strings.Builder{}

	// Calculate accuracy from reaction time
	var accuracy float64
	switch {
	case reactionTimeMs < 300:
		accuracy = 0
	case reactionTimeMs < 600:
		accuracy = 1 + float64(reactionTimeMs-300)/300.0
	case reactionTimeMs < 1000:
		accuracy = 3 + float64(reactionTimeMs-600)/250.0
	case reactionTimeMs < 2000:
		accuracy = 6 + float64(reactionTimeMs-1000)/500.0
	default:
		accuracy = 9
	}

	if accuracy <= 2 {
		foodGained := 52 + g.Rand.Float64()*6
		g.Food += foodGained
		result.WriteString(fmt.Sprintf("RIGHT BETWEEN THE EYES! You got a big one!\nFull bellies tonight! (+%.0f food)\n", foodGained))
	} else if g.Rand.Float64()*100 < 13*accuracy {
		result.WriteString("You missed - and your dinner got away...\n")
	} else {
		foodGained := 48 - 2*accuracy
		g.Food += foodGained
		result.WriteString(fmt.Sprintf("Nice shot! Right on target! Good eatin' tonight! (+%.0f food)\n", foodGained))
	}

	g.Bullets -= 10 + 3*accuracy
	if g.Bullets < 0 {
		g.Bullets = 0
	}

	// Hunting adds reduced travel distance
	huntTravel := 45 + g.Rand.Float64()*20
	g.Mileage += huntTravel
	result.WriteString(fmt.Sprintf("You traveled %.0f miles while hunting.\n", huntTravel))

	g.ClampResources()
	g.TurnPhase = PhaseMainMenu

	// Check win condition
	if g.Mileage >= float64(TrailLength) {
		g.HandleFinalTurn(p)
	}

	return result.String()
}

// HandleRiderTactic resolves a rider encounter with the player's chosen tactic,
// then finishes the rest of the turn.
func (g *GameState) HandleRiderTactic(p *Player, tactic int) string {
	if p == nil {
		return "Error: Player not found.\n"
	}
	result := &strings.Builder{}

	result.WriteString(g.ResolveRiderTactic(p, tactic))

	if !g.GameOver && p.Alive {
		result.WriteString(g.FinishTurn(p, g.PendingEatingLevel))
	}

	g.TurnPhase = PhaseMainMenu
	return result.String()
}

func (g *GameState) HandleEatingResult(p *Player, eatingLevel int) string {
	result := &strings.Builder{}
	result.WriteString(g.HandleIllness(p, eatingLevel))
	return result.String()
}

func (g *GameState) HandleMountains(p *Player) string {
	result := &strings.Builder{}

	result.WriteString("\n*** MOUNTAINS ***\n")

	mileRef := g.Mileage / 100
	baseChance := mileRef - float64(MountainThreshold)/100
	mountainFactor := (9 - (baseChance*baseChance+72)/(baseChance*baseChance+12))
	if g.Rand.Float64()*10*mountainFactor > 0 {
		result.WriteString("RUGGED MOUNTAINS\n")

		r := g.Rand.Float64()
		if r <= 0.1 {
			result.WriteString("YOU GOT LOST - Lose valuable time trying to find trail!\n")
			g.Mileage -= 60
		} else if r <= 0.11 {
			result.WriteString("WAGON DAMAGED! - Lose time and supplies\n")
			g.MiscSupplies -= 5
			g.Bullets -= 20
			g.Mileage -= 20 + g.Rand.Float64()*30
		} else {
			result.WriteString("THE GOING GETS SLOW\n")
			g.Mileage -= 45 + g.Rand.Float64()*50
		}
	}

	// Blizzard in final stretch (3800-4500)
	if g.Mileage > 3800 && g.Mileage < float64(TrailLength) {
		if g.Rand.Float64() < 0.3 {
			result.WriteString("BLIZZARD IN MOUNTAIN PASS - Time and supplies lost\n")
			g.Food -= 25
			g.MiscSupplies -= 10
			g.Bullets -= 30
			g.Mileage -= 30 + g.Rand.Float64()*40

			if g.Clothing < 18+g.Rand.Float64()*2 {
				result.WriteString(g.HandleIllness(p, 2))
			}
		}
	}

	g.ClampResources()
	return result.String()
}

func (g *GameState) HandleFinalTurn(p *Player) string {
	result := &strings.Builder{}

	result.WriteString("\n*** CONGRATULATIONS! ***\n")
	result.WriteString("YOU FINALLY ARRIVED AT ONLINE CITY\n")
	result.WriteString(fmt.Sprintf("AFTER %d LONG MILES - HOORAY!!!!!\n", TrailLength))
	result.WriteString("A REAL PIONEER!\n")

	arrivalDate := g.calculateArrivalDate()
	result.WriteString(fmt.Sprintf("Arrival: %s\n", arrivalDate))

	// Show party survivors
	survivors := 0
	for _, m := range p.Party {
		if m.Alive {
			survivors++
		}
	}
	result.WriteString(fmt.Sprintf("\nSurvivors: %d of %d party members\n", survivors, len(p.Party)))

	result.WriteString("\nFINAL INVENTORY:\n")
	result.WriteString(fmt.Sprintf("  Food: %.0f\n", g.Food))
	result.WriteString(fmt.Sprintf("  Bullets: %.0f\n", g.Bullets))
	result.WriteString(fmt.Sprintf("  Clothing: %.0f\n", g.Clothing))
	result.WriteString(fmt.Sprintf("  Misc Supplies: %.0f\n", g.MiscSupplies))
	result.WriteString(fmt.Sprintf("  Cash: $%.2f\n", g.Cash))

	result.WriteString("\nPRESIDENT JAMES K. POLK SENDS YOU HIS\n")
	result.WriteString("HEARTIEST CONGRATULATIONS\n")
	result.WriteString("AND WISHES YOU A PROSPEROUS LIFE AHEAD\n")
	result.WriteString("AT YOUR NEW HOME\n")

	g.GameOver = true
	g.Win = true
	g.FinalDate = arrivalDate

	return result.String()
}

func (g *GameState) calculateArrivalDate() string {
	days := g.TurnNumber * 7
	month := "JULY"
	day := 1

	baseDate := time.Date(1847, time.March, 29, 0, 0, 0, 0, time.UTC)
	arrival := baseDate.AddDate(0, 0, days)

	month = arrival.Month().String()
	day = arrival.Day()

	return fmt.Sprintf("%s %d, 1847", month, day)
}

func (g *GameState) formatStatus() string {
	result := &strings.Builder{}

	dates := []string{"APRIL 12", "APRIL 26", "MAY 10", "MAY 24", "JUNE 7", "JUNE 21",
		"JULY 5", "JULY 19", "AUGUST 2", "AUGUST 16", "AUGUST 31", "SEPTEMBER 13",
		"SEPTEMBER 27", "OCTOBER 11", "OCTOBER 25", "NOVEMBER 8", "NOVEMBER 22",
		"DECEMBER 6", "DECEMBER 20", "JANUARY 3", "JANUARY 17", "JANUARY 31",
		"FEBRUARY 14", "FEBRUARY 28", "MARCH 14", "MARCH 28"}

	week := (g.TurnNumber / 4)
	if week >= len(dates) {
		week = len(dates) - 1
	}

	result.WriteString(fmt.Sprintf("\nMONDAY %s 1847\n", dates[week]))
	result.WriteString(fmt.Sprintf("\nTOTAL MILEAGE IS %.0f\n", g.Mileage))
	result.WriteString("\nRESOURCES:\n")
	result.WriteString(fmt.Sprintf("  FOOD          BULLETS     CLOTHING    MISC       CASH\n"))
	result.WriteString(fmt.Sprintf("  %.0f          %.0f         %.0f        %.0f       $%.0f\n",
		g.Food, g.Bullets, g.Clothing, g.MiscSupplies, g.Cash))

	if g.Food < 13 {
		result.WriteString("\nYOU'D BETTER DO SOME HUNTING OR BUY FOOD AND SOON!!!!\n")
	}

	return result.String()
}

func (g *GameState) InitialPurchase(p *Player) (float64, float64, float64, float64, float64, float64) {
	oxen := 220.0
	food := 100.0
	bullets := 50.0
	clothing := 20.0
	misc := 10.0
	cash := 700.0

	if p.Type == PlayerTypeCPU {
		oxen = 200 + g.Rand.Float64()*100
		food = 150 + g.Rand.Float64()*150
		bullets = 50 + g.Rand.Float64()*100
		clothing = 25 + g.Rand.Float64()*30
		misc = 15 + g.Rand.Float64()*20
		cash = 700 - oxen - food - bullets - clothing - misc
		if cash < 0 {
			cash = 0
			oxen = 200
			food = 200
			bullets = 100
			clothing = 50
			misc = 30
			cash = 120
		}
	}

	return oxen, food, bullets, clothing, misc, cash
}

func (g *GameState) StartGame() string {
	result := &strings.Builder{}

	result.WriteString("\n======================================\n")
	result.WriteString("       THE ONLINE TRAIL\n")
	result.WriteString("======================================\n\n")

	result.WriteString("DO YOU NEED INSTRUCTIONS (YES/NO)?\n")

	return result.String()
}

func (g *GameState) ShowInstructions() string {
	result := &strings.Builder{}

	result.WriteString("\n*** INSTRUCTIONS ***\n\n")
	result.WriteString("THIS PROGRAM SIMULATES A TRIP OVER THE ONLINE TRAIL FROM\n")
	result.WriteString("INDEPENDENCE, MISSOURI TO ONLINE CITY, OREGON IN 1847.\n")
	result.WriteString(fmt.Sprintf("YOUR FAMILY OF FIVE WILL COVER THE %d MILE ONLINE TRAIL\n", TrailLength))
	result.WriteString("IN 5-6 MONTHS --- IF YOU MAKE IT ALIVE.\n\n")

	result.WriteString("YOU HAD SAVED $900 TO SPEND FOR THE TRIP, AND YOU'VE JUST\n")
	result.WriteString("   PAID $200 FOR A WAGON.\n")
	result.WriteString("YOU WILL NEED TO SPEND THE REST OF YOUR MONEY ON THE\n")
	result.WriteString("   FOLLOWING ITEMS:\n\n")

	result.WriteString("     OXEN - YOU CAN SPEND $200-$300 ON YOUR TEAM\n")
	result.WriteString("            THE MORE YOU SPEND, THE FASTER YOU'LL GO\n\n")

	result.WriteString("     FOOD - THE MORE YOU HAVE, THE LESS CHANCE THERE\n")
	result.WriteString("            IS OF GETTING SICK\n\n")

	result.WriteString("AMMUNITION - $1 BUYS A BELT OF 50 BULLETS\n")
	result.WriteString("            YOU WILL NEED BULLETS FOR ATTACKS BY ANIMALS\n")
	result.WriteString("            AND BANDITS, AND FOR HUNTING FOOD\n\n")

	result.WriteString("CLOTHING - THIS IS ESPECIALLY IMPORTANT FOR THE COLD\n")
	result.WriteString("            WEATHER YOU WILL ENCOUNTER WHEN CROSSING\n")
	result.WriteString("            THE MOUNTAINS\n\n")

	result.WriteString("MISCELLANEOUS SUPPLIES - THIS INCLUDES MEDICINE AND\n")
	result.WriteString("            OTHER THINGS YOU WILL NEED FOR SICKNESS\n")
	result.WriteString("            AND EMERGENCY REPAIRS\n\n")

	result.WriteString("YOU CAN SPEND ALL YOUR MONEY BEFORE YOU START YOUR TRIP -\n")
	result.WriteString("OR YOU CAN SAVE SOME OF YOUR CASH TO SPEND AT FORTS ALONG\n")
	result.WriteString("THE WAY WHEN YOU RUN LOW. HOWEVER, ITEMS COST MORE AT\n")
	result.WriteString("THE FORTS. YOU CAN ALSO GO HUNTING ALONG THE WAY TO GET\n")
	result.WriteString("MORE FOOD.\n")

	result.WriteString("GOOD LUCK!!!\n\n")

	return result.String()
}

func (g *GameState) GetShootingPrompt() string {
	wordIdx := g.Rand.Intn(len(shootingWords))
	return shootingWords[wordIdx]
}
