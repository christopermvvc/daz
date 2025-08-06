package playlist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

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
	
	// Playlist state tracking per channel
	playlists map[string][]PlaylistItem // channel -> playlist items
	
	// Batch import tracking
	batchImports map[string]*BatchImport // channel -> active batch import
}

type Config struct {
	AdminOnly bool `json:"admin_only"`
}

// PlaylistItem represents a single item in the playlist
type PlaylistItem struct {
	UID       string `json:"uid"`
	MediaID   string `json:"media_id"`
	MediaType string `json:"type"`
	Title     string `json:"title"`
	Duration  int    `json:"duration"`
	QueuedBy  string `json:"queueby"`
	Temp      bool   `json:"temp"`
}

// BatchImport tracks an ongoing batch import operation
type BatchImport struct {
	URLs      []string
	Current   int
	Total     int
	Succeeded int
	Failed    int
	StartTime time.Time
	User      string
	Channel   string
	Timer     *time.Timer
}

func New() framework.Plugin {
	return &Plugin{
		name:         "playlist",
		adminOnly:    true, // Default to admin only
		playlists:    make(map[string][]PlaylistItem),
		batchImports: make(map[string]*BatchImport),
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
	
	// Subscribe to playlist events to maintain state
	err = p.eventBus.Subscribe("cytube.event.playlist", p.handlePlaylistEvent)
	if err != nil {
		logger.Error(p.name, "Failed to subscribe to cytube.event.playlist: %v", err)
	} else {
		logger.Info(p.name, "Successfully subscribed to cytube.event.playlist")
	}
	
	// Subscribe to queue success events during batch imports
	err = p.eventBus.Subscribe("cytube.event.queue", p.handleQueueSuccess)
	if err != nil {
		logger.Error(p.name, "Failed to subscribe to cytube.event.queue: %v", err)
	}
	
	// Subscribe to queue failure events during batch imports
	err = p.eventBus.Subscribe("cytube.event.queueFail", p.handleQueueFail)
	if err != nil {
		logger.Error(p.name, "Failed to subscribe to cytube.event.queueFail: %v", err)
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
					"commands": "playlistadd,playlistdelete",
					"min_rank": "0", // Allow all users, we'll check admin in handler
				},
			},
		},
	}
	
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		return fmt.Errorf("failed to broadcast registration: %w", err)
	}
	
	logger.Info(p.name, "Registered playlistadd and playlistdelete commands with EventFilter")
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
	
	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.Command == nil {
		return nil
	}
	
	// Route to appropriate handler based on command
	cmdName := req.Data.Command.Name
	
	// Handle asynchronously
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		
		switch cmdName {
		case "playlistadd":
			p.handlePlaylistAddCommand(req)
		case "playlistdelete":
			p.handlePlaylistDeleteCommand(req)
		default:
			logger.Warn(p.name, "Unknown command: %s", cmdName)
		}
	}()
	
	return nil
}

func (p *Plugin) handlePlaylistEvent(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}
	
	if dataEvent.Data == nil || dataEvent.Data.RawEvent == nil {
		return nil
	}
	
	// Check if it's a PlaylistArrayEvent (full playlist)
	if playlistEvent, ok := dataEvent.Data.RawEvent.(*framework.PlaylistArrayEvent); ok {
		channel := playlistEvent.ChannelName
		
		p.mu.Lock()
		// Convert framework.PlaylistItem to our internal PlaylistItem
		items := make([]PlaylistItem, len(playlistEvent.Items))
		for i, item := range playlistEvent.Items {
			items[i] = PlaylistItem{
				UID:       item.UID,
				MediaID:   item.MediaID,
				MediaType: item.MediaType,
				Title:     item.Title,
				Duration:  item.Duration,
				QueuedBy:  item.QueuedBy,
				// Note: Temp field is not in framework.PlaylistItem
			}
		}
		p.playlists[channel] = items
		p.mu.Unlock()
		
		logger.Info(p.name, "Updated playlist for channel %s with %d items", channel, len(items))
	}
	
	return nil
}

