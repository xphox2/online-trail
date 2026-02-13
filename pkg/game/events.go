package game

import (
	"fmt"
	"strings"
)

func (g *GameState) HandleRiverCrossing(p *Player) string {
	result := &strings.Builder{}

	// Kansas River: 600-1200
	if g.Mileage >= 600 && g.Mileage < 1200 {
		result.WriteString("KANSAS RIVER CROSSING\n")
		if g.Rand.Float64() < 0.15 {
			result.WriteString("Your wagon was swamped!\n")
			g.Food -= 30
			g.Clothing -= 20
			g.Mileage -= g.Rand.Float64()*20 + 20
			g.ClampResources()
			result.WriteString(g.DamageRandomMember(p, 5))
		} else {
			result.WriteString("You crossed safely.\n")
		}
	} else if g.Mileage >= 2000 && g.Mileage < 2600 {
		// Green River: 2000-2600
		result.WriteString("GREEN RIVER CROSSING\n")
		if g.Rand.Float64() < 0.2 {
			result.WriteString("Strong currents! You lost supplies!\n")
			g.Food -= 40
			g.MiscSupplies -= 10
			g.Mileage -= g.Rand.Float64()*30 + 25
			g.ClampResources()
			result.WriteString(g.DamageRandomMember(p, 10))
		} else {
			result.WriteString("Safe crossing.\n")
		}
	} else if g.Mileage >= 3000 && g.Mileage < 3400 {
		// Snake River: 3000-3400 (NEW)
		result.WriteString("SNAKE RIVER CROSSING\n")
		if g.Rand.Float64() < 0.22 {
			result.WriteString("Treacherous waters! The wagon nearly capsized!\n")
			g.Food -= 35
			g.Bullets -= 30
			g.Mileage -= g.Rand.Float64()*25 + 20
			g.ClampResources()
			result.WriteString(g.DamageRandomMember(p, 15))
		} else {
			result.WriteString("Careful crossing - you made it!\n")
		}
	} else if g.Mileage >= 3800 && g.Mileage < 4200 {
		// Columbia River: 3800-4200
		result.WriteString("COLUMBIA RIVER - THE FINAL RIVER\n")
		if g.Rand.Float64() < 0.25 {
			result.WriteString("Dangerous rapids! Supplies lost!\n")
			g.Food -= 50
			g.Clothing -= 30
			g.Mileage -= g.Rand.Float64()*40 + 30
			g.ClampResources()
			result.WriteString(g.DamageRandomMember(p, 20))
		} else {
			result.WriteString("You made it across!\n")
		}
	}

	return result.String()
}

func (g *GameState) HandleHunting(p *Player) string {
	result := &strings.Builder{}

	if g.Bullets < 50 {
		result.WriteString("Not enough bullets to hunt!\n")
		return result.String()
	}

	g.Bullets -= 50

	shootTime := g.getShootingTime(p)
	accuracy := g.calculateAccuracy(shootTime, p.ShootingRank)

	if accuracy <= 1 {
		foodGained := 52 + g.Rand.Float64()*6
		g.Food += foodGained
		result.WriteString(fmt.Sprintf("RIGHT BETWEEN THE EYES! You got a big one!\nFull bellies tonight! (+%.0f food)\n", foodGained))
	} else if g.Rand.Float64()*100 < 13*float64(accuracy) {
		result.WriteString("You missed - and your dinner got away...\n")
	} else {
		foodGained := 48 - 2*float64(accuracy)
		g.Food += foodGained
		result.WriteString(fmt.Sprintf("Nice shot! Right on target! Good eatin' tonight! (+%.0f food)\n", foodGained))
	}

	g.Bullets -= 10 + 3*float64(accuracy)
	if g.Bullets < 0 {
		g.Bullets = 0
	}

	// Hunting adds reduced travel distance for 4500 mile trail
	huntTravel := 45 + g.Rand.Float64()*20
	g.Mileage += huntTravel
	result.WriteString(fmt.Sprintf("You traveled %.0f miles while hunting.\n", huntTravel))

	g.ClampResources()

	if g.Mileage >= float64(TrailLength) {
		g.HandleFinalTurn(p)
	}

	return result.String()
}

