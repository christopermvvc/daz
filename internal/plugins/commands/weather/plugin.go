package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
	name           string
	running        bool
	eventBus       framework.EventBus
	lastRequestMap map[string]time.Time
	rateLimitSecs  int
	mu             sync.RWMutex
	config         Config
}

type Config struct {
	RateLimitSeconds int `json:"rate_limit_seconds"`
}

func New() framework.Plugin {
	return &Plugin{
		name:           "weather",
		running:        false,
		lastRequestMap: make(map[string]time.Time),
		rateLimitSecs:  20,
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
		if p.config.RateLimitSeconds > 0 {
			p.rateLimitSecs = p.config.RateLimitSeconds
		}
	}

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	registerEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "weather,w",
					"min_rank": "0",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", registerEvent); err != nil {
		logger.Error(p.name, "Failed to register command: %v", err)
		return fmt.Errorf("failed to register command: %w", err)
	}

	if err := p.eventBus.Subscribe("command.weather.execute", p.handleWeatherCommand); err != nil {
		return fmt.Errorf("failed to subscribe to weather command: %w", err)
	}
	if err := p.eventBus.Subscribe("command.w.execute", p.handleWeatherCommand); err != nil {
		return fmt.Errorf("failed to subscribe to w command: %w", err)
	}

	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()

	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := "stopped"
	if p.running {
		status = "running"
	}

	return framework.PluginStatus{
		Name:  p.name,
		State: status,
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) handleWeatherCommand(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest

	// Extract username and channel from Command.Params
	username := ""
	channel := ""
	isPM := false
	if req.Data != nil && req.Data.Command != nil && req.Data.Command.Params != nil {
		username = req.Data.Command.Params["username"]
		channel = req.Data.Command.Params["channel"]
		if pmFlag := req.Data.Command.Params["is_pm"]; pmFlag == "true" {
			isPM = true
		}
	}

	// Get args from Command.Args
	var args []string
	if req.Data != nil && req.Data.Command != nil {
		args = req.Data.Command.Args
	}

	if !p.checkRateLimit(username) {
		message := fmt.Sprintf("Please wait %d seconds between weather requests.", p.rateLimitSecs)
		p.sendResponse(username, channel, message, false, isPM)
		return nil
	}

	if len(args) == 0 {
		p.sendResponse(username, channel, "Please specify a location. Usage: !weather <location>", false, isPM)
		return nil
	}

	location := strings.Join(args, " ")
	weather, err := p.getWeather(location)
	if err != nil {
		logger.Warn(p.name, "Weather lookup failed for location %q: %v", location, err)
		p.sendResponse(username, channel, fmt.Sprintf("Couldn't fetch weather for %q right now. Please try again in a bit.", location), false, isPM)
		return nil
	}

	p.sendResponse(username, channel, weather, true, isPM)
	return nil
}

func (p *Plugin) checkRateLimit(username string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	lastRequest, exists := p.lastRequestMap[username]
	if !exists || time.Since(lastRequest) >= time.Duration(p.rateLimitSecs)*time.Second {
		p.lastRequestMap[username] = time.Now()
		return true
	}
	return false
}

func (p *Plugin) getWeather(location string) (string, error) {
	if weather, err := p.getOpenMeteoWeather(location); err == nil {
		return weather, nil
	}

	if weather, err := p.getSimpleWeather(location); err == nil {
		return weather, nil
	}

	if weather, err := p.getWttrJSONWeather(location); err == nil {
		return weather, nil
	}

	return "", fmt.Errorf("all weather providers failed")
}

