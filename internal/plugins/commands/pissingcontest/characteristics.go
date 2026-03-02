package pissingcontest

import "math/rand"

var characteristicPools = map[string][]characteristic{
	"intimidation": {
		{Name: "The Horse Cock", Category: "intimidation", Description: "opponent gets nervous", Rarity: "uncommon", Effects: map[string]float64{"opponent_aim": -15, "opponent_duration": -10}},
		{Name: "Big Dick Energy", Category: "intimidation", Description: "intimidating presence", Rarity: "uncommon", Effects: map[string]float64{"opponent_stage_fright_chance": 25, "opponent_aim": -20}},
		{Name: "The Anaconda", Category: "intimidation", Description: "opponent keeps looking", Rarity: "uncommon", Effects: map[string]float64{"opponent_aim": -30, "opponent_duration": -20}},
		{Name: "Meat Hammer", Category: "intimidation", Description: "opponent unfocused", Rarity: "uncommon", Effects: map[string]float64{"opponent_distance": -25, "opponent_aim": -15}},
		{Name: "The Intimidator", Category: "intimidation", Description: "opponent stutters", Rarity: "uncommon", Effects: map[string]float64{"opponent_duration": -20, "opponent_volume": -30}},
		{Name: "Donkey Dick", Category: "intimidation", Description: "opponent quick finish", Rarity: "uncommon", Effects: map[string]float64{"opponent_duration": -50, "opponent_distance": 10}},
		{Name: "The Monster", Category: "intimidation", Description: "might scare opponent away", Rarity: "rare", Effects: map[string]float64{"opponent_forfeit_chance": 30, "opponent_all": -25}},
		{Name: "Porn Star Pete", Category: "intimidation", Description: "opponent distracted", Rarity: "uncommon", Effects: map[string]float64{"opponent_aim": -35, "opponent_distance": -15}},
		{Name: "Alpha Hog", Category: "intimidation", Description: "opponent loses confidence", Rarity: "uncommon", Effects: map[string]float64{"opponent_all": -20}},
	},
	"mockery": {
		{Name: "Pencil Dick", Category: "mockery", Description: "opponent relaxes", Rarity: "common", Effects: map[string]float64{"opponent_all": 20}},
		{Name: "Baby Carrot", Category: "mockery", Description: "opponent laughing", Rarity: "common", Effects: map[string]float64{"opponent_duration": 30, "opponent_aim": -20}},
		{Name: "The Acorn", Category: "mockery", Description: "opponent pities", Rarity: "common", Effects: map[string]float64{"opponent_aim": 60}},
		{Name: "Micro Mike", Category: "mockery", Description: "opponent showing off", Rarity: "common", Effects: map[string]float64{"opponent_distance": 40, "opponent_aim": -10}},
		{Name: "Button Mushroom", Category: "mockery", Description: "boosts opponent confidence", Rarity: "common", Effects: map[string]float64{"opponent_all": 15}},
		{Name: "The Innie", Category: "mockery", Description: "opponent staring", Rarity: "common", Effects: map[string]float64{"opponent_aim": -10, "opponent_duration": 25}},
		{Name: "Tic Tac", Category: "mockery", Description: "opponent confidence soars", Rarity: "common", Effects: map[string]float64{"opponent_all": 30}},
		{Name: "Pinky Finger", Category: "mockery", Description: "easy target practice", Rarity: "common", Effects: map[string]float64{"opponent_aim": 50, "opponent_volume": 20}},
		{Name: "The Nugget", Category: "mockery", Description: "opponent cant miss", Rarity: "common", Effects: map[string]float64{"opponent_aim": 60, "opponent_distance": 30}},
		{Name: "Clit Dick", Category: "mockery", Description: "precision advantage", Rarity: "common", Effects: map[string]float64{"opponent_aim": 40}},
	},
	"performance": {
		{Name: "Fire Hose Frank", Category: "performance", Description: "extreme distance", Rarity: "legendary", ForceComment: true, Effects: map[string]float64{"distance": 60, "duration": -40, "aim": -30}},
		{Name: "The Camel", Category: "performance", Description: "massive bladder", Rarity: "uncommon", Effects: map[string]float64{"volume": 50, "distance": -20}},
		{Name: "Marathon Man", Category: "performance", Description: "goes forever", Rarity: "uncommon", Effects: map[string]float64{"duration_min": 20, "distance": -30, "aim": 20}},
		{Name: "Quick Draw McGraw", Category: "performance", Description: "lightning fast", Rarity: "uncommon", Effects: map[string]float64{"duration_max": 3, "aim": -40, "distance": 30}},
		{Name: "Old Faithful", Category: "performance", Description: "reliable performer", Rarity: "uncommon", Effects: map[string]float64{"all": -25, "aim": 40}},
		{Name: "The Sprinkler", Category: "performance", Description: "unpredictable stream", Rarity: "uncommon", Effects: map[string]float64{"aim": -50}},
		{Name: "Pressure Washer", Category: "performance", Description: "high pressure", Rarity: "legendary", ForceComment: true, Effects: map[string]float64{"distance_min": 3, "aim": -40, "duration": -20}},
		{Name: "The Dripper", Category: "performance", Description: "slow and steady", Rarity: "uncommon", Effects: map[string]float64{"duration": 80, "distance": -60, "aim": 50}},
		{Name: "Beer Bladder", Category: "performance", Description: "fullness advantage", Rarity: "uncommon", Effects: map[string]float64{"volume": 40, "aim": -30, "duration": 20}},
		{Name: "The Firehose", Category: "performance", Description: "maximum pressure", Rarity: "legendary", ForceComment: true, Effects: map[string]float64{"distance": 70, "aim": -60, "duration": -30}},
	},
	"failure": {
		{Name: "Stage Fright Steve", Category: "failure", Description: "performance anxiety", Rarity: "uncommon", Effects: map[string]float64{"forfeit_chance": 25, "all": -40}},
		{Name: "Nervous Nelly", Category: "failure", Description: "easily intimidated", Rarity: "uncommon", Effects: map[string]float64{"aim": -30}},
		{Name: "The Quitter", Category: "failure", Description: "gives up easily", Rarity: "uncommon", Effects: map[string]float64{"quit_if_losing": 1}},
		{Name: "Broken Bobby", Category: "failure", Description: "equipment issues", Rarity: "uncommon", Effects: map[string]float64{"malfunction_chance": 15, "aim": -50}},
		{Name: "Shy Guy", Category: "failure", Description: "stage fright prone", Rarity: "uncommon", Effects: map[string]float64{"volume": -60, "aim": -40}},
	},
	"distracted": {
		{Name: "Curious George", Category: "distracted", Description: "keeps peeking", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": -40, "duration": -20}},
		{Name: "The Admirer", Category: "distracted", Description: "caught staring", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"all": -30, "opponent_aim": 20}},
		{Name: "Closet Case", Category: "distracted", Description: "sneaking looks", Rarity: "uncommon", Effects: map[string]float64{"aim": -50}},
		{Name: "The Comparer", Category: "distracted", Description: "busy measuring", Rarity: "uncommon", Effects: map[string]float64{"aim": -30, "volume": -40}},
		{Name: "Sword Fighter", Category: "distracted", Description: "wants to cross streams", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": -60}},
	},
	"special": {
		{Name: "Wind Sailor", Category: "special", Description: "affected by wind", Rarity: "uncommon", Effects: map[string]float64{"aim": -20}, SelfCondition: "wind_sailor"},
		{Name: "Weather Immune", Category: "special", Description: "unaffected by conditions", Rarity: "rare", Effects: map[string]float64{"ignore_weather": 1}},
		{Name: "Cold Blooded", Category: "special", Description: "shrinks in cold", Rarity: "uncommon", Effects: map[string]float64{"cold_turtle": 1, "volume": -40, "aim": 30}},
		{Name: "The Legend", Category: "special", Description: "legendary status", Rarity: "legendary", ForceComment: true, Effects: map[string]float64{"all": 25}},
		{Name: "Piss God", Category: "special", Description: "godlike abilities", Rarity: "legendary", Effects: map[string]float64{"all": 40}},
		{Name: "Pierced Python", Category: "special", Description: "jewelry causes havoc", Rarity: "legendary", ForceComment: true, Effects: map[string]float64{"distance": 55, "opponent_aim": -35, "opponent_all": 0}},
	},
	"counter": {
		{Name: "The Equalizer", Category: "counter", Description: "size doesn't matter", Rarity: "rare", Effects: map[string]float64{"aim": 20}},
		{Name: "Confidence King", Category: "counter", Description: "unshakeable", Rarity: "rare", Effects: map[string]float64{"aim": 30}},
		{Name: "Steady Hands", Category: "counter", Description: "sick control", Rarity: "uncommon", Effects: map[string]float64{"aim": 60, "distance": -20}},
		{Name: "The Mirror", Category: "counter", Description: "copies opponent strength", Rarity: "rare", Effects: map[string]float64{"copy_best_stat": 1}},
		{Name: "Laser Guided", Category: "counter", Description: "precision focused", Rarity: "rare", Effects: map[string]float64{"aim": 80, "volume": -30}},
	},
	"aim": {
		{Name: "Crooked Dick Craig", Category: "aim", Description: "aims left always", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": -60, "distance": 20}},
		{Name: "One-Eyed Willie", Category: "aim", Description: "no depth perception", Rarity: "uncommon", Effects: map[string]float64{"aim": -40, "volume": 30}},
		{Name: "The Stormtrooper", Category: "aim", Description: "can't hit anything", Rarity: "uncommon", Effects: map[string]float64{"aim": -80, "distance": 40}},
		{Name: "Snipers Knob", Category: "aim", Description: "tactical precision", Rarity: "rare", Effects: map[string]float64{"aim": 70, "volume": -30}},
		{Name: "Bentley", Category: "aim", Description: "bent shaft", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": -50}},
		{Name: "GPS Guided", Category: "aim", Description: "can hit a fly at 10 paces", Rarity: "rare", Effects: map[string]float64{"aim": 90, "duration": -40}},
		{Name: "The Cyclops", Category: "aim", Description: "one ball throws balance", Rarity: "uncommon", Effects: map[string]float64{"aim": -30, "distance": 20}},
		{Name: "Shaky Pete", Category: "aim", Description: "constant shivers", Rarity: "uncommon", Effects: map[string]float64{"aim": -45, "duration": 15}},
		{Name: "Cross-Eyed Carl", Category: "aim", Description: "dangerous aim", Rarity: "uncommon", Effects: map[string]float64{"aim": -70, "distance": 20}},
		{Name: "The Marksman", Category: "aim", Description: "steady aim", Rarity: "uncommon", Effects: map[string]float64{"aim": 50, "volume": -20}},
		{Name: "Spray and Pray", Category: "aim", Description: "chaotic stream", Rarity: "rare", Effects: map[string]float64{"aim": -90, "distance": 60, "volume": 40}},
		{Name: "Lefty Loosey", Category: "aim", Description: "favors left side", Rarity: "uncommon", Effects: map[string]float64{"aim": -35, "duration": 25}},
		{Name: "The Pendulum", Category: "aim", Description: "swings side to side", Rarity: "uncommon", Effects: map[string]float64{"aim": -40, "distance": 30}},
		{Name: "Laser Dick", Category: "aim", Description: "dead-on aim but pisses like a girl", Rarity: "legendary", Effects: map[string]float64{"aim": 100, "all": -50}},
		{Name: "Parkinsons Paul", Category: "aim", Description: "uncontrollable shaking", Rarity: "uncommon", Effects: map[string]float64{"aim": -55, "duration": 40}},
	},
	"balls": {
		{Name: "One Nut Wonder", Category: "balls", Description: "lopsided stance", Rarity: "uncommon", Effects: map[string]float64{"aim": -25, "distance": 30}},
		{Name: "Big Balls Barry", Category: "balls", Description: "balls interfere", Rarity: "uncommon", Effects: map[string]float64{"aim": -35, "volume": 40}},
		{Name: "High and Tight", Category: "balls", Description: "tucked for precision", Rarity: "uncommon", Effects: map[string]float64{"aim": 40, "volume": -20}},
		{Name: "Low Hangers", Category: "balls", Description: "saggy interference", Rarity: "uncommon", Effects: map[string]float64{"aim": -45, "duration": 50}},
		{Name: "The Tangled Sack", Category: "balls", Description: "twisted balls", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": -60}},
		{Name: "Smooth as Eggs", Category: "balls", Description: "freshly shaved", Rarity: "uncommon", Effects: map[string]float64{"aim": 30}},
		{Name: "The Beanbag", Category: "balls", Description: "saggy sack", Rarity: "uncommon", Effects: map[string]float64{"aim": -40, "duration": 60}},
		{Name: "Blue Baller", Category: "balls", Description: "backed up", Rarity: "uncommon", Effects: map[string]float64{"aim": -50, "volume": 70}},
		{Name: "The Juggler", Category: "balls", Description: "constant adjusting", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": -70}},
		{Name: "Nuts of Steel", Category: "balls", Description: "unbreakable", Rarity: "rare", Effects: map[string]float64{"aim": 25}},
	},
	"volume": {
		{Name: "The Gusher", Category: "volume", Description: "extreme volume", Rarity: "rare", Effects: map[string]float64{"volume": 80, "aim": -40}},
		{Name: "Dehydrated Dave", Category: "volume", Description: "concentrated stream", Rarity: "uncommon", Effects: map[string]float64{"volume": -70, "distance": 50}},
		{Name: "Bladder Buster", Category: "volume", Description: "massive capacity", Rarity: "rare", Effects: map[string]float64{"volume_min": 1500, "duration": -30}},
		{Name: "The Trickler", Category: "volume", Description: "weak flow", Rarity: "uncommon", Effects: map[string]float64{"volume": -80, "duration": 60, "aim": 40}},
		{Name: "Kidney Flusher", Category: "volume", Description: "inspires urgency", Rarity: "uncommon", Effects: map[string]float64{"volume": 60}},
		{Name: "The Reservoir", Category: "volume", Description: "maximum capacity", Rarity: "rare", Effects: map[string]float64{"volume": 100, "distance": -50}},
		{Name: "Dust Bowl", Category: "volume", Description: "practically empty", Rarity: "uncommon", Effects: map[string]float64{"volume": -90}},
		{Name: "Heavy Flow", Category: "volume", Description: "impressive volume", Rarity: "rare", Effects: map[string]float64{"volume": 70, "aim": -40}},
	},
	"substance": {
		{Name: "Coke Dick Colin", Category: "substance", Description: "equipment failure likely", Rarity: "uncommon", Effects: map[string]float64{"malfunction_chance": 80, "volume": -90}},
		{Name: "Beer Goggles Bob", Category: "substance", Description: "thinks he's winning", Rarity: "uncommon", Effects: map[string]float64{"aim": -60}},
		{Name: "Stoned Stupid", Category: "substance", Description: "might forget to start", Rarity: "uncommon", Effects: map[string]float64{"all": -50}},
		{Name: "Speed Freak", Category: "substance", Description: "hyperfast stream", Rarity: "uncommon", Effects: map[string]float64{"duration_max": 2, "distance": 60, "aim": -70}},
		{Name: "Whiskey Dick Wayne", Category: "substance", Description: "whiskey problems", Rarity: "uncommon", Effects: map[string]float64{"all": -40}},
		{Name: "Meth Head Mark", Category: "substance", Description: "unpredictable effects", Rarity: "uncommon", Effects: map[string]float64{"all": 0}},
		{Name: "Shroom Tripper", Category: "substance", Description: "seeing colors", Rarity: "uncommon", Effects: map[string]float64{"aim": -90}},
		{Name: "Blackout Barry", Category: "substance", Description: "might piss anywhere", Rarity: "uncommon", Effects: map[string]float64{"wrong_direction_chance": 40}},
	},
	"technique": {
		{Name: "The Leaner", Category: "technique", Description: "45 degree angle", Rarity: "rare", ForceComment: true, Effects: map[string]float64{"distance": 40, "volume": -30}},
		{Name: "Hands Free Harry", Category: "technique", Description: "no hands technique", Rarity: "uncommon", Effects: map[string]float64{"aim": -80, "duration": 30}},
		{Name: "Two Hander", Category: "technique", Description: "needs both hands", Rarity: "uncommon", Effects: map[string]float64{"aim": 60, "distance": -20}},
		{Name: "The Helicopter", Category: "technique", Description: "spinning technique", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": -90, "distance": 40}},
		{Name: "Sitting Sam", Category: "technique", Description: "sits to pee", Rarity: "uncommon", ForceComment: true, Effects: map[string]float64{"aim": 80, "distance": -60}},
		{Name: "The Squatter", Category: "technique", Description: "weird stance", Rarity: "uncommon", Effects: map[string]float64{"volume": 50, "distance": -70}},
		{Name: "Pinch and Roll", Category: "technique", Description: "foreskin technique", Rarity: "uncommon", Effects: map[string]float64{"aim": 70, "duration": -40}},
		{Name: "The Shaker", Category: "technique", Description: "excessive shaking", Rarity: "uncommon", Effects: map[string]float64{"duration": 30, "aim": -20}},
	},
	"disruptor": {
		{Name: "The Psycher", Category: "disruptor", Description: "fake sounds", Rarity: "uncommon", Effects: map[string]float64{"opponent_all": -30}},
		{Name: "Trash Talker", Category: "disruptor", Description: "verbal assault", Rarity: "uncommon", Effects: map[string]float64{"opponent_all": -20}},
		{Name: "The Moaner", Category: "disruptor", Description: "sex noises", Rarity: "uncommon", Effects: map[string]float64{"opponent_aim": -50}},
		{Name: "Stage Whisperer", Category: "disruptor", Description: "mutters constantly", Rarity: "uncommon", Effects: map[string]float64{"opponent_all": -40}},
		{Name: "The Giggler", Category: "disruptor", Description: "contagious laughter", Rarity: "uncommon", Effects: map[string]float64{"opponent_aim": -30}},
		{Name: "Commentary Craig", Category: "disruptor", Description: "live commentary", Rarity: "uncommon", Effects: map[string]float64{"opponent_all": -25}},
		{Name: "The Jinxer", Category: "disruptor", Description: "calls moves", Rarity: "uncommon", Effects: map[string]float64{"opponent_fail_chance": 20}},
	},
	"environmental": {
		{Name: "Wind Whisperer", Category: "environmental", Description: "controls wind", Rarity: "rare", Effects: map[string]float64{"all": 20}},
		{Name: "The Weatherman", Category: "environmental", Description: "weather expert", Rarity: "rare", Effects: map[string]float64{"all": 20}},
		{Name: "Storm Caller", Category: "environmental", Description: "changes weather", Rarity: "rare", Effects: map[string]float64{"weather_change_chance": 30}},
		{Name: "Temperature Tommy", Category: "environmental", Description: "chills the air", Rarity: "rare", Effects: map[string]float64{"all": 0}},
		{Name: "The Groundskeeper", Category: "environmental", Description: "knows terrain", Rarity: "rare", Effects: map[string]float64{"aim": 30, "distance": 20}},
	},
	"legendary_failure": {
		{Name: "The Disaster", Category: "legendary_failure", Description: "everything fails", Rarity: "legendary", Effects: map[string]float64{"all_failure_chance": 50}},
		{Name: "Murphys Law", Category: "legendary_failure", Description: "what can go wrong will", Rarity: "legendary", Effects: map[string]float64{"random_failures": 1}},
		{Name: "The Catastrophe", Category: "legendary_failure", Description: "prone to all failures", Rarity: "legendary", Effects: map[string]float64{"all_failure_chance": 25}},
		{Name: "Bad Luck Brian", Category: "legendary_failure", Description: "cursed luck", Rarity: "legendary", Effects: map[string]float64{"bad_rng": 1}},
		{Name: "The Cursed", Category: "legendary_failure", Description: "multiple issues", Rarity: "legendary", Effects: map[string]float64{"multiple_failures": 1}},
		{Name: "Glass Cannon", Category: "legendary_failure", Description: "all or nothing", Rarity: "legendary", Effects: map[string]float64{"perfect_or_fail": 1}},
		{Name: "The Imploder", Category: "legendary_failure", Description: "dramatic failure", Rarity: "legendary", Effects: map[string]float64{"start_strong_fail": 1}},
		{Name: "The Jinx", Category: "legendary_failure", Description: "curses both players", Rarity: "legendary", Effects: map[string]float64{"mutual_malfunction": 1}},
	},
	"self_condition": {
		{Name: "Iron Bladder", Category: "self_condition", Description: "immune to all conditions", Rarity: "legendary", SelfCondition: "The Zone"},
		{Name: "Zen Master", Category: "self_condition", Description: "can't be intimidated", Rarity: "legendary", SelfCondition: "Full Tank"},
		{Name: "The Professional", Category: "self_condition", Description: "ignores distractions", Rarity: "legendary", SelfCondition: "Laser Focus"},
		{Name: "Battle Hardened", Category: "self_condition", Description: "seen it all before", Rarity: "legendary", SelfCondition: "No Warmup"},
		{Name: "The Veteran", Category: "self_condition", Description: "experienced pisser", Rarity: "legendary", SelfCondition: "No Warmup"},
	},
	"mutual_condition": {
		{Name: "The Awkward", Category: "mutual_condition", Description: "makes everyone nervous", Rarity: "rare", MutualCondition: "Shy Bladder"},
		{Name: "Drama Queen", Category: "mutual_condition", Description: "too theatrical", Rarity: "rare", MutualCondition: "Stage Fright"},
		{Name: "The Infectious", Category: "mutual_condition", Description: "contagious laughter", Rarity: "rare", MutualCondition: "The Giggles"},
		{Name: "Chaos Bringer", Category: "mutual_condition", Description: "brings chaos", Rarity: "legendary", MutualCondition: "random"},
		{Name: "The Jinx", Category: "mutual_condition", Description: "jinxes everyone", Rarity: "legendary", MutualCondition: "Equipment Malfunction"},
	},
}

func randomCharacteristic() characteristic {
	return randomWeightedCharacteristic()
}

func randomWeightedCharacteristic() characteristic {
	totalWeight := 0
	for _, set := range characteristicPools {
		for _, c := range set {
			w := c.Weight
			if w <= 0 {
				w = 1
			}
			totalWeight += w
		}
	}
	if totalWeight <= 0 {
		for _, set := range characteristicPools {
			if len(set) > 0 {
				return set[0]
			}
		}
		return characteristic{}
	}

	roll := rand.Intn(totalWeight)
	for _, set := range characteristicPools {
		for _, c := range set {
			w := c.Weight
			if w <= 0 {
				w = 1
			}
			if roll < w {
				return c
			}
			roll -= w
		}
	}

	for _, set := range characteristicPools {
		if len(set) > 0 {
			return set[0]
		}
	}
	return characteristic{}
}