func (g *GameState) HandleIllness(p *Player, eatingLevel int) string {
	result := &strings.Builder{}

	var illnessChance float64
	switch eatingLevel {
	case 1:
		illnessChance = 0.65
	case 2:
		illnessChance = 0.50
	case 3:
		illnessChance = 0.25
	}

	if g.Rand.Float64() < illnessChance {
		severity := g.Rand.Float64()
		if severity < 0.33 {
			result.WriteString("MILD ILLNESS - Medicine used\n")
			g.Mileage -= 5
			g.MiscSupplies -= 2
			result.WriteString(g.DamageRandomMember(p, 10))
		} else if severity < 0.66 {
			result.WriteString("BAD ILLNESS - Medicine used\n")
			g.Mileage -= 5
			g.MiscSupplies -= 5
			result.WriteString(g.DamageRandomMember(p, 20))
		} else {
			result.WriteString("SERIOUS ILLNESS - Must stop for medical attention\n")
			g.MiscSupplies -= 10
			result.WriteString(fmt.Sprintf("Doctor's bill is $20\n"))
			g.Cash -= 20
			result.WriteString(g.DamageRandomMember(p, 30))
		}

		if g.MiscSupplies < 0 && !g.GameOver {
			result.WriteString("You ran out of medical supplies!\n")
			result.WriteString(g.DamageRandomMember(p, 40))
		}
	}

	return result.String()
}

// CheckRiders determines if riders appear and returns true if they do.
// It sets PendingRiderHostile and PendingRiderCount on the game state.
func (g *GameState) CheckRiders() bool {
	baseChance := float64(g.Mileage)/100 - 4
	chance := baseChance*baseChance + 72
	chance = chance / (baseChance*baseChance + 12)
	chance = chance * 10 * g.Rand.Float64()

	if chance > 1 {
		return false
	}

	g.PendingRiderHostile = g.Rand.Float64() < 0.8
	g.PendingRiderCount = 3 + g.Rand.Intn(8)
	return true
}

// ResolveRiderTactic resolves a rider encounter with the given tactic.
func (g *GameState) ResolveRiderTactic(p *Player, tactic int) string {
	result := &strings.Builder{}
	hostile := g.PendingRiderHostile

	if hostile {
		switch tactic {
		case 1: // Run
			g.Mileage += 20
			g.MiscSupplies -= 15
			g.Bullets -= 50
			g.OxenCost -= 40
			result.WriteString("You fled from the riders!\n")
			// Running has a chance of taking damage
			if g.Rand.Float64() < 0.3 {
				result.WriteString("They got some shots off as you fled!\n")
				result.WriteString(g.DamageRandomMember(p, 15))
			}
		case 2: // Attack
			shootTime := g.getShootingTime(p)
			accuracy := g.calculateAccuracy(shootTime, p.ShootingRank)
			g.Bullets -= float64(accuracy)*40 + 80

			if accuracy <= 1 {
				result.WriteString("NICE SHOOTING - You drove them off!\n")
			} else if accuracy > 4 {
				result.WriteString("LOUSY SHOT - You got knifed!\n")
				result.WriteString("You have to see the doctor.\n")
				g.Cash -= 20
				result.WriteString(g.DamageRandomMember(p, 25))
			} else {
				result.WriteString("Kinda slow with your Colt .45\n")
				result.WriteString(g.DamageRandomMember(p, 15))
			}
		case 3: // Continue
			if g.Rand.Float64() > 0.8 {
				result.WriteString("They did not attack.\n")
				g.ClampResources()
				return result.String()
			}
			g.Bullets -= 50
			g.MiscSupplies -= 15
			result.WriteString("They attacked and you defended.\n")
			result.WriteString(g.DamageRandomMember(p, 20))
		case 4: // Circle Wagons
			shootTime := g.getShootingTime(p)
			accuracy := g.calculateAccuracy(shootTime, p.ShootingRank)
			g.Bullets -= float64(accuracy)*30 + 80
			g.Mileage -= 25
			if accuracy <= 1 {
				result.WriteString("NICE SHOOTING - You drove them off!\n")
			} else if accuracy > 4 {
				result.WriteString("LOUSY SHOT - You got knifed!\n")
				g.Cash -= 20
				result.WriteString(g.DamageRandomMember(p, 30))
			} else {
				result.WriteString("KINDA SLOW - They got some licks in\n")
				result.WriteString(g.DamageRandomMember(p, 15))
			}
		}
	} else {
		switch tactic {
		case 1:
			g.Mileage += 15
			g.OxenCost -= 10
			result.WriteString("You ran from friendly riders. Wasted energy.\n")
		case 2:
			g.Mileage -= 5
			g.Bullets -= 50
			result.WriteString("You attacked friendly riders! They fought back.\n")
			result.WriteString(g.DamageRandomMember(p, 20))
		case 3:
			result.WriteString("They passed by peacefully. Nothing happened.\n")
		case 4:
			g.Mileage -= 20
			result.WriteString("You circled wagons but they meant no harm. Time lost.\n")
		}
	}

	if g.Bullets < 0 {
		result.WriteString("You ran out of bullets in the fight!\n")
		result.WriteString(g.DamageRandomMember(p, 50))
	}

	g.ClampResources()
	return result.String()
}