func (p *Plugin) getWttrJSONWeather(location string) (string, error) {

	encodedLocation := url.QueryEscape(location)

	// Using custom format for more detailed weather info
	// %l = location, %C = weather condition, %t = temperature, %f = feels like
	// %h = humidity, %w = wind, %p = precipitation, %P = pressure
	weatherURL := fmt.Sprintf("https://wttr.in/%s?format=j1", encodedLocation)

	client := &http.Client{Timeout: 5 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", weatherURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "daz-bot/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("weather service returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	bodyText := strings.TrimSpace(string(body))
	if looksLikeHTML(bodyText) {
		return "", fmt.Errorf("weather service returned an unexpected response")
	}

	// Parse JSON response
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}

	// Extract and format the weather data
	formatted := p.formatWeatherResponse(data)
	if formatted == "" {
		return "", fmt.Errorf("unable to format weather data")
	}

	return formatted, nil
}

func (p *Plugin) getSimpleWeather(location string) (string, error) {
	encodedLocation := url.QueryEscape(location)
	url := fmt.Sprintf("https://wttr.in/%s?format=%%l:+%%c+%%t+(feels+like+%%f)%%nTomorrow:+%%c+%%t", encodedLocation)

	client := &http.Client{Timeout: 5 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "daz-bot/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	weather := strings.TrimSpace(string(body))
	if weather == "" {
		return "", fmt.Errorf("no weather data received")
	}

	if looksLikeHTML(weather) {
		return "", fmt.Errorf("weather service returned an unexpected response")
	}

	if strings.Contains(weather, "Unknown location") {
		return "", fmt.Errorf("unknown location: %s", location)
	}

	return weather, nil
}

func (p *Plugin) getOpenMeteoWeather(location string) (string, error) {
	geoURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=en&format=json", url.QueryEscape(location))
	geoReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, geoURL, nil)
	if err != nil {
		return "", err
	}
	geoReq.Header.Set("User-Agent", "daz-bot/1.0")

	client := &http.Client{Timeout: 4 * time.Second}
	geoResp, err := client.Do(geoReq)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = geoResp.Body.Close()
	}()

	if geoResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("geocoding returned status %d", geoResp.StatusCode)
	}

	geoBody, err := io.ReadAll(geoResp.Body)
	if err != nil {
		return "", err
	}

	var geoData struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
		} `json:"results"`
	}

	if err := json.Unmarshal(geoBody, &geoData); err != nil {
		return "", err
	}
	if len(geoData.Results) == 0 {
		return "", fmt.Errorf("unknown location: %s", location)
	}

	place := geoData.Results[0]
	forecastURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current=temperature_2m,apparent_temperature,weather_code&daily=weather_code,temperature_2m_max,temperature_2m_min&timezone=auto&forecast_days=2",
		place.Latitude,
		place.Longitude,
	)

	forecastReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, forecastURL, nil)
	if err != nil {
		return "", err
	}
	forecastReq.Header.Set("User-Agent", "daz-bot/1.0")

	forecastResp, err := client.Do(forecastReq)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = forecastResp.Body.Close()
	}()

	if forecastResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("forecast returned status %d", forecastResp.StatusCode)
	}

	forecastBody, err := io.ReadAll(forecastResp.Body)
	if err != nil {
		return "", err
	}

	var forecastData struct {
		Current struct {
			Temperature2M       float64 `json:"temperature_2m"`
			ApparentTemperature float64 `json:"apparent_temperature"`
			WeatherCode         int     `json:"weather_code"`
		} `json:"current"`
		Daily struct {
			WeatherCode      []int     `json:"weather_code"`
			Temperature2MMax []float64 `json:"temperature_2m_max"`
			Temperature2MMin []float64 `json:"temperature_2m_min"`
		} `json:"daily"`
	}

	if err := json.Unmarshal(forecastBody, &forecastData); err != nil {
		return "", err
	}

	locationLabel := place.Name
	if place.Country != "" {
		locationLabel = fmt.Sprintf("%s, %s", place.Name, place.Country)
	}

	nowDesc := weatherCodeDescription(forecastData.Current.WeatherCode)
	nowEmoji := p.getWeatherEmoji(nowDesc)
	nowC := forecastData.Current.Temperature2M
	nowF := cToF(nowC)
	feelsC := forecastData.Current.ApparentTemperature
	feelsF := cToF(feelsC)

	response := fmt.Sprintf("📍 %s\n%s Now: %s • %.0f°C/%.0f°F (feels %.0f°C/%.0f°F)",
		locationLabel,
		nowEmoji,
		nowDesc,
		nowC,
		nowF,
		feelsC,
		feelsF,
	)

	if len(forecastData.Daily.WeatherCode) > 1 &&
		len(forecastData.Daily.Temperature2MMin) > 1 &&
		len(forecastData.Daily.Temperature2MMax) > 1 {
		tomorrowDesc := weatherCodeDescription(forecastData.Daily.WeatherCode[1])
		tomorrowEmoji := p.getWeatherEmoji(tomorrowDesc)
		minC := forecastData.Daily.Temperature2MMin[1]
		maxC := forecastData.Daily.Temperature2MMax[1]
		response += fmt.Sprintf("\n%s Tomorrow: %s • %.0f-%.0f°C/%.0f-%.0f°F",
			tomorrowEmoji,
			tomorrowDesc,
			minC,
			maxC,
			cToF(minC),
			cToF(maxC),
		)
	}

	return response, nil
}

func cToF(c float64) float64 {
	return c*9/5 + 32
}

func weatherCodeDescription(code int) string {
	switch code {
	case 0:
		return "Clear"
	case 1:
		return "Mainly clear"
	case 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Fog"
	case 51, 53, 55, 56, 57:
		return "Drizzle"
	case 61, 63, 65, 66, 67, 80, 81, 82:
		return "Rain"
	case 71, 73, 75, 77, 85, 86:
		return "Snow"
	case 95, 96, 99:
		return "Thunderstorm"
	default:
		return "Unknown"
	}
}