func (p *Plugin) handlePlaylistAddCommand(req *framework.PluginRequest) {
	logger.Info(p.name, "handlePlaylistAddCommand called")
	
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
	
	// Check if it's a Pastebin URL
	if strings.Contains(url, "pastebin.com/") && !strings.Contains(url, "/raw/") {
		logger.Info(p.name, "Detected Pastebin URL, processing batch import")
		p.handlePastebinImport(params["channel"], params["username"], url, params["is_pm"] == "true")
		return
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
	case strings.Contains(lowerURL, "docs.google.com") || strings.Contains(lowerURL, "drive.google.com"):
		return "gd" // Google Drive
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
	case "gd":
		// Extract Google Drive file ID
		// Format: https://docs.google.com/file/d/FILE_ID/...
		// or: https://drive.google.com/file/d/FILE_ID/...
		if strings.Contains(url, "/file/d/") {
			parts := strings.Split(url, "/file/d/")
			if len(parts) > 1 {
				// Get the ID part (everything before the next /)
				idParts := strings.Split(parts[1], "/")
				if len(idParts) > 0 {
					return idParts[0]
				}
			}
		} else if strings.Contains(url, "/d/") {
			parts := strings.Split(url, "/d/")
			if len(parts) > 1 {
				idParts := strings.Split(parts[1], "/")
				if len(idParts) > 0 {
					return idParts[0]
				}
			}
		}
		// Fallback: return the full URL for Google Drive
		return url
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

func (p *Plugin) handlePlaylistDeleteCommand(req *framework.PluginRequest) {
	logger.Info(p.name, "handlePlaylistDeleteCommand called")
	
	if req.Data == nil || req.Data.Command == nil {
		logger.Error(p.name, "Missing command data in request")
		return
	}
	
	cmd := req.Data.Command
	params := cmd.Params
	
	// Join all args back together (they were split by spaces in EventFilter)
	fullArgs := strings.Join(cmd.Args, " ")
	
	logger.Info(p.name, "Processing playlistdelete command from %s in channel %s with raw args: %s",
		params["username"], params["channel"], fullArgs)
	
	// Check admin permission
	if p.adminOnly && params["is_admin"] != "true" {
		logger.Warn(p.name, "Non-admin user %s attempted to use playlistdelete", params["username"])
		p.sendResponse(params["channel"], params["username"], 
			"Sorry, only admins can delete items from the playlist.", params["is_pm"] == "true")
		return
	}
	
	// Parse URL from arguments
	if fullArgs == "" {
		p.sendResponse(params["channel"], params["username"],
			"Usage: !playlistdelete <url>", params["is_pm"] == "true")
		return
	}
	
	var url string
	
	// Check if there's an HTML anchor tag with href (CyTube wraps URLs in <a> tags)
	hrefRegex := regexp.MustCompile(`<a[^>]*href="([^"]+)"[^>]*>.*?</a>`)
	if matches := hrefRegex.FindStringSubmatch(fullArgs); len(matches) > 1 {
		url = matches[1]
	} else {
		// No HTML, use the raw argument
		url = strings.TrimSpace(fullArgs)
	}
	
	logger.Info(p.name, "Looking for URL %s in playlist for channel %s", url, params["channel"])
	
	// Find the UID for this URL in our cached playlist
	uid := p.findUIDByURL(params["channel"], url)
	if uid == "" {
		logger.Warn(p.name, "URL not found in playlist: %s", url)
		p.sendResponse(params["channel"], params["username"],
			"That URL was not found in the playlist.", params["is_pm"] == "true")
		return
	}
	
	logger.Info(p.name, "Found UID %s for URL %s, sending delete request", uid, url)
	
	// Send delete request to core plugin
	if err := p.deleteFromPlaylist(params["channel"], uid); err != nil {
		logger.Error(p.name, "Failed to delete item from playlist: %v", err)
		p.sendResponse(params["channel"], params["username"],
			"Failed to delete item from playlist.",
			params["is_pm"] == "true")
		return
	}
	
	// Send success confirmation
	p.sendResponse(params["channel"], params["username"],
		"Item removed from the playlist!",
		params["is_pm"] == "true")
}

func (p *Plugin) findUIDByURL(channel, url string) string {
	// First check in-memory cache
	p.mu.RLock()
	playlist, exists := p.playlists[channel]
	p.mu.RUnlock()
	
	if exists {
		// Search for exact URL match in cache
		for _, item := range playlist {
			// For direct files (type "fi"), the MediaID is the URL
			if item.MediaType == "fi" && item.MediaID == url {
				return item.UID
			}
			
			// For other media types, construct the URL and compare
			constructedURL := p.constructURL(item.MediaType, item.MediaID)
			if constructedURL == url {
				return item.UID
			}
		}
	}

	// If not found in cache or cache doesn't exist, query SQL database
	logger.Info(p.name, "Cache miss for channel %s, querying SQL for UID", channel)
	
	// Extract media ID and type from URL
	mediaType := p.detectMediaType(url)
	mediaID := p.extractMediaID(url, mediaType)
	
	logger.Info(p.name, "SQL lookup - Channel: %s, MediaID: %s, MediaType: %s", channel, mediaID, mediaType)
	
	// Generate a correlation ID for the request
	correlationID := fmt.Sprintf("%d", time.Now().UnixNano())
	
	// Query the SQL database for the UID
	queryReq := &framework.EventData{
		SQLQueryRequest: &framework.SQLQueryRequest{
			CorrelationID: correlationID,
			Query: `SELECT uid FROM daz_mediatracker_queue 
					WHERE channel = $1 AND media_id = $2 AND media_type = $3 
					ORDER BY position ASC LIMIT 1`,
			Params: []framework.SQLParam{
				framework.NewSQLParam(channel),
				framework.NewSQLParam(mediaID),
				framework.NewSQLParam(mediaType),
			},
			RequestBy: p.name,
		},
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Create metadata for the request
	metadata := &framework.EventMetadata{
		Source:        p.name,
		CorrelationID: correlationID,
	}
	
	responseEvent, err := p.eventBus.Request(ctx, "sql", "plugin.request", queryReq, metadata)
	if err != nil {
		logger.Error(p.name, "Failed to query SQL for UID: %v", err)
		return ""
	}
	
	// Log the raw response for debugging
	if responseEvent != nil {
		if responseEvent.PluginResponse != nil {
			logger.Info(p.name, "Got plugin response instead of SQL response")
		}
	}
	
	if responseEvent == nil || responseEvent.SQLQueryResponse == nil {
		logger.Error(p.name, "Invalid SQL response format - nil SQL response")
		return ""
	}
	
	response := responseEvent.SQLQueryResponse
	if !response.Success {
		logger.Error(p.name, "SQL query failed: %s", response.Error)
		return ""
	}
	
	if len(response.Rows) > 0 && len(response.Rows[0]) > 0 {
		// Parse the UID from the response
		var uid string
		if err := json.Unmarshal(response.Rows[0][0], &uid); err != nil {
			logger.Error(p.name, "Failed to parse UID from SQL response: %v", err)
			return ""
		}
		logger.Info(p.name, "Found UID %s for URL %s via SQL", uid, url)
		return uid
	}
	
	logger.Warn(p.name, "URL %s not found in SQL database for channel %s", url, channel)
	return ""
}

func (p *Plugin) constructURL(mediaType, mediaID string) string {
	switch mediaType {
	case "yt":
		return "https://youtube.com/watch?v=" + mediaID
	case "vi":
		return "https://vimeo.com/" + mediaID
	case "dm":
		return "https://dailymotion.com/video/" + mediaID
	case "tw":
		return "https://twitch.tv/" + mediaID
	case "sc":
		return mediaID // SoundCloud URLs are stored as-is
	case "fi":
		return mediaID // Direct files store full URL as ID
	default:
		return mediaID
	}
}


func (p *Plugin) deleteFromPlaylist(channel, uid string) error {
	// Create the delete request for the core plugin
	deleteRequest := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "core",
			From: p.name,
			Type: "delete_media",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"channel": channel,
					"uid":     uid,
				},
			},
		},
	}
	
	// Send delete request to core plugin
	if err := p.eventBus.Broadcast("plugin.request", deleteRequest); err != nil {
		return fmt.Errorf("failed to send delete request: %w", err)
	}
	
	logger.Info(p.name, "Sent delete request for UID '%s' in channel %s", uid, channel)
	
	return nil
}

