package pissingcontest

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
)

func applyCharacteristic(stats *contestResult, self characteristic, opponent *contestResult, ownerMods, opponentMods *contestModifiers) {
	if self.Effects == nil {
		return
	}
	applyEffect(stats, opponent, ownerMods, opponentMods, self.Effects)
}

func applyCondition(stats *contestResult, cond condition, opponent *contestResult, ownerMods, opponentMods *contestModifiers) {
	if cond.Effects == nil {
		return
	}
	if strings.EqualFold(cond.Type, "debuff") && ownerMods != nil && ownerMods.ignoreDebuffs {
		return
	}
	applyEffect(stats, opponent, ownerMods, opponentMods, cond.Effects)
}

func isFailureCondition(conditionType string) bool {
	return strings.EqualFold(conditionType, "failure")
}

func applyLocationEffects(stats *contestResult, loc location, _ *contestModifiers) {
	if loc.Effects == nil {
		return
	}
	applyEffect(stats, nil, nil, nil, loc.Effects)
}

func applyWeatherEffects(stats *contestResult, weather weatherRoll, isWindSailor bool, mods *contestModifiers) {
	if mods != nil && mods.ignoreWeather {
		return
	}
	if weather.Wind.Effects != nil {
		effects := weather.Wind.Effects
		if isWindSailor && len(weather.Wind.Effects) > 0 {
			effects = make(map[string]float64, len(weather.Wind.Effects))
			for key, value := range weather.Wind.Effects {
				effects[key] = value * 2
			}
		}
		applyEffect(stats, nil, nil, nil, effects)

		if weather.Wind.RandomEffectChance > 0 && rollChance(weather.Wind.RandomEffectChance) {
			adjust := 30.0
			if rand.Float64() < 0.5 {
				adjust = -adjust
			}
			applyEffect(stats, nil, nil, nil, map[string]float64{"random_stat": adjust})
		}
	}
	if weather.Temperature.Effects != nil {
		applyEffect(stats, nil, nil, nil, weather.Temperature.Effects)
	}
	if weather.Special != nil && weather.Special.Effects != nil {
		applyEffect(stats, nil, nil, nil, weather.Special.Effects)
	}
}

func applyEffect(stats *contestResult, opponent *contestResult, actorMods, opponentMods *contestModifiers, effects map[string]float64) {
	chosen := []string{"distance", "volume", "aim", "duration"}

	for rawKey, value := range effects {
		target := stats
		targetMods := actorMods
		key := rawKey
		if strings.HasPrefix(key, "opponent_") {
			target = opponent
			key = strings.TrimPrefix(key, "opponent_")
			targetMods = opponentMods
			if target == nil {
				continue
			}
		}

		if applyModifier(targetMods, key, value) {
			continue
		}

		switch key {
		case "all":
			target.distance *= (1 + value/100)
			target.volume *= (1 + value/100)
			target.aim *= (1 + value/100)
			target.duration *= (1 + value/100)
		case "distance":
			target.distance *= (1 + value/100)
		case "distance_variance":
			target.distance *= 1 + ((rand.Float64()*2 - 1) * value / 100)
		case "volume":
			target.volume *= (1 + value/100)
		case "aim":
			target.aim *= (1 + value/100)
		case "duration":
			target.duration *= (1 + value/100)
		case "distance_tailwind":
			if rand.Float64() < 0.5 {
				target.distance *= (1 + value/100)
			}
		case "distance_headwind":
			if rand.Float64() < 0.5 {
				target.distance *= (1 + value/100)
			}
		case "random_stat":
			if value != 0 {
				i := rand.Intn(len(chosen))
				switch chosen[i] {
				case "distance":
					target.distance *= (1 + value/100)
				case "volume":
					target.volume *= (1 + value/100)
				case "aim":
					target.aim *= (1 + value/100)
				case "duration":
					target.duration *= (1 + value/100)
				}
			}
		case "distance_min":
			if target.distance < value {
				target.distance = value
			}
		case "distance_max":
			if target.distance > value {
				target.distance = value
			}
		case "volume_min":
			if target.volume < value {
				target.volume = value
			}
		case "volume_max":
			if target.volume > value {
				target.volume = value
			}
		case "duration_min":
			if target.duration < value {
				target.duration = value
			}
		case "duration_max":
			if target.duration > value {
				target.duration = value
			}
		}
	}

	targetBounds(stats)
	if opponent != nil {
		targetBounds(opponent)
	}
}