// HandleRiders is used for CPU auto-resolution of rider encounters.
func (g *GameState) HandleRiders(p *Player) string {
	if !g.CheckRiders() {
		return ""
	}

	result := &strings.Builder{}
	if g.PendingRiderHostile {
		result.WriteString(fmt.Sprintf("RIDERS AHEAD! %d hostile riders!\n", g.PendingRiderCount))
	} else {
		result.WriteString(fmt.Sprintf("RIDERS AHEAD. %d riders, they don't look hostile.\n", g.PendingRiderCount))
	}

	tactic := 3
	if p.Type == PlayerTypeCPU {
		tactic = g.cpuChooseTactic(g.PendingRiderHostile)
	}

	result.WriteString(g.ResolveRiderTactic(p, tactic))
	return result.String()
}

func (g *GameState) cpuChooseTactic(hostile bool) int {
	if !hostile {
		return 3
	}
	weights := []float64{0.2, 0.3, 0.2, 0.3}
	r := g.Rand.Float64()
	sum := 0.0
	for i, w := range weights {
		sum += w
		if r <= sum {
			return i + 1
		}
	}
	return 3
}

func (g *GameState) getShootingTime(p *Player) float64 {
	if p.Type == PlayerTypeCPU {
		baseTime := 0.5 + g.Rand.Float64()*1.5
		return baseTime - float64(p.ShootingRank-1)*0.15
	}
	return 0
}

func (g *GameState) calculateAccuracy(shootTime float64, shootingRank int) float64 {
	baseTime := shootTime * 3600
	accuracy := baseTime - float64(shootingRank-1)
	if accuracy <= 0 {
		accuracy = 0
	}
	if accuracy > 9 {
		accuracy = 9
	}
	return accuracy
}

func (g *GameState) HandleRandomEvent(p *Player) string {
	result := &strings.Builder{}

	type eventFunc func(*Player) string
	events := []eventFunc{
		g.eventWagonBreakdown,
		g.eventOxInjury,
		g.eventDaughterBrokenArm,
		g.eventOxWandersOff,
		g.eventSonGetsLost,
		g.eventUnsafeWater,
		g.eventHeavyRains,
		g.eventBandits,
		g.eventFireInWagon,
		g.eventLostInFog,
		g.eventSnakeBite,
		g.eventWagonSwamped,
		g.eventWildAnimals,
		g.eventHailStorm,
		g.eventBadFood,
	}

	r := g.Rand.Float64() * 100
	thresholds := []float64{6, 11, 13, 15, 17, 22, 32, 35, 37, 42, 44, 54, 64, 69, 100}

	eventIdx := 0
	for i, t := range thresholds {
		if r < t {
			eventIdx = i
			break
		}
	}

	result.WriteString(events[eventIdx](p))

	// 5% chance to find abandoned wagon (separate from normal events)
	if g.Rand.Float64() < 0.05 {
		result.WriteString(g.eventAbandonedWagon(p))
	}

	return result.String()
}