// handlePastebinImport processes a Pastebin URL containing a list of URLs to import
func (p *Plugin) handlePastebinImport(channel, username, pastebinURL string, isPM bool) {
	// Convert to raw URL
	rawURL := strings.Replace(pastebinURL, "pastebin.com/", "pastebin.com/raw/", 1)
	
	logger.Info(p.name, "Fetching Pastebin content from %s", rawURL)
	
	// Fetch the content
	resp, err := http.Get(rawURL)
	if err != nil {
		logger.Error(p.name, "Failed to fetch Pastebin content: %v", err)
		p.sendResponse(channel, username, "Failed to fetch Pastebin content. Please check the URL.", isPM)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		logger.Error(p.name, "Pastebin returned status %d", resp.StatusCode)
		p.sendResponse(channel, username, fmt.Sprintf("Failed to fetch Pastebin content (status %d).", resp.StatusCode), isPM)
		return
	}
	
	// Read the content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error(p.name, "Failed to read Pastebin content: %v", err)
		p.sendResponse(channel, username, "Failed to read Pastebin content.", isPM)
		return
	}
	
	// Parse URLs from content - handle both newline and comma-separated formats
	contentStr := string(content)
	var urls []string
	
	// First check if content contains commas (likely comma-separated)
	if strings.Contains(contentStr, ",") {
		// Split by commas
		items := strings.Split(contentStr, ",")
		for _, item := range items {
			item = strings.TrimSpace(item)
			// Skip empty items and comments
			if item == "" || strings.HasPrefix(item, "#") {
				continue
			}
			// Basic URL validation
			if strings.Contains(item, "://") || strings.HasPrefix(item, "www.") {
				urls = append(urls, item)
			}
		}
	} else {
		// Split by newlines (original format)
		lines := strings.Split(contentStr, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Skip empty lines and comments (lines starting with #)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Basic URL validation
			if strings.Contains(line, "://") || strings.HasPrefix(line, "www.") {
				urls = append(urls, line)
			}
		}
	}
	
	if len(urls) == 0 {
		p.sendResponse(channel, username, "No valid URLs found in the Pastebin content.", isPM)
		return
	}
	
	// Check if there's already a batch import in progress for this channel
	p.mu.Lock()
	if existing, exists := p.batchImports[channel]; exists {
		p.mu.Unlock()
		p.sendResponse(channel, username, 
			fmt.Sprintf("A batch import is already in progress (%d/%d items). Please wait for it to complete.", 
				existing.Current, existing.Total), isPM)
		return
	}
	
	// Create new batch import
	batchImport := &BatchImport{
		URLs:      urls,
		Current:   0,
		Total:     len(urls),
		Succeeded: 0,
		Failed:    0,
		StartTime: time.Now(),
		User:      username,
		Channel:   channel,
	}
	p.batchImports[channel] = batchImport
	p.mu.Unlock()
	
	// Send initial response
	p.sendResponse(channel, username, 
		fmt.Sprintf("Starting batch import of %d URLs. Items will be added every 2 seconds.", len(urls)), isPM)
	
	// Start the batch import process
	p.wg.Add(1)
	go p.processBatchImport(batchImport, isPM)
}

