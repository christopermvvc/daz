package pissingcontest

import (
	"math/rand"
	"strings"
	"time"
)

var windEffects = []weather{
	{Name: "Dead Still", Description: "no wind", Effects: map[string]float64{}},
	{Name: "Light Breeze", Description: "gentle breeze", Effects: map[string]float64{"aim": -5, "distance_variance": 10}},
	{Name: "Gusty as Fuck", Description: "unpredictable gusts", Effects: map[string]float64{"aim": -20, "distance_variance": 30}},
	{Name: "Cyclone Warning", Description: "extreme winds", Effects: map[string]float64{"distance_tailwind": 50, "distance_headwind": -40, "aim": -60}},
	{Name: "Dust Storm", Description: "can't see shit", Effects: map[string]float64{"aim": -80}, MightHitOpponentChance: 0.3},
	{Name: "Willy Willy", Description: "spinning wind chaos", Effects: map[string]float64{}, RandomEffectChance: 0.3},
}

var temperatureEffects = []weather{
	{Name: "Freezing Me Nuts Off", Description: "cold and tight", Effects: map[string]float64{"volume": -40, "duration": 20}, TurtleModeChance: 0.3},
	{Name: "Not Too Shabby", Description: "decent enough", Effects: map[string]float64{}},
	{Name: "Bloody Hot", Description: "hydration advantage", Effects: map[string]float64{"volume": 30, "duration": -20}},
	{Name: "Satan's Armpit", Description: "too hot to think", Effects: map[string]float64{"duration": -20}, PassOutChance: 0.1},
}

var specialWeather = []weather{
	{Name: "Pissing Rain", Description: "ironic rain", Effects: map[string]float64{"aim": -40}, Message: "it's pissin' down!"},
	{Name: "Hailstorm", Description: "dangerous hail", Effects: map[string]float64{"aim": -80}, Message: "fuck! hail!", InstantForfeitChance: 0.2},
	{Name: "Lightning Storm", Description: "metal risk", Effects: map[string]float64{"aim": -60}, Message: "lightning risk!", MalfunctionChance: 10},
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
		events = append(events, contestEvent{Message: "wind blew piss onto opponent!"})
	}
	if w.Temperature.TurtleModeChance > 0 && rollChance(w.Temperature.TurtleModeChance) {
		events = append(events, contestEvent{Message: "full turtle mode activated"})
	}
	if w.Temperature.PassOutChance > 0 && rollChance(w.Temperature.PassOutChance) {
		events = append(events, contestEvent{Message: "passed out from heat!", Forfeit: true})
	}
	if w.Special != nil {
		if w.Special.InstantForfeitChance > 0 && rollChance(w.Special.InstantForfeitChance) {
			events = append(events, contestEvent{Message: "got hit by hail! Forfeit!", Forfeit: true})
		}
		if w.Special.MalfunctionChance > 0 && rollChance(w.Special.MalfunctionChance/100) {
			events = append(events, contestEvent{Message: "lightning struck the metal!"})
		}
	}

	if w.Wind.RandomEffectChance > 0 && rollChance(w.Wind.RandomEffectChance) {
		statOptions := []string{"distance", "volume", "aim", "duration"}
		stat := statOptions[rand.Intn(len(statOptions))]
		if rand.Float64() < 0.5 {
			events = append(events, contestEvent{Message: "Willy Willy blew the stream off course."})
		} else {
			events = append(events, contestEvent{Message: "The wind gotcha and changed the flow hard toward " + stat})
		}
	}

	return events
}
