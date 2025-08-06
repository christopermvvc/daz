package playlist

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	config    *Config
	running   bool
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	adminOnly bool
}

type Config struct {
	AdminOnly bool `json:"admin_only"`
}

func New() framework.Plugin {
	return &Plugin{
		name:      "playlist",
		adminOnly: true, // Default to admin only
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	
	// Parse configuration if provided
	if len(config) > 0 {
		var cfg Config
		if err := json.Unmarshal(config, &cfg); err != nil {
			logger.Error(p.name, "Failed to parse config: %v", err)
			return err
		}
		p.config = &cfg
		p.adminOnly = cfg.AdminOnly
	}
	
	p.ctx, p.cancel = context.WithCancel(context.Background())
	
	logger.Info(p.name, "Playlist plugin initialized")
	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.mu.Unlock()
	
	// Register command with EventFilter
	if err := p.registerCommand(); err != nil {
		logger.Error(p.name, "Failed to register command: %v", err)
		return err
	}
	
	// Subscribe to command execution events
	// EventFilter sends to command.{PLUGIN_NAME}.execute, not command.{COMMAND_NAME}.execute!
	err := p.eventBus.Subscribe("command.playlist.execute", p.handleCommand)
	if err != nil {
		logger.Error(p.name, "Failed to subscribe to command.playlist.execute: %v", err)
	} else {
		logger.Info(p.name, "Successfully subscribed to command.playlist.execute")
	}
	
	logger.Info(p.name, "Playlist plugin started")
	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin not running")
	}
	p.running = false
	p.mu.Unlock()
	
	if p.cancel != nil {
		p.cancel()
	}
	
	p.wg.Wait()
	
	logger.Info(p.name, "Playlist plugin stopped")
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	// Main event handler - most events are handled via subscriptions
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	state := "stopped"
	if p.running {
		state = "running"
	}
	
	return framework.PluginStatus{
		Name:  p.name,
		State: state,
	}
}

func (p *Plugin) Dependencies() []string {
	// Depend on eventfilter for command registration
	return []string{"eventfilter"}
}

func (p *Plugin) Ready() bool {
	// Check if dependencies are ready
	// This is called by the framework before starting
	return true
}

func (p *Plugin) registerCommand() error {
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "playlistadd",
					"min_rank": "0", // Allow all users, we'll check admin in handler
				},
			},
		},
	}
	
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		return fmt.Errorf("failed to broadcast registration: %w", err)
	}
	
	logger.Info(p.name, "Registered playlistadd command with EventFilter")
	return nil
}

func (p *Plugin) handleCommand(event framework.Event) error {
	logger.Info(p.name, "handleCommand called with event type: %T", event)
	
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		logger.Error(p.name, "Invalid event type for command: %T", event)
		return fmt.Errorf("invalid event type for command")
	}
	
	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}
	
	// Handle asynchronously
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.handlePlaylistCommand(dataEvent.Data.PluginRequest)
	}()
	
	return nil
}

func (p *Plugin) handlePlaylistCommand(req *framework.PluginRequest) {
	logger.Info(p.name, "handlePlaylistCommand called")
	
	if req.Data == nil || req.Data.Command == nil {
		logger.Error(p.name, "Missing command data in request")
		return
	}
	
	cmd := req.Data.Command
	params := cmd.Params
	
	// Join all args back together first (they were split by spaces in EventFilter)
	// This is necessary because HTML tags get broken across multiple args
	fullArgs := strings.Join(cmd.Args, " ")
	
	logger.Info(p.name, "Processing playlistadd command from %s in channel %s with raw args: %s",
		params["username"], params["channel"], fullArgs)
	
	// Check admin permission
	logger.Info(p.name, "Admin check: adminOnly=%v, is_admin=%s, username=%s", 
		p.adminOnly, params["is_admin"], params["username"])
	
	if p.adminOnly && params["is_admin"] != "true" {
		logger.Warn(p.name, "Non-admin user %s attempted to use playlistadd", params["username"])
		p.sendResponse(params["channel"], params["username"], 
			"Sorry, only admins can add items to the playlist.", params["is_pm"] == "true")
		return
	}
	
	// Parse arguments - need at least URL
	if fullArgs == "" {
		p.sendResponse(params["channel"], params["username"],
			"Usage: !playlistadd [title] <url>", params["is_pm"] == "true")
		return
	}
	
	var title, url string
	
	// Check if there's an HTML anchor tag with href (CyTube wraps URLs in <a> tags)
	hrefRegex := regexp.MustCompile(`<a[^>]*href="([^"]+)"[^>]*>.*?</a>`)
	if matches := hrefRegex.FindStringSubmatch(fullArgs); len(matches) > 1 {
		// Extract URL from href attribute
		url = matches[1]
		
		// Extract title: everything before the <a tag
		beforeLink := strings.Split(fullArgs, "<a")[0]
		title = strings.TrimSpace(beforeLink)
		
		if title == "" {
			title = "Default Title"
		}
		
		logger.Info(p.name, "Extracted from HTML - Title: %s, URL: %s", title, url)
	} else {
		// No HTML, parse normally
		// Check if we have a space-separated title and URL
		parts := strings.Fields(fullArgs)
		
		if len(parts) == 1 {
			// Just URL provided
			url = parts[0]
			title = "Default Title"
		} else {
			// Check if last part looks like a URL
			lastPart := parts[len(parts)-1]
			if strings.Contains(lastPart, "://") || strings.HasPrefix(lastPart, "www.") {
				// Last part is URL, everything before is title
				url = lastPart
				title = strings.Join(parts[:len(parts)-1], " ")
			} else {
				// Treat entire input as URL
				url = fullArgs
				title = "Default Title"
			}
		}
		
		logger.Info(p.name, "Parsed normally - Title: %s, URL: %s", title, url)
	}
	
	logger.Info(p.name, "Adding item to playlist - Title: %s, URL: %s, Channel: %s, User: %s",
		title, url, params["channel"], params["username"])
	
	// Send request to CyTube to add to playlist
	if err := p.addToPlaylist(params["channel"], title, url); err != nil {
		logger.Error(p.name, "Failed to add item to playlist: %v", err)
		p.sendResponse(params["channel"], params["username"],
			"Failed to add item to playlist. Check the URL and try again.",
			params["is_pm"] == "true")
		return
	}
	
	// Send success confirmation
	p.sendResponse(params["channel"], params["username"],
		fmt.Sprintf("Added '%s' to the playlist!", title),
		params["is_pm"] == "true")
}