// processBatchImport handles the actual batch import process
func (p *Plugin) processBatchImport(batch *BatchImport, isPM bool) {
	defer p.wg.Done()
	
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	// Add first item immediately
	p.addBatchItem(batch, isPM)
	
	for {
		select {
		case <-ticker.C:
			if !p.addBatchItem(batch, isPM) {
				// Batch complete
				return
			}
			
		case <-p.ctx.Done():
			// Plugin shutting down
			p.mu.Lock()
			delete(p.batchImports, batch.Channel)
			p.mu.Unlock()
			return
		}
	}
}

// addBatchItem adds the next item from the batch and returns false if batch is complete
func (p *Plugin) addBatchItem(batch *BatchImport, isPM bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Check if batch is complete
	if batch.Current >= batch.Total {
		// Batch complete
		elapsed := time.Since(batch.StartTime)
		message := fmt.Sprintf("Batch import complete! Processed %d items in %s. Success: %d, Failed: %d", 
			batch.Total, elapsed.Round(time.Second), batch.Succeeded, batch.Failed)
		if batch.Failed > 0 {
			message += " (Failed items likely due to Google Drive restrictions)"
		}
		p.sendResponse(batch.Channel, batch.User, message, isPM)
		delete(p.batchImports, batch.Channel)
		return false
	}
	
	// Get next URL
	url := batch.URLs[batch.Current]
	batch.Current++
	
	// Check if we should send a progress update (at 50% for lists > 100 items)
	if batch.Total > 100 && batch.Current == batch.Total/2 {
		p.sendResponse(batch.Channel, batch.User,
			fmt.Sprintf("Batch import 50%% complete (%d/%d items added).", batch.Current, batch.Total), isPM)
	}
	
	// Add the item to playlist (title will be auto-fetched by CyTube)
	logger.Info(p.name, "Adding batch item %d/%d: %s", batch.Current, batch.Total, url)
	
	// Use goroutine to avoid blocking the ticker
	go func(url string, current, total int) {
		// Don't pass empty title - let CyTube fetch it
		if err := p.addToPlaylist(batch.Channel, "Batch Import Item", url); err != nil {
			logger.Error(p.name, "Failed to add batch item %d/%d: %v", current, total, err)
			// Continue with next item even if this one fails
		}
	}(url, batch.Current, batch.Total)
	
	return true
}

