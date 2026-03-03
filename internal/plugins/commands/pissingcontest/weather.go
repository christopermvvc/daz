package pissingcontest

import (
	"math/rand"
	"strings"
	"time"
)

var windEffects = []weather{
	{Name: "Dead Still", Description: "no wind", Effects: map[string]float64{}},
	{Name: "Light Breeze", Description: "a gentle push that dares you to commit", Effects: map[string]float64{"aim": -5, "distance_variance": 10}},
	{Name: "Gusty as Fuck", Description: "random blasts and fake confidence", Effects: map[string]float64{"aim": -20, "distance_variance": 30}},
	{Name: "Cyclone Warning", Description: "blown caps, blown focus, blown everything", Effects: map[string]float64{"distance_tailwind": 50, "distance_headwind": -40, "aim": -60}},
	{Name: "Dust Storm", Description: "nothing visible except panic and grit", Effects: map[string]float64{"aim": -80}, MightHitOpponentChance: 0.3},
	{Name: "Willy Willy", Description: "spinning wind chaos and bad timing", Effects: map[string]float64{}, RandomEffectChance: 0.3},
	{Name: "Ripping Crosswind", Description: "constant side pressure", Effects: map[string]float64{"aim": -25, "distance_variance": 20}},
}

var temperatureEffects = []weather{
	{Name: "Freezing Me Nuts Off", Description: "numb hands and frozen ambition", Effects: map[string]float64{"volume": -40, "duration": 20}, TurtleModeChance: 0.3},
	{Name: "Not Too Shabby", Description: "decent enough", Effects: map[string]float64{}},
	{Name: "Bloody Hot", Description: "heatwave and a little desperation", Effects: map[string]float64{"volume": 30, "duration": -20}},
	{Name: "Satan's Armpit", Description: "humidity turned the room into a sauna", Effects: map[string]float64{"duration": -20}, PassOutChance: 0.1},
	{Name: "Muggy Afternoon", Description: "sweaty wrists, sweaty hands, sweaty nerves", Effects: map[string]float64{"aim": -10, "duration": 10}},
}

var specialWeather = []weather{
	{Name: "Pissing Rain", Description: "perfectly ironic, everything leaking", Effects: map[string]float64{"aim": -40}, Message: "it's pissin' down and so is everyone"},
	{Name: "Hailstorm", Description: "dangerous hail, instant embarrassment", Effects: map[string]float64{"aim": -80}, Message: "holy fuck, hail!", InstantForfeitChance: 0.2},
	{Name: "Lightning Storm", Description: "metal risk and bad timing", Effects: map[string]float64{"aim": -60}, Message: "lightning risk!", MalfunctionChance: 10},
	{Name: "Steam Burst", Description: "hot mist and fogged focus", Effects: map[string]float64{"aim": -30, "distance_variance": 20}, Message: "steam burst in the air"},
}

func randomWeather() weatherRoll {
	return weatherRoll{
		Wind:        randomFromSlice(windEffects),
		Temperature: randomFromSlice(temperatureEffects),
		Special:     randomSpecialWeather(),
	}
}

func randomSpecialWeather() *weather {
	if rand.Float64() >= 0.1 {
		return nil
	}
	selected := randomFromSlice(specialWeather)
	return &selected
}

func formatWeather(w weatherRoll) string {
	parts := []string{w.Wind.Name, w.Temperature.Name}
	if w.Special != nil {
		parts = append(parts, w.Special.Name)
	}
	return strings.Join(parts, ", ")
}

func timeNowHour() int {
	return time.Now().Hour()
}

func checkWeatherEvents(w weatherRoll) []contestEvent {
	events := make([]contestEvent, 0)

	if w.Wind.MightHitOpponentChance > 0 && rollChance(w.Wind.MightHitOpponentChance) {
		events = append(events, contestEvent{Message: "the wind redirected your stream across the line"})
	}
	if w.Temperature.TurtleModeChance > 0 && rollChance(w.Temperature.TurtleModeChance) {
		events = append(events, contestEvent{Message: "full turtle mode engaged without warning"})
	}
	if w.Temperature.PassOutChance > 0 && rollChance(w.Temperature.PassOutChance) {
		events = append(events, contestEvent{Message: "too hot to think and passed out, contest over!", Forfeit: true})
	}
	if w.Special != nil {
		if w.Special.InstantForfeitChance > 0 && rollChance(w.Special.InstantForfeitChance) {
			events = append(events, contestEvent{Message: "weather threw a full stop and ended the round", Forfeit: true})
		}
		if w.Special.MalfunctionChance > 0 && rollChance(w.Special.MalfunctionChance/100) {
			events = append(events, contestEvent{Message: "electrical static popped through the spot!"})
		}
	}

	if w.Wind.RandomEffectChance > 0 && rollChance(w.Wind.RandomEffectChance) {
		statOptions := []string{"distance", "volume", "aim", "duration"}
		stat := statOptions[rand.Intn(len(statOptions))]
		if rand.Float64() < 0.5 {
			events = append(events, contestEvent{Message: "a sudden gust shoved the stream away from the target"})
		} else {
			events = append(events, contestEvent{Message: "the wind bent the flow and changed the target toward " + stat})
		}
	}

	return events
}