func (g *GameState) eventWagonBreakdown(p *Player) string {
	g.Mileage -= 15 + g.Rand.Float64()*5
	g.MiscSupplies -= 8
	return "WAGON BREAKS DOWN - Lose time and supplies fixing it\n"
}

func (g *GameState) eventOxInjury(p *Player) string {
	g.Mileage -= 25
	g.OxenCost -= 20
	return "OX INJURES LEG - Slows you down rest of trip\n"
}

func (g *GameState) eventDaughterBrokenArm(p *Player) string {
	result := "BAD LUCK - Your daughter broke her arm\nYou had to stop and use supplies to make a sling\n"
	g.Mileage -= 5 + g.Rand.Float64()*4
	g.MiscSupplies -= 2 + g.Rand.Float64()*3
	// Damage daughter (index 3) specifically
	if len(p.Party) > 3 && p.Party[3].Alive {
		result += g.DamagePartyMember(p, 3, 10)
	}
	return result
}

func (g *GameState) eventOxWandersOff(p *Player) string {
	g.Mileage -= 17
	return "OX WANDERS OFF - Spend time looking for it\n"
}

func (g *GameState) eventSonGetsLost(p *Player) string {
	result := "YOUR SON GETS LOST - Spend half the day looking for him\n"
	g.Mileage -= 10
	// Damage son (index 2) specifically
	if len(p.Party) > 2 && p.Party[2].Alive {
		result += g.DamagePartyMember(p, 2, 8)
	}
	return result
}

func (g *GameState) eventUnsafeWater(p *Player) string {
	g.Mileage -= 10 + g.Rand.Float64()*10
	result := "UNSAFE WATER - Lose time looking for clean spring\n"
	result += g.DamageRandomMember(p, 8)
	return result
}

func (g *GameState) eventHeavyRains(p *Player) string {
	if g.Mileage > float64(MountainThreshold) {
		if g.Clothing > 22+g.Rand.Float64()*4 {
			return "COLD WEATHER - You have enough clothing to keep you warm\n"
		}
		result := "COLD WEATHER - You don't have enough clothing! Risk of illness.\n"
		result += g.DamageRandomMember(p, 12)
		return result
	}
	g.Food -= 10
	g.Bullets -= 50
	g.MiscSupplies -= 15
	g.Mileage -= 10 + g.Rand.Float64()*10
	return "HEAVY RAINS - Time and supplies lost\n"
}

func (g *GameState) eventBandits(p *Player) string {
	shootTime := g.getShootingTime(&Player{Type: PlayerTypeCPU})
	accuracy := g.calculateAccuracy(shootTime, 3)
	g.Bullets -= 20 * accuracy

	if g.Bullets < 0 {
		g.Cash /= 3
		g.Cash -= 20
		g.OxenCost -= 20
		g.MiscSupplies -= 5
		result := "BANDITS ATTACK - You ran out of bullets! They took cash and an ox!\n"
		result += g.DamageRandomMember(p, 30)
		return result
	}

	if accuracy <= 1 {
		// Player won the fight - loot the bandits!
		lootCash := 30.0 + g.Rand.Float64()*50
		lootFood := 15.0 + g.Rand.Float64()*25
		lootBullets := 30.0 + g.Rand.Float64()*70

		g.Cash += lootCash
		g.Food += lootFood
		g.Bullets += lootBullets

		result := &strings.Builder{}
		result.WriteString("BANDITS ATTACK - Quickest draw outside of Dodge City! You got 'em!\n")
		result.WriteString(fmt.Sprintf("You looted their camp: $%.0f cash, %.0f food, %.0f bullets!\n", lootCash, lootFood, lootBullets))
		g.ClampResources()
		return result.String()
	}

	g.OxenCost -= 20
	g.MiscSupplies -= 5
	result := "BANDITS ATTACK - You got shot in the leg! Better have a doc look at it.\n"
	result += g.DamageRandomMember(p, 20)
	return result
}