func (p *Plugin) addToPlaylist(channel, title, url string) error {
	// Detect media type from URL
	mediaType := p.detectMediaType(url)
	mediaID := p.extractMediaID(url, mediaType)
	
	// Create the queue request for the core plugin
	queueRequest := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "core",
			From: p.name,
			Type: "queue_media",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"channel": channel,
					"type":    mediaType,
					"id":      mediaID,
					"pos":     "end",
					"temp":    "false",
					"title":   title,
				},
			},
		},
	}
	
	// Send queue request to core plugin
	if err := p.eventBus.Broadcast("plugin.request", queueRequest); err != nil {
		return fmt.Errorf("failed to send queue request: %w", err)
	}
	
	logger.Info(p.name, "Sent queue request for '%s' (type: %s, id: %s) in channel %s", 
		title, mediaType, mediaID, channel)
	
	return nil
}

// detectMediaType attempts to determine the media type from URL
func (p *Plugin) detectMediaType(url string) string {
	lowerURL := strings.ToLower(url)
	
	switch {
	case strings.Contains(lowerURL, "youtube.com") || strings.Contains(lowerURL, "youtu.be"):
		return "yt"
	case strings.Contains(lowerURL, "vimeo.com"):
		return "vi"
	case strings.Contains(lowerURL, "dailymotion.com"):
		return "dm"
	case strings.Contains(lowerURL, "twitch.tv"):
		return "tw"
	case strings.Contains(lowerURL, "soundcloud.com"):
		return "sc"
	case strings.HasSuffix(lowerURL, ".mp4") || strings.HasSuffix(lowerURL, ".webm") || 
		 strings.HasSuffix(lowerURL, ".ogg") || strings.HasSuffix(lowerURL, ".mp3"):
		return "fi" // Direct file
	default:
		// For unknown types, treat as direct file
		return "fi"
	}
}

// extractMediaID extracts the media ID from URL based on type
func (p *Plugin) extractMediaID(url, mediaType string) string {
	switch mediaType {
	case "yt":
		// Extract YouTube video ID
		if strings.Contains(url, "youtu.be/") {
			parts := strings.Split(url, "youtu.be/")
			if len(parts) > 1 {
				id := strings.Split(parts[1], "?")[0]
				return id
			}
		} else if strings.Contains(url, "v=") {
			parts := strings.Split(url, "v=")
			if len(parts) > 1 {
				id := strings.Split(parts[1], "&")[0]
				return id
			}
		}
	case "fi":
		// For direct files, use the full URL as ID
		return url
	default:
		// For other types, return the URL as-is
		return url
	}
	
	// Fallback to URL if extraction fails
	return url
}

func (p *Plugin) sendResponse(channel, username, message string, isPM bool) {
	if isPM {
		// Send as PM using plugin response system
		responseData := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					CommandResult: &framework.CommandResultData{
						Success: true,
						Output:  message,
					},
					KeyValue: map[string]string{
						"username": username,
						"channel":  channel,
					},
				},
			},
		}
		if err := p.eventBus.Broadcast("plugin.response", responseData); err != nil {
			logger.Error(p.name, "Failed to send PM response: %v", err)
		}
	} else {
		// Send as public chat message
		chatData := &framework.EventData{
			RawMessage: &framework.RawMessageData{
				Message: message,
				Channel: channel,
			},
		}
		if err := p.eventBus.Broadcast("cytube.send", chatData); err != nil {
			logger.Error(p.name, "Failed to send chat response: %v", err)
		}
	}
}