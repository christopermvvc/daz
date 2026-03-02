package pissingcontest

import (
	"math/rand"
	"strings"
)

var conditionPools = map[string][]condition{
	"failure": {
		{
			Name:        "Stage Fright",
			Type:        "failure",
			Description: "can't perform under pressure",
			Message:     "couldn't even get it out",
			Weight:      2,
		},
		{
			Name:        "Shy Bladder",
			Type:        "failure",
			Description: "someone's watching, can't start",
			Message:     "got shy bladder and froze up",
			Weight:      2,
		},
		{
			Name:        "Equipment Malfunction",
			Type:        "failure",
			Description: "something's wrong down there",
			Message:     "had equipment problems",
			Weight:      1,
		},
		{
			Name:        "The Clench",
			Type:        "failure",
			Description: "gotta shit too bad, can't piss",
			Message:     "clenched too hard to piss",
			Weight:      1,
		},
		{
			Name:        "Whiskey Dick",
			Type:        "failure",
			Description: "too drunk to function",
			Message:     "was too pissed to piss",
			Weight:      1,
		},
		{
			Name:        "Froze Solid",
			Type:        "failure",
			Description: "too cold, turtle mode",
			Message:     "'s ween went full turtle mode - everything retracted",
			Weight:      1,
		},
		{
			Name:        "Pulled a Muscle",
			Type:        "failure",
			Description: "tried too hard, injured",
			Message:     "hurt himself trying",
			Weight:      1,
		},
		{
			Name:        "Cop Showed Up",
			Type:        "failure",
			Description: "both players run away",
			Message:     "got spooked by the cops",
			Mutual:      true,
			Weight:      1,
		},
	},
	"debuff": {
		{
			Name:        "Split Stream",
			Type:        "debuff",
			Description: "stream splits in two",
			Message:     "got split stream",
			Effects:     map[string]float64{"distance": -50},
			Weight:      3,
		},
		{
			Name:        "Weak Flow",
			Type:        "debuff",
			Description: "pathetic pressure",
			Message:     "suffering from weak flow",
			Effects:     map[string]float64{"all": -20},
			Weight:      2,
		},
		{
			Name:        "The Shakes",
			Type:        "debuff",
			Description: "uncontrollable shaking",
			Message:     "got the shakes",
			Effects:     map[string]float64{"random_stat": -40},
			Weight:      2,
		},
		{
			Name:        "Can't Find It",
			Type:        "debuff",
			Description: "fumbling around",
			Message:     "couldn't find it quickly",
			Effects:     map[string]float64{"duration": -15},
			Weight:      1,
		},
		{
			Name:        "Zipper Stuck",
			Type:        "debuff",
			Description: "late start",
			Message:     "zipper got stuck",
			Effects:     map[string]float64{"volume": -25},
			Weight:      1,
		},
		{
			Name:        "The Dribbles",
			Type:        "debuff",
			Description: "pathetic dribble",
			Message:     "just dribbling",
			Effects:     map[string]float64{"distance_max": 1},
			Weight:      1,
		},
		{
			Name:        "Twisted Nut",
			Type:        "debuff",
			Description: "extreme pain",
			Message:     "twisted a nut in agony",
			Effects:     map[string]float64{"all": -50},
			Weight:      1,
		},
		{
			Name:        "Sack Stuck",
			Type:        "debuff",
			Description: "zipper accident",
			Message:     "sack stuck in zipper",
			Effects:     map[string]float64{"aim": -40, "volume": -30},
			Weight:      1,
		},
	},
	"buff": {
		{
			Name:        "Full Tank",
			Type:        "buff",
			Description: "bladder's about to burst",
			Message:     "got a full tank ready to go",
			Effects:     map[string]float64{"distance": 30},
			Weight:      2,
		},
		{
			Name:        "Morning Glory",
			Type:        "buff",
			Description: "morning advantage",
			Message:     "blessed with morning glory",
			Effects:     map[string]float64{"all": 15},
			Weight:      2,
		},
		{
			Name:        "The Zone",
			Type:        "buff",
			Description: "in the zone",
			Message:     "entered the zone",
			Effects:     map[string]float64{"immune_debuffs": 1},
			Weight:      1,
		},
		{
			Name:        "Hydro Power",
			Type:        "buff",
			Description: "well hydrated",
			Message:     "powered by hydration",
			Effects:     map[string]float64{"volume": 40},
			Weight:      2,
		},
		{
			Name:        "Laser Focus",
			Type:        "buff",
			Description: "total concentration",
			Message:     "achieved laser focus",
			Effects:     map[string]float64{"duration": 25},
			Weight:      2,
		},
	},
	"weird": {
		{
			Name:        "Backwards Flow",
			Type:        "weird",
			Description: "pissed on self",
			Message:     "somehow pissed backwards on himself",
			Effects:     map[string]float64{"distance": -999},
			Weight:      1,
		},
		{
			Name:        "The Fountain",
			Type:        "weird",
			Description: "straight up fountain",
			Message:     "created a fountain straight up",
			Effects:     map[string]float64{"distance": 0, "straight_up": 1},
			Weight:      1,
		},
		{
			Name:        "Ghost Piss",
			Type:        "weird",
			Description: "no volume recorded",
			Message:     "ghost piss - where'd it go?",
			Effects:     map[string]float64{"volume": 0},
			Weight:      1,
		},
		{
			Name:        "Pissin' Blood",
			Type:        "weird",
			Description: "medical emergency - blood in piss",
			Message:     "pissin' blood! Medical emergency!",
			Fine:        50,
			FineMessage: "Ambulance called! $50 medical bill",
			Effects:     map[string]float64{"all": -20, "duration": 999},
			Weight:      1,
		},
		{
			Name:        "Piss Shivers",
			Type:        "weird",
			Description: "uncontrollable shaking",
			Message:     "got the piss shivers",
			Effects:     map[string]float64{"aim": -50, "shaking": 1},
			Weight:      1,
		},
		{
			Name:        "The Misfire",
			Type:        "weird",
			Description: "aimed wrong way",
			Message:     "misfired completely wrong direction",
			Effects:     map[string]float64{"wrong_way": 1},
			Weight:      1,
		},
		{
			Name:        "Power Washer",
			Type:        "weird",
			Description: "extreme pressure burst",
			Message:     "went full power washer mode",
			Effects:     map[string]float64{"distance": 50, "volume": 50, "duration": -50},
			Weight:      1,
		},
		{
			Name:        "The Trickle",
			Type:        "weird",
			Description: "eternal trickle",
			Message:     "trickling forever",
			Effects:     map[string]float64{"duration": 80, "distance": -80, "volume": -80},
			Weight:      1,
		},
		{
			Name:        "Fire Hose",
			Type:        "weird",
			Description: "uncontrolled power",
			Message:     "fire hose mode activated",
			Effects:     map[string]float64{"all": 30, "aim": -60},
			Weight:      1,
		},
	},
}

func randomCondition() (condition, bool) {
	if rand.Float64() > 0.10 {
		return condition{}, false
	}

	weights := []struct {
		ty string
		w  int
	}{
		{ty: "failure", w: 1},
		{ty: "debuff", w: 6},
		{ty: "buff", w: 2},
		{ty: "weird", w: 1},
	}
	total := 0
	for _, item := range weights {
		total += item.w
	}
	pick := rand.Intn(total)

	idx := 0
	for _, item := range weights {
		idx += item.w
		if pick < idx {
			choices := conditionPools[item.ty]
			return randomFromPool(choices), true
		}
	}

	return randomFromPool(conditionPools["debuff"]), true
}

func randomFromPool(items []condition) condition {
	return items[rand.Intn(len(items))]
}

func getConditionByName(name string) (condition, bool) {
	if name == "" {
		return condition{}, false
	}
	for _, items := range conditionPools {
		for _, cond := range items {
			if strings.EqualFold(cond.Name, name) {
				return cond, true
			}
		}
	}
	return condition{}, false
}