func looksLikeHTML(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.HasPrefix(lower, "<head") ||
		strings.HasPrefix(lower, "<body")
}

func (p *Plugin) formatWeatherResponse(data map[string]interface{}) string {
	// Extract current conditions
	current, ok := data["current_condition"].([]interface{})
	if !ok || len(current) == 0 {
		return ""
	}

	currentCond, ok := current[0].(map[string]interface{})
	if !ok {
		return ""
	}

	// Extract location
	nearestArea, _ := data["nearest_area"].([]interface{})
	location := "Unknown Location"
	if len(nearestArea) > 0 {
		area, ok := nearestArea[0].(map[string]interface{})
		if ok {
			if areaName, ok := area["areaName"].([]interface{}); ok && len(areaName) > 0 {
				if nameEntry, ok := areaName[0].(map[string]interface{}); ok {
					if name, ok := nameEntry["value"].(string); ok {
						location = name
					}
				}
			}
			if country, ok := area["country"].([]interface{}); ok && len(country) > 0 {
				if countryEntry, ok := country[0].(map[string]interface{}); ok {
					if countryName, ok := countryEntry["value"].(string); ok {
						location += ", " + countryName
					}
				}
			}
		}
	}

	// Extract weather description
	weatherDesc := ""
	if desc, ok := currentCond["weatherDesc"].([]interface{}); ok && len(desc) > 0 {
		if val, ok := desc[0].(map[string]interface{})["value"].(string); ok {
			weatherDesc = val
		}
	}

	// Extract temperatures
	tempC, _ := currentCond["temp_C"].(string)
	tempF, _ := currentCond["temp_F"].(string)

	// Get weather emoji
	emoji := p.getWeatherEmoji(weatherDesc)

	// Format current weather - concise version
	response := fmt.Sprintf("📍 %s\n", location)
	response += fmt.Sprintf("%s Now: %s • %s°C/%s°F\n", emoji, weatherDesc, tempC, tempF)

	// Add forecast for tomorrow
	if weather, ok := data["weather"].([]interface{}); ok && len(weather) > 1 {
		tomorrow, ok := weather[1].(map[string]interface{})
		if ok {
			maxTempC, _ := tomorrow["maxtempC"].(string)
			maxTempF, _ := tomorrow["maxtempF"].(string)
			minTempC, _ := tomorrow["mintempC"].(string)
			minTempF, _ := tomorrow["mintempF"].(string)

			tomorrowDesc := ""
			if hourly, ok := tomorrow["hourly"].([]interface{}); ok && len(hourly) > 0 {
				if midday, ok := hourly[len(hourly)/2].(map[string]interface{}); ok {
					if desc, ok := midday["weatherDesc"].([]interface{}); ok && len(desc) > 0 {
						if valEntry, ok := desc[0].(map[string]interface{}); ok {
							if val, ok := valEntry["value"].(string); ok {
								tomorrowDesc = val
							}
						}
					}
				}
			}

			tomorrowEmoji := p.getWeatherEmoji(tomorrowDesc)

			response += fmt.Sprintf("%s Tomorrow: %s • %s-%s°C/%s-%s°F",
				tomorrowEmoji, tomorrowDesc, minTempC, maxTempC, minTempF, maxTempF)
		}
	}

	return response
}

func (p *Plugin) getWeatherEmoji(description string) string {
	desc := strings.ToLower(description)
	switch {
	case strings.Contains(desc, "clear") || strings.Contains(desc, "sunny"):
		return "☀️"
	case strings.Contains(desc, "partly cloudy"):
		return "⛅"
	case strings.Contains(desc, "cloudy") || strings.Contains(desc, "overcast"):
		return "☁️"
	case strings.Contains(desc, "rain") || strings.Contains(desc, "drizzle"):
		return "🌧️"
	case strings.Contains(desc, "thunder") || strings.Contains(desc, "storm"):
		return "⛈️"
	case strings.Contains(desc, "snow"):
		return "❄️"
	case strings.Contains(desc, "fog") || strings.Contains(desc, "mist"):
		return "🌫️"
	case strings.Contains(desc, "wind"):
		return "💨"
	default:
		return "🌤️"
	}
}

func (p *Plugin) sendResponse(username, channel, message string, success, isPM bool) {
	if isPM {
		responseData := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				From:    p.name,
				Success: success,
				Data: &framework.ResponseData{
					CommandResult: &framework.CommandResultData{
						Success: success,
						Output:  message,
					},
					KeyValue: map[string]string{
						"username": username,
						"channel":  channel,
					},
				},
			},
		}
		_ = p.eventBus.Broadcast("plugin.response", responseData)
		return
	}

	chatData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}
	_ = p.eventBus.Broadcast("cytube.send", chatData)
}