func applyModifier(mods *contestModifiers, key string, value float64) bool {
	if mods == nil {
		return false
	}
	if value == 0 {
		return false
	}

	switch key {
	case "all_failure_chance":
		mods.allFailureChance += toChance(value)
		return true
	case "forfeit_chance":
		mods.forfeitChance += toChance(value)
		return true
	case "opponent_forfeit_chance":
		mods.forfeitChance += toChance(value)
		return true
	case "opponent_fail_chance":
		mods.forfeitChance += toChance(value)
		return true
	case "fail_chance":
		mods.forfeitChance += toChance(value)
		return true
	case "malfunction_chance":
		mods.malfunctionChance += toChance(value)
		return true
	case "wrong_direction_chance":
		mods.wrongDirectionChance += toChance(value)
		return true
	case "copy_best_stat":
		mods.copyBestStat = true
		return true
	case "ignore_debuffs":
		mods.ignoreDebuffs = true
		return true
	case "ignore_weather":
		mods.ignoreWeather = true
		return true
	case "cold_turtle":
		mods.coldTurtle = true
		return true
	case "random_failures":
		mods.randomFailures = true
		return true
	case "multiple_failures":
		mods.multipleFailures = true
		return true
	case "perfect_or_fail":
		mods.perfectOrFail = true
		return true
	case "start_strong_fail":
		mods.startStrongFail = true
		return true
	case "mutual_malfunction":
		mods.mutualMalfunction = true
		return true
	case "weather_change_chance":
		mods.weatherChangeChance = value
		return true
	case "bad_rng":
		mods.badRNG = true
		return true
	case "straight_up":
		mods.straightUp = true
		return true
	case "shaking":
		mods.shaking = true
		return true
	case "wrong_way":
		mods.wrongWay = true
		return true
	case "immune_debuffs":
		mods.ignoreDebuffs = true
		return true
	case "quit_if_losing":
		mods.quitIfLosing = true
		return true
	case "stage_fright_chance":
		mods.allFailureChance += toChance(value)
		return true
	case "opponent_stage_fright_chance":
		mods.allFailureChance += toChance(value)
		return true
	}
	return false
}

func toChance(value float64) float64 {
	if value > 1 {
		return value / 100
	}
	return value
}

func targetBounds(stats *contestResult) {
	if stats.distance < 0 {
		stats.distance = 0
	}
	if stats.volume < 0 {
		stats.volume = 0
	}
	if stats.aim < 0 {
		stats.aim = 0
	}
	if stats.duration < 0 {
		stats.duration = 0
	}
	stats.aim = math.Min(stats.aim, 1000)
	if stats.duration > 0 {
		stats.duration = math.Min(stats.duration, 120)
	}
}

func highlightContestStat(stats contestResult) string {
	if stats.aim >= 90 {
		return "pinpoint aim"
	}
	if stats.distance >= 6 {
		return "long-range arseplunger"
	}
	if stats.volume >= 1500 {
		return "high-pressure cannon"
	}
	if stats.duration >= 90 {
		return "marathon mode"
	}

	switch {
	case stats.wrongDirection:
		return "wrong-way special"
	case stats.straightUp:
		return "the fountain of shame"
	case stats.duration <= 4:
		return "instant blast"
	case stats.volume <= 300:
		return "stingy stream"
	default:
		return "steady flow"
	}
}

func formatStats(stats contestResult, score int, user string) string {
	return fmt.Sprintf(
		"📏%.1fm 💧%dml 🎯%.0f%% ⏱️%.0fs [%d] -%s // %s",
		stats.distance,
		int(math.Round(stats.volume)),
		stats.aim,
		stats.duration,
		score,
		user,
		highlightContestStat(stats),
	)
}
