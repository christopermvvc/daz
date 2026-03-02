package pissingcontest

import "math/rand"

var locationPools = map[string][]location{
	"morning": {
		{
			Name:        "Servo Sideyard",
			Description: "headwind and diesel fumes",
			Effects:     map[string]float64{"distance": 20, "aim": -15},
			Fine:        5,
			FineMessage: "Raj caught ya! $5 fine",
			FineChance:  1,
		},
		{
			Name:        "School Pickup Zone",
			Description: "illegal spot, gotta rush",
			Effects:     map[string]float64{"duration": -60},
			Fine:        10,
			FineMessage: "School zone fine! $10",
			FineChance:  1,
		},
		{
			Name:        "Beach Shower Block",
			Description: "sand in the stream",
			Effects:     map[string]float64{"aim": -40, "volume": 20},
		},
	},
	"arvo": {
		{
			Name:        "Bottlo Loading Dock",
			Description: "slab obstacles, beer courage",
			Effects:     map[string]float64{"volume": 30, "distance": -20},
		},
		{
			Name:        "Cricket Pitch Boundary",
			Description: "showing off distance",
			Effects:     map[string]float64{"distance": 40},
		},
		{
			Name:        "Westfield Level 3",
			Description: "shy bladder central",
			Effects:     map[string]float64{"volume": -40, "aim": -30},
		},
	},
	"night": {
		{
			Name:        "Kebab Shop Wall",
			Description: "garlic sauce smell",
			Effects:     map[string]float64{"volume": 40, "aim": -30},
		},
		{
			Name:        "Pub Beer Garden",
			Description: "crowd pressure",
			Effects:     map[string]float64{"volume": 30, "aim": -40},
		},
		{
			Name:        "Drive-In Movies",
			Description: "dark and sneaky angle",
			Effects:     map[string]float64{"aim": -60, "distance": 40},
		},
	},
	"lateNight": {
		{
			Name:        "Servo ATM",
			Description: "camera paranoia",
			Effects:     map[string]float64{"all": -40},
			Fine:        5,
			FineMessage: "skimmer fine $5",
			FineChance:  0.5,
		},
		{
			Name:        "Behind the Cop Shop",
			Description: "adrenaline boost",
			Effects:     map[string]float64{"distance": 60},
		},
	},
	"rural": {
		{
			Name:        "The Bush Dunny",
			Description: "spider fear, drop toilet practice",
			Effects:     map[string]float64{"duration": -60, "aim": 40},
		},
		{
			Name:        "Mates Farm Dam",
			Description: "downhill advantage, mud hazard",
			Effects:     map[string]float64{"distance": 70, "aim": -30},
		},
		{
			Name:        "Camping Ground",
			Description: "nature's calling",
			Effects:     map[string]float64{"all": 25},
		},
	},
	"special": {
		{
			Name:        "Footy Finals Portaloo",
			Description: "queue pressure",
			Effects:     map[string]float64{"duration": -50, "volume": 40},
			Fine:        20,
			FineMessage: "queue officer fine $20",
			FineChance:  0.3,
		},
		{
			Name:        "Races VIP Area",
			Description: "fancy cunts watching",
			Effects:     map[string]float64{"volume": -40, "duration": 30},
		},
	},
	"aimBonus": {
		{
			Name:        "Train Platform Edge",
			Description: "drop distance, railing guide",
			Effects:     map[string]float64{"distance": 50, "aim": 30},
		},
		{
			Name:        "Bridge Underpass",
			Description: "wall guides and echo",
			Effects:     map[string]float64{"aim": 40, "distance": 20},
		},
	},
}

func randomLocationAt(timeHour int) location {
	var pool []location
	switch {
	case timeHour >= 6 && timeHour < 12:
		pool = append(pool, locationPools["morning"]...)
	case timeHour >= 12 && timeHour < 18:
		pool = append(pool, locationPools["arvo"]...)
	case timeHour >= 18 && timeHour < 24:
		pool = append(pool, locationPools["night"]...)
	default:
		pool = append(pool, locationPools["lateNight"]...)
	}
	pool = append(pool, locationPools["rural"]...)
	if rand.Float64() < 0.1 {
		pool = append(pool, locationPools["special"]...)
	}
	if rand.Float64() < 0.15 {
		pool = append(pool, locationPools["aimBonus"]...)
	}

	return randomFromSlice(pool)
}

func checkLocationEvents(location location) []contestEvent {
	events := make([]contestEvent, 0)

	if location.Fine > 0 && location.FineChance > 0 && rollChance(location.FineChance) {
		if location.FineMessage != "" {
			events = append(events, contestEvent{Message: location.FineMessage})
		}
	}

	if location.CloggedChance > 0 && rollChance(location.CloggedChance) {
		events = append(events, contestEvent{Message: "trough's clogged with sawdust!"})
	}
	if location.WicketFailChance > 0 && rollChance(location.WicketFailChance) {
		events = append(events, contestEvent{Message: "hit the wicket! Instant disqualification!", Forfeit: true})
	}
	if location.BiteChance > 0 && rollChance(location.BiteChance) {
		events = append(events, contestEvent{Message: "dog bit ya dick! Contest over!", Forfeit: true})
	}
	if location.SprinklerChance > 0 && rollChance(location.SprinklerChance) {
		events = append(events, contestEvent{Message: "sprinklers turned on!"})
	}
	if location.CopChance > 0 && rollChance(location.CopChance) {
		events = append(events, contestEvent{Message: "cop showed up!"})
	}
	if location.RunOverChance > 0 && rollChance(location.RunOverChance) {
		events = append(events, contestEvent{Message: "cabbie ran ya over!", Forfeit: true})
	}
	if location.TinnieHitChance > 0 && rollChance(location.TinnieHitChance) {
		events = append(events, contestEvent{Message: "pissed in ya own tinnie!"})
	}
	if location.AngryChance > 0 && rollChance(location.AngryChance) {
		events = append(events, contestEvent{Message: "woke up angry campers!"})
	}

	return events
}