func (g *GameState) eventFireInWagon(p *Player) string {
	g.Food -= 40
	g.Bullets -= 40
	g.MiscSupplies -= 3 + g.Rand.Float64()*8
	g.Mileage -= 15
	result := "FIRE IN WAGON - Food and supplies damaged\n"
	if g.Rand.Float64() < 0.3 {
		result += g.DamageRandomMember(p, 15)
	}
	return result
}

func (g *GameState) eventLostInFog(p *Player) string {
	g.Mileage -= 10 + g.Rand.Float64()*5
	return "LOST IN HEAVY FOG - Time is lost\n"
}

func (g *GameState) eventSnakeBite(p *Player) string {
	g.Bullets -= 10
	g.MiscSupplies -= 5
	result := "SNAKE BITE! "
	if g.MiscSupplies < 0 {
		result += "No medicine available!\n"
		result += g.DamageRandomMember(p, 40)
		return result
	}
	result += "You killed a poisonous snake after it bit you\n"
	result += g.DamageRandomMember(p, 25)
	return result
}

func (g *GameState) eventWagonSwamped(p *Player) string {
	g.Food -= 30
	g.Clothing -= 20
	g.Mileage -= 20 + g.Rand.Float64()*20
	return "WAGON GETS SWAMPED FORDING RIVER - Lose food and clothes\n"
}

func (g *GameState) eventWildAnimals(p *Player) string {
	shootTime := g.getShootingTime(&Player{Type: PlayerTypeCPU})
	accuracy := g.calculateAccuracy(shootTime, 3)

	if g.Bullets < 40 {
		result := "WILD ANIMALS ATTACK - You were too low on bullets! The wolves overpowered you.\n"
		result += g.DamageRandomMember(p, 35)
		return result
	}

	g.Bullets -= 20 * accuracy
	g.Clothing -= accuracy * 4
	g.Food -= accuracy * 8

	if accuracy <= 2 {
		return "NICE SHOOTIN' PARTNER - They didn't get much\n"
	}
	result := "SLOW ON THE DRAW - They got at your food and clothes\n"
	result += g.DamageRandomMember(p, 15)
	return result
}

func (g *GameState) eventHailStorm(p *Player) string {
	g.Mileage -= 5 + g.Rand.Float64()*10
	g.Bullets -= 20
	g.MiscSupplies -= 4 + g.Rand.Float64()*3
	result := "HAIL STORM - Supplies damaged\n"
	if g.Rand.Float64() < 0.2 {
		result += g.DamageRandomMember(p, 10)
	}
	return result
}

func (g *GameState) eventBadFood(p *Player) string {
	result := "You got sick from something you ate.\n"
	result += g.DamageRandomMember(p, 12)
	return result
}

func (g *GameState) eventAbandonedWagon(p *Player) string {
	result := &strings.Builder{}
	result.WriteString("\n*** LUCKY FIND! ***\n")
	result.WriteString("You discovered an abandoned wagon by the trail!\n")

	// Random loot
	cashFound := 20.0 + g.Rand.Float64()*30
	foodFound := 20.0 + g.Rand.Float64()*40
	bulletsFound := 50.0 + g.Rand.Float64()*100

	g.Cash += cashFound
	g.Food += foodFound
	g.Bullets += bulletsFound

	result.WriteString(fmt.Sprintf("Found: $%.0f cash, %.0f food, %.0f bullets\n", cashFound, foodFound, bulletsFound))
	g.ClampResources()
	return result.String()
}
