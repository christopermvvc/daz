package pissingcontest

import "math/rand"

var locationPools = map[string][]location{
	"morning": {
		{
			Name:        "Servo Sideyard",
			Description: "headwind, diesel fumes, and a perfect audience",
			Effects:     map[string]float64{"distance": 20, "aim": -15},
			Fine:        5,
			FineMessage: "Raj caught ya! $5 fine",
			FineChance:  1,
		},
		{
			Name:        "School Pickup Zone",
			Description: "buses screech, parents nearby, and a panic clock",
			Effects:     map[string]float64{"duration": -60},
			Fine:        10,
			FineMessage: "School zone fine! $10",
			FineChance:  1,
		},
		{
			Name:        "Beach Shower Block",
			Description: "wet sand underfoot and tiny fish judging your line",
			Effects:     map[string]float64{"aim": -40, "volume": 20},
		},
	},
	"arvo": {
		{
			Name:        "Bottlo Loading Dock",
			Description: "slab fumes, stacked crates, and one very loud echo",
			Effects:     map[string]float64{"volume": 30, "distance": -20},
		},
		{
			Name:        "Cricket Pitch Boundary",
			Description: "stadium lights and people who think they're refs",
			Effects:     map[string]float64{"distance": 40},
		},
		{
			Name:        "Westfield Level 3",
			Description: "tile glare and every eye in your direction",
			Effects:     map[string]float64{"volume": -40, "aim": -30},
		},
		{
			Name:        "Old Mall Utility Block",
			Description: "echo chamber with terrible lighting",
			Effects:     map[string]float64{"aim": -45, "distance": -10},
		},
	},
	"night": {
		{
			Name:        "Kebab Shop Wall",
			Description: "garlic-heavy air and too many onlookers",
			Effects:     map[string]float64{"volume": 40, "aim": -30},
		},
		{
			Name:        "Pub Beer Garden",
			Description: "drunk cheers bouncing off the back wall",
			Effects:     map[string]float64{"volume": 30, "aim": -40},
		},
		{
			Name:        "Drive-In Movies",
			Description: "movie trailers in the distance and sneaky angles",
			Effects:     map[string]float64{"aim": -60, "distance": 40},
		},
		{
			Name:        "Rooftop Plantroom",
			Description: "cold pipes and a view that hurts",
			Effects:     map[string]float64{"distance": 20, "aim": -35},
		},
	},
	"lateNight": {
		{
			Name:        "Servo ATM",
			Description: "security cams and no one to help you hide",
			Effects:     map[string]float64{"all": -40},
			Fine:        5,
			FineMessage: "skimmer fine $5",
			FineChance:  0.5,
		},
		{
			Name:        "Behind the Cop Shop",
			Description: "flashlight checks and a weak bladder panic",
			Effects:     map[string]float64{"distance": 60},
		},
		{
			Name:        "Clocktower Stairwell",
			Description: "stairs are your only privacy and the only audience is concrete",
			Effects:     map[string]float64{"aim": -20, "duration": 20},
		},
	},
	"rural": {
		{
			Name:        "The Bush Dunny",
			Description: "insect noise, deep silence, and confidence tests",
			Effects:     map[string]float64{"duration": -60, "aim": 40},
		},
		{
			Name:        "Mates Farm Dam",
			Description: "slopes and mud, one slip and you'll get humbled",
			Effects:     map[string]float64{"distance": 70, "aim": -30},
		},
		{
			Name:        "Camping Ground",
			Description: "pine scent, rough ground, and bragging rights",
			Effects:     map[string]float64{"all": 25},
		},
		{
			Name:        "Cattle Docks",
			Description: "wet boards and noisy judgement from everything alive",
			Effects:     map[string]float64{"all": 10},
		},
	},
	"special": {
		{
			Name:        "Footy Finals Portaloo",
			Description: "queue chaos and every fan got a mic in their phone",
			Effects:     map[string]float64{"duration": -50, "volume": 40},
			Fine:        20,
			FineMessage: "queue officer fine $20",
			FineChance:  0.3,
		},
		{
			Name:        "Races VIP Area",
			Description: "shiny shoes, expensive suits, and better excuses",
			Effects:     map[string]float64{"volume": -40, "duration": 30},
		},
		{
			Name:        "Council Workers' Restroom",
			Description: "sprinkler alarm, sticky handles, and too much lighting",
			Effects:     map[string]float64{"volume": 20, "aim": -35},
			Fine:        40,
			FineMessage: "city inspector fine $40",
			FineChance:  0.2,
		},
	},
	"aimBonus": {
		{
			Name:        "Train Platform Edge",
			Description: "rails guide your direction if you don't panic",
			Effects:     map[string]float64{"distance": 50, "aim": 30},
		},
		{
			Name:        "Bridge Underpass",
			Description: "hard echoes turn tiny mistakes into comedy",
			Effects:     map[string]float64{"aim": 40, "distance": 20},
		},
		{
			Name:        "Cargo Bay Ramp",
			Description: "slant helps distance if your aim survives",
			Effects:     map[string]float64{"distance": 35, "aim": -10},
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
		events = append(events, contestEvent{Message: "the trough is full and the flow fights for space!"})
	}
	if location.WicketFailChance > 0 && rollChance(location.WicketFailChance) {
		events = append(events, contestEvent{Message: "got side-swiped and took the wicket line - instant disqualification!", Forfeit: true})
	}
	if location.BiteChance > 0 && rollChance(location.BiteChance) {
		events = append(events, contestEvent{Message: "a stray dog went full speed and ruined the rhythm!", Forfeit: true})
	}
	if location.SprinklerChance > 0 && rollChance(location.SprinklerChance) {
		events = append(events, contestEvent{Message: "some idiot hit the sprinkler and turned it into a relay race!"})
	}
	if location.CopChance > 0 && rollChance(location.CopChance) {
		events = append(events, contestEvent{Message: "a bored cop materialized and ruined the vibe!"})
	}
	if location.RunOverChance > 0 && rollChance(location.RunOverChance) {
		events = append(events, contestEvent{Message: "taxi backed up wrong and clipped the angle!", Forfeit: true})
	}
	if location.TinnieHitChance > 0 && rollChance(location.TinnieHitChance) {
		events = append(events, contestEvent{Message: "hit the tinnie and had to laugh while it happened!"})
	}
	if location.AngryChance > 0 && rollChance(location.AngryChance) {
		events = append(events, contestEvent{Message: "the local campers woke up pissed and started filming!"})
	}

	return events
}