// handleQueueSuccess tracks successful queue additions during batch imports
func (p *Plugin) handleQueueSuccess(event framework.Event) error {
	// Extract channel name from the event
	var channelName string
	if cytubeEvent, ok := event.(*framework.CytubeEvent); ok {
		channelName = cytubeEvent.ChannelName
	} else if genericEvent, ok := event.(*framework.GenericEvent); ok {
		// GenericEvent embeds CytubeEvent
		channelName = genericEvent.ChannelName
	}
	
	if channelName == "" {
		// Can't determine channel, skip tracking
		return nil
	}
	
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Only track for the specific channel with a batch import
	if batch, exists := p.batchImports[channelName]; exists {
		// Only count if we're expecting a response (haven't counted all items yet)
		if batch.Current > batch.Succeeded + batch.Failed {
			batch.Succeeded++
			logger.Debug(p.name, "Batch import item succeeded for channel %s (%d/%d)", 
				channelName, batch.Succeeded, batch.Total)
		}
	}
	
	return nil
}

// handleQueueFail tracks failed queue additions during batch imports
func (p *Plugin) handleQueueFail(event framework.Event) error {
	// Extract channel name from the event
	var channelName string
	var failureReason string
	
	if cytubeEvent, ok := event.(*framework.CytubeEvent); ok {
		channelName = cytubeEvent.ChannelName
	} else if genericEvent, ok := event.(*framework.GenericEvent); ok {
		// GenericEvent embeds CytubeEvent
		channelName = genericEvent.ChannelName
		
		// Try to extract failure reason from raw JSON
		if len(genericEvent.RawJSON) > 0 {
			var queueFailData struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(genericEvent.RawJSON, &queueFailData); err == nil {
				failureReason = queueFailData.Msg
			}
		}
	}
	
	if channelName == "" {
		// Can't determine channel, skip tracking
		return nil
	}
	
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Only track for the specific channel with a batch import
	if batch, exists := p.batchImports[channelName]; exists {
		// Only count if we're expecting a response (haven't counted all items yet)
		if batch.Current > batch.Succeeded + batch.Failed {
			batch.Failed++
			logger.Debug(p.name, "Batch import item failed for channel %s (%d/%d)", 
				channelName, batch.Failed, batch.Total)
			
			if failureReason != "" {
				logger.Info(p.name, "Queue failure reason: %s", failureReason)
			}
		}
	}
	
	return nil
}