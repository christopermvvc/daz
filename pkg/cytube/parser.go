package cytube

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Parser struct {
	channel string
	roomID  string
}

func NewParser(channel string, roomID string) *Parser {
	return &Parser{
		channel: channel,
		roomID:  roomID,
	}
}

func (p *Parser) ParseEvent(event Event) (framework.Event, error) {
	timestamp := time.Now()

	baseEvent := framework.CytubeEvent{
		EventType:   event.Type,
		EventTime:   timestamp,
		ChannelName: p.channel,
		RoomID:      p.roomID,
		RawData:     event.Data,
		Metadata:    make(map[string]string),
	}

	// Only log important events or errors
	// Remove verbose logging for production

	switch event.Type {
	case "chatMsg":
		return p.parseChatMessage(baseEvent, event.Data)
	case "pm":
		return p.parsePM(baseEvent, event.Data)
	case "userJoin":
		return p.parseUserJoin(baseEvent, event.Data)
	case "userLeave":
		return p.parseUserLeave(baseEvent, event.Data)
	case "changeMedia":
		return p.parseVideoChange(baseEvent, event.Data)
	case "mediaUpdate":
		return p.parseMediaUpdate(baseEvent, event.Data)
	case "setCurrent":
		return &baseEvent, nil
	case "login":
		return p.parseLogin(baseEvent, event.Data)
	case "addUser":
		return p.parseAddUser(baseEvent, event.Data)
	case "meta":
		return p.parseChannelMeta(baseEvent, event.Data)
	case "playlist":
		return p.parsePlaylist(baseEvent, event.Data)
	case "setPlaylistMeta":
		return p.parseSetPlaylistMeta(baseEvent, event.Data)
	case "usercount", "userCount":
		return p.parseUserCount(baseEvent, event.Data)
	case "setUserRank":
		return p.parseSetUserRank(baseEvent, event.Data)
	case "setPlaylistLocked":
		return p.parseSetPlaylistLocked(baseEvent, event.Data)
	case "setPermissions":
		return p.parseSetPermissions(baseEvent, event.Data)
	case "setMotd":
		return p.parseSetMotd(baseEvent, event.Data)
	case "channelOpts":
		return p.parseChannelOpts(baseEvent, event.Data)
	case "channelCSSJS":
		return p.parseChannelCSSJS(baseEvent, event.Data)
	case "setUserMeta":
		return p.parseSetUserMeta(baseEvent, event.Data)
	case "setAFK":
		return p.parseSetAFK(baseEvent, event.Data)
	case "clearVoteskipVote":
		return p.parseClearVoteskipVote(baseEvent, event.Data)
	case "queue":
		return p.parseQueueEvent(baseEvent, event.Data)
	case "delete":
		return p.parseDeleteEvent(baseEvent, event.Data)
	case "moveVideo":
		return p.parseMoveVideoEvent(baseEvent, event.Data)
	case "queueFail":
		return p.parseQueueFailEvent(baseEvent, event.Data)
	case "userlist":
		return p.parseUserListEvent(baseEvent, event.Data)
	default:
		// Use generic event handler for unknown types
		return p.parseGenericEvent(baseEvent, event.Type, event.Data)
	}
}

func (p *Parser) parseChatMessage(base framework.CytubeEvent, data json.RawMessage) (*framework.ChatMessageEvent, error) {
	var msg ChatMessagePayload
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal chat message: %w", err)
	}

	event := &framework.ChatMessageEvent{
		CytubeEvent: base,
		Username:    msg.Username,
		Message:     msg.Message,
		UserRank:    msg.Rank,
		MessageTime: msg.Time,
	}

	if msg.Meta.UID != "" {
		event.UserID = msg.Meta.UID
	}

	return event, nil
}

func (p *Parser) parseUserJoin(base framework.CytubeEvent, data json.RawMessage) (*framework.UserJoinEvent, error) {
	var user UserPayload
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("unmarshal user join: %w", err)
	}

	event := &framework.UserJoinEvent{
		CytubeEvent: base,
		Username:    user.Name,
		UserRank:    user.Rank,
	}

	return event, nil
}

func (p *Parser) parseUserLeave(base framework.CytubeEvent, data json.RawMessage) (*framework.UserLeaveEvent, error) {
	var user UserPayload
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("unmarshal user leave: %w", err)
	}

	event := &framework.UserLeaveEvent{
		CytubeEvent: base,
		Username:    user.Name,
	}

	return event, nil
}

func (p *Parser) parseVideoChange(base framework.CytubeEvent, data json.RawMessage) (*framework.VideoChangeEvent, error) {
	var media MediaPayload
	if err := json.Unmarshal(data, &media); err != nil {
		return nil, fmt.Errorf("unmarshal video change: %w", err)
	}

	event := &framework.VideoChangeEvent{
		CytubeEvent: base,
		VideoID:     media.ID,
		VideoType:   media.Type,
		Duration:    media.Duration.Int(),
		Title:       media.Title,
	}

	return event, nil
}

func (p *Parser) parseMediaUpdate(base framework.CytubeEvent, data json.RawMessage) (*framework.MediaUpdateEvent, error) {
	var update MediaUpdatePayload
	if err := json.Unmarshal(data, &update); err != nil {
		return nil, fmt.Errorf("unmarshal media update: %w", err)
	}

	event := &framework.MediaUpdateEvent{
		CytubeEvent: base,
		CurrentTime: update.CurrentTime,
		Paused:      update.Paused,
	}

	return event, nil
}

func (p *Parser) parseLogin(base framework.CytubeEvent, data json.RawMessage) (*framework.LoginEvent, error) {
	var login LoginPayload
	if err := json.Unmarshal(data, &login); err != nil {
		return nil, fmt.Errorf("unmarshal login: %w", err)
	}

	event := &framework.LoginEvent{
		CytubeEvent: base,
		Username:    login.Name,
		UserRank:    login.Rank,
		Success:     login.Success,
		FailReason:  login.Reason,
		IPAddress:   login.IP,
		Aliases:     login.Aliases,
	}

	// Add metadata
	base.Metadata["login_success"] = fmt.Sprintf("%t", login.Success)
	if login.Reason != "" {
		base.Metadata["fail_reason"] = login.Reason
	}

	return event, nil
}

func (p *Parser) parseAddUser(base framework.CytubeEvent, data json.RawMessage) (*framework.AddUserEvent, error) {
	var addUser AddUserPayload
	if err := json.Unmarshal(data, &addUser); err != nil {
		return nil, fmt.Errorf("unmarshal add user: %w", err)
	}

	event := &framework.AddUserEvent{
		CytubeEvent: base,
		Username:    addUser.Name,
		UserRank:    addUser.Rank,
		AddedBy:     addUser.AddedBy,
		Registered:  addUser.Registered,
		Email:       addUser.Email,
	}

	// Add metadata
	base.Metadata["registered"] = fmt.Sprintf("%t", addUser.Registered)
	if addUser.AddedBy != "" {
		base.Metadata["added_by"] = addUser.AddedBy
	}

	return event, nil
}

func (p *Parser) parseChannelMeta(base framework.CytubeEvent, data json.RawMessage) (*framework.ChannelMetaEvent, error) {
	var meta MetaPayload
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal meta: %w", err)
	}

	event := &framework.ChannelMetaEvent{
		CytubeEvent: base,
		Field:       meta.Field,
		OldValue:    meta.OldValue,
		NewValue:    meta.NewValue,
		ChangedBy:   meta.ChangedBy,
		ChangeType:  meta.ChangeType,
	}

	// Add metadata
	base.Metadata["field"] = meta.Field
	base.Metadata["change_type"] = meta.ChangeType
	base.Metadata["changed_by"] = meta.ChangedBy

	return event, nil
}

// PlaylistArrayItem represents a playlist item when sent as a direct array (initial load)
type PlaylistArrayItem struct {
	Media struct {
		ID       string          `json:"id"`
		Title    string          `json:"title"`
		Seconds  int             `json:"seconds"`
		Duration string          `json:"duration"`
		Type     string          `json:"type"`
		Meta     json.RawMessage `json:"meta"`
	} `json:"media"`
	UID     FlexibleUID `json:"uid"` // Can be string or number
	Temp    bool        `json:"temp"`
	QueueBy string      `json:"queueby"`
}

func (p *Parser) parsePlaylist(base framework.CytubeEvent, data json.RawMessage) (framework.Event, error) {
	// First, try to detect if it's an array or object
	trimmed := string(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		// It's an array - initial playlist load
		var items []PlaylistArrayItem
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("unmarshal playlist array: %w", err)
		}

		// Convert array items to PlaylistItem
		playlistItems := make([]framework.PlaylistItem, len(items))
		for i, item := range items {
			// Parse metadata if present
			var rawMetadata map[string]interface{}
			var mediaMetadata *framework.MediaMetadata

			if len(item.Media.Meta) > 0 {
				if err := json.Unmarshal(item.Media.Meta, &rawMetadata); err != nil {
					// Log error but continue processing
					logger.Warn("Cytube Parser", "Failed to parse metadata for item %s: %v", item.Media.ID, err)
				} else if rawMetadata != nil {
					// Convert raw metadata to MediaMetadata
					mediaMetadata = p.convertToMediaMetadata(rawMetadata)
				}
			}

			playlistItems[i] = framework.PlaylistItem{
				UID:       item.UID.String(), // Convert FlexibleUID to string
				Position:  i,
				MediaID:   item.Media.ID,
				MediaType: item.Media.Type,
				Title:     item.Media.Title,
				Duration:  item.Media.Seconds,
				QueuedBy:  item.QueueBy,
				Metadata:  mediaMetadata,
			}
		}

		event := &framework.PlaylistArrayEvent{
			CytubeEvent: base,
			Items:       playlistItems,
		}

		// Add metadata
		base.Metadata["event_subtype"] = "array"
		base.Metadata["item_count"] = fmt.Sprintf("%d", len(items))

		return event, nil
	}

	// It's an object - playlist modification
	var playlist PlaylistPayload
	if err := json.Unmarshal(data, &playlist); err != nil {
		return nil, fmt.Errorf("unmarshal playlist: %w", err)
	}

	// Convert PlaylistItem to QueueItem
	queueItems := make([]framework.QueueItem, len(playlist.Items))
	for i, item := range playlist.Items {
		queueItems[i] = framework.QueueItem{
			Position:  playlist.Position + i, // Calculate position for each item
			MediaID:   item.MediaID,
			MediaType: item.Type,
			Title:     item.Title,
			Duration:  item.Duration,
			QueuedBy:  item.AddedBy,
			QueuedAt:  time.Now().Unix(),
		}
	}

	event := &framework.PlaylistEvent{
		CytubeEvent: base,
		Action:      playlist.Action,
		Items:       queueItems,
		Position:    playlist.Position,
		FromPos:     playlist.From,
		ToPos:       playlist.To,
		User:        playlist.User,
		ItemCount:   len(playlist.Items),
	}

	// Add metadata
	base.Metadata["action"] = playlist.Action
	base.Metadata["user"] = playlist.User
	base.Metadata["item_count"] = fmt.Sprintf("%d", len(playlist.Items))

	// For single item actions, add item details
	if len(playlist.Items) == 1 {
		item := playlist.Items[0]
		base.Metadata["media_title"] = item.Title
		base.Metadata["media_type"] = item.Type
		base.Metadata["added_by"] = item.AddedBy
	}

	return event, nil
}

func (p *Parser) parseGenericEvent(base framework.CytubeEvent, eventType string, data json.RawMessage) (*framework.GenericEvent, error) {
	event := &framework.GenericEvent{
		CytubeEvent: base,
		UnknownType: eventType,
		RawJSON:     data,
	}

	// Attempt to parse as generic JSON object
	var rawData map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawData); err == nil {
		// Convert to map[string]FieldValue for framework compatibility
		parsedMap := make(map[string]framework.FieldValue)

		for key, value := range rawData {
			fieldVal := framework.FieldValue{}

			// Try to parse as string
			var strVal string
			if err := json.Unmarshal(value, &strVal); err == nil {
				fieldVal.String = strVal
				base.Metadata[key] = strVal
			} else {
				// Try to parse as number
				var numVal float64
				if err := json.Unmarshal(value, &numVal); err == nil {
					fieldVal.Number = numVal
					base.Metadata[key] = fmt.Sprintf("%v", numVal)
				} else {
					// Try to parse as boolean
					var boolVal bool
					if err := json.Unmarshal(value, &boolVal); err == nil {
						fieldVal.Boolean = boolVal
						base.Metadata[key] = fmt.Sprintf("%v", boolVal)
					} else if string(value) == "null" {
						fieldVal.IsNull = true
						base.Metadata[key] = "null"
					}
				}
			}

			parsedMap[key] = fieldVal
		}

		event.ParsedData = parsedMap
	}

	// Always store event type
	base.Metadata["event_type"] = eventType

	return event, nil
}

func (p *Parser) parseSetPlaylistMeta(base framework.CytubeEvent, data json.RawMessage) (*framework.SetPlaylistMetaEvent, error) {
	var meta SetPlaylistMetaPayload
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal setPlaylistMeta: %w", err)
	}

	event := &framework.SetPlaylistMetaEvent{
		CytubeEvent:   base,
		Count:         meta.Count,
		RawTime:       meta.RawTime,
		FormattedTime: meta.Time,
	}

	// Add metadata
	base.Metadata["playlist_count"] = fmt.Sprintf("%d", meta.Count)
	base.Metadata["playlist_time"] = meta.Time
	base.Metadata["playlist_seconds"] = fmt.Sprintf("%d", meta.RawTime)

	return event, nil
}

func (p *Parser) parseUserCount(base framework.CytubeEvent, data json.RawMessage) (*framework.UserCountEvent, error) {
	var count int
	if err := json.Unmarshal(data, &count); err != nil {
		return nil, fmt.Errorf("unmarshal usercount: %w", err)
	}

	event := &framework.UserCountEvent{
		CytubeEvent: base,
		Count:       count,
	}

	// Add metadata
	base.Metadata["user_count"] = fmt.Sprintf("%d", count)

	return event, nil
}

func (p *Parser) parseSetUserRank(base framework.CytubeEvent, data json.RawMessage) (*framework.SetUserRankEvent, error) {
	var rank SetUserRankPayload
	if err := json.Unmarshal(data, &rank); err != nil {
		return nil, fmt.Errorf("unmarshal setUserRank: %w", err)
	}

	event := &framework.SetUserRankEvent{
		CytubeEvent: base,
		Username:    rank.Name,
		Rank:        rank.Rank,
	}

	// Add metadata
	base.Metadata["username"] = rank.Name
	base.Metadata["rank"] = fmt.Sprintf("%d", rank.Rank)

	return event, nil
}

func (p *Parser) parseSetPlaylistLocked(base framework.CytubeEvent, data json.RawMessage) (*framework.SetPlaylistLockedEvent, error) {
	var locked bool
	if err := json.Unmarshal(data, &locked); err != nil {
		return nil, fmt.Errorf("unmarshal setPlaylistLocked: %w", err)
	}

	event := &framework.SetPlaylistLockedEvent{
		CytubeEvent: base,
		Locked:      locked,
	}

	// Add metadata
	base.Metadata["playlist_locked"] = fmt.Sprintf("%t", locked)

	return event, nil
}

func (p *Parser) parseSetPermissions(base framework.CytubeEvent, data json.RawMessage) (*framework.SetPermissionsEvent, error) {
	var perms SetPermissionsPayload
	if err := json.Unmarshal(data, &perms); err != nil {
		return nil, fmt.Errorf("unmarshal setPermissions: %w", err)
	}

	// Convert struct to map for easier access
	permMap := make(map[string]float64)
	permMap["seeplaylist"] = perms.Seeplaylist
	permMap["playlistadd"] = perms.Playlistadd
	permMap["playlistnext"] = perms.Playlistnext
	permMap["playlistmove"] = perms.Playlistmove
	permMap["playlistdelete"] = perms.Playlistdelete
	permMap["playlistjump"] = perms.Playlistjump
	permMap["playlistaddlist"] = perms.Playlistaddlist
	permMap["oplaylistadd"] = perms.Oplaylistadd
	permMap["oplaylistnext"] = perms.Oplaylistnext
	permMap["oplaylistmove"] = perms.Oplaylistmove
	permMap["oplaylistdelete"] = perms.Oplaylistdelete
	permMap["oplaylistjump"] = perms.Oplaylistjump
	permMap["oplaylistaddlist"] = perms.Oplaylistaddlist
	permMap["playlistaddcustom"] = perms.Playlistaddcustom
	permMap["playlistaddrawfile"] = perms.Playlistaddrawfile
	permMap["playlistaddlive"] = perms.Playlistaddlive
	permMap["exceedmaxlength"] = perms.Exceedmaxlength
	permMap["addnontemp"] = perms.Addnontemp
	permMap["settemp"] = perms.Settemp
	permMap["playlistshuffle"] = perms.Playlistshuffle
	permMap["playlistclear"] = perms.Playlistclear
	permMap["pollctl"] = perms.Pollctl
	permMap["pollvote"] = perms.Pollvote
	permMap["viewhiddenpoll"] = perms.Viewhiddenpoll
	permMap["voteskip"] = perms.Voteskip
	permMap["viewvoteskip"] = perms.Viewvoteskip
	permMap["mute"] = perms.Mute
	permMap["kick"] = perms.Kick
	permMap["ban"] = perms.Ban
	permMap["motdedit"] = perms.Motdedit
	permMap["filteredit"] = perms.Filteredit
	permMap["filterimport"] = perms.Filterimport
	permMap["emoteedit"] = perms.Emoteedit
	permMap["emoteimport"] = perms.Emoteimport
	permMap["playlistlock"] = perms.Playlistlock
	permMap["leaderctl"] = perms.Leaderctl
	permMap["drink"] = perms.Drink
	permMap["chat"] = perms.Chat
	permMap["chatclear"] = perms.Chatclear
	permMap["exceedmaxitems"] = perms.Exceedmaxitems
	permMap["deletefromchannellib"] = perms.Deletefromchannellib
	permMap["exceedmaxdurationperuser"] = perms.Exceedmaxdurationperuser

	event := &framework.SetPermissionsEvent{
		CytubeEvent: base,
		Permissions: permMap,
	}

	// Add some key permissions to metadata
	base.Metadata["chat_permission"] = fmt.Sprintf("%.1f", perms.Chat)
	base.Metadata["playlist_add_permission"] = fmt.Sprintf("%.1f", perms.Playlistadd)

	return event, nil
}

func (p *Parser) parseSetMotd(base framework.CytubeEvent, data json.RawMessage) (*framework.SetMotdEvent, error) {
	var motd string
	if err := json.Unmarshal(data, &motd); err != nil {
		return nil, fmt.Errorf("unmarshal setMotd: %w", err)
	}

	event := &framework.SetMotdEvent{
		CytubeEvent: base,
		Motd:        motd,
	}

	// Add metadata
	base.Metadata["motd_length"] = fmt.Sprintf("%d", len(motd))
	if motd != "" {
		base.Metadata["has_motd"] = "true"
	} else {
		base.Metadata["has_motd"] = "false"
	}

	return event, nil
}

func (p *Parser) parseChannelOpts(base framework.CytubeEvent, data json.RawMessage) (*framework.ChannelOptsEvent, error) {
	var opts ChannelOptsPayload
	if err := json.Unmarshal(data, &opts); err != nil {
		return nil, fmt.Errorf("unmarshal channelOpts: %w", err)
	}

	// Convert struct to map for the event
	optsMap := make(map[string]framework.ChannelOption)

	// Boolean options
	optsMap["allow_voteskip"] = framework.ChannelOption{
		BoolValue: opts.AllowVoteskip,
		ValueType: "bool",
	}
	optsMap["chat_antiflood"] = framework.ChannelOption{
		BoolValue: opts.ChatAntiflood,
		ValueType: "bool",
	}
	optsMap["show_public"] = framework.ChannelOption{
		BoolValue: opts.ShowPublic,
		ValueType: "bool",
	}
	optsMap["enable_link_regex"] = framework.ChannelOption{
		BoolValue: opts.EnableLinkRegex,
		ValueType: "bool",
	}
	optsMap["password"] = framework.ChannelOption{
		BoolValue: opts.Password,
		ValueType: "bool",
	}
	optsMap["allow_dupes"] = framework.ChannelOption{
		BoolValue: opts.AllowDupes,
		ValueType: "bool",
	}
	optsMap["torbanned"] = framework.ChannelOption{
		BoolValue: opts.Torbanned,
		ValueType: "bool",
	}
	optsMap["block_anonymous_users"] = framework.ChannelOption{
		BoolValue: opts.BlockAnonymousUsers,
		ValueType: "bool",
	}
	optsMap["allow_ascii_control"] = framework.ChannelOption{
		BoolValue: opts.AllowAsciiControl,
		ValueType: "bool",
	}

	// Float options
	optsMap["voteskip_ratio"] = framework.ChannelOption{
		FloatValue: opts.VoteskipRatio,
		ValueType:  "float",
	}

	// Integer options
	optsMap["afk_timeout"] = framework.ChannelOption{
		IntValue:  opts.AfkTimeout,
		ValueType: "int",
	}
	optsMap["maxlength"] = framework.ChannelOption{
		IntValue:  opts.Maxlength,
		ValueType: "int",
	}
	optsMap["playlist_max_per_user"] = framework.ChannelOption{
		IntValue:  opts.PlaylistMaxPerUser,
		ValueType: "int",
	}
	optsMap["new_user_chat_delay"] = framework.ChannelOption{
		IntValue:  opts.NewUserChatDelay,
		ValueType: "int",
	}
	optsMap["new_user_chat_link_delay"] = framework.ChannelOption{
		IntValue:  opts.NewUserChatLinkDelay,
		ValueType: "int",
	}
	optsMap["playlist_max_duration_per_user"] = framework.ChannelOption{
		IntValue:  opts.PlaylistMaxDurationPerUser,
		ValueType: "int",
	}

	// String options
	optsMap["pagetitle"] = framework.ChannelOption{
		StringValue: opts.Pagetitle,
		ValueType:   "string",
	}
	optsMap["externalcss"] = framework.ChannelOption{
		StringValue: opts.ExternalCSS,
		ValueType:   "string",
	}
	optsMap["externaljs"] = framework.ChannelOption{
		StringValue: opts.ExternalJS,
		ValueType:   "string",
	}

	// Special case: AntifloodParams is a struct, store as JSON string
	antifloodJSON, _ := json.Marshal(opts.ChatAntifloodParams)
	optsMap["chat_antiflood_params"] = framework.ChannelOption{
		StringValue: string(antifloodJSON),
		ValueType:   "string",
	}

	event := &framework.ChannelOptsEvent{
		CytubeEvent: base,
		Options:     optsMap,
	}

	// Add key options to metadata
	base.Metadata["pagetitle"] = opts.Pagetitle
	base.Metadata["allow_voteskip"] = fmt.Sprintf("%t", opts.AllowVoteskip)
	base.Metadata["chat_antiflood"] = fmt.Sprintf("%t", opts.ChatAntiflood)
	base.Metadata["show_public"] = fmt.Sprintf("%t", opts.ShowPublic)

	return event, nil
}

func (p *Parser) parseChannelCSSJS(base framework.CytubeEvent, data json.RawMessage) (*framework.ChannelCSSJSEvent, error) {
	var cssjs ChannelCSSJSPayload
	if err := json.Unmarshal(data, &cssjs); err != nil {
		return nil, fmt.Errorf("unmarshal channelCSSJS: %w", err)
	}

	event := &framework.ChannelCSSJSEvent{
		CytubeEvent: base,
		CSS:         cssjs.CSS,
		CSSHash:     cssjs.CSSHash,
		JS:          cssjs.JS,
		JSHash:      cssjs.JSHash,
	}

	// Add metadata
	base.Metadata["css_hash"] = cssjs.CSSHash
	base.Metadata["js_hash"] = cssjs.JSHash
	base.Metadata["has_css"] = fmt.Sprintf("%t", cssjs.CSS != "")
	base.Metadata["has_js"] = fmt.Sprintf("%t", cssjs.JS != "")

	return event, nil
}

func (p *Parser) parseSetUserMeta(base framework.CytubeEvent, data json.RawMessage) (*framework.SetUserMetaEvent, error) {
	var meta SetUserMetaPayload
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal setUserMeta: %w", err)
	}

	event := &framework.SetUserMetaEvent{
		CytubeEvent: base,
		Username:    meta.Name,
		AFK:         meta.Meta.AFK,
		Muted:       meta.Meta.Muted,
	}

	// Add metadata
	base.Metadata["username"] = meta.Name
	base.Metadata["afk"] = fmt.Sprintf("%t", meta.Meta.AFK)
	base.Metadata["muted"] = fmt.Sprintf("%t", meta.Meta.Muted)

	return event, nil
}

func (p *Parser) parseSetAFK(base framework.CytubeEvent, data json.RawMessage) (*framework.SetAFKEvent, error) {
	var afkData SetAFKPayload
	if err := json.Unmarshal(data, &afkData); err != nil {
		return nil, fmt.Errorf("unmarshal setAFK: %w", err)
	}

	event := &framework.SetAFKEvent{
		CytubeEvent: base,
		Username:    afkData.Name,
		AFK:         afkData.AFK,
	}

	// Add metadata
	base.Metadata["username"] = afkData.Name
	base.Metadata["afk"] = fmt.Sprintf("%t", afkData.AFK)

	return event, nil
}

func (p *Parser) parseClearVoteskipVote(base framework.CytubeEvent, data json.RawMessage) (*framework.ClearVoteskipVoteEvent, error) {
	// This event usually has no data
	event := &framework.ClearVoteskipVoteEvent{
		CytubeEvent: base,
	}

	// Add metadata
	base.Metadata["action"] = "clear_voteskip"

	return event, nil
}

func (p *Parser) parsePM(base framework.CytubeEvent, data json.RawMessage) (*framework.PrivateMessageEvent, error) {
	var pm PrivateMessagePayload
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, fmt.Errorf("unmarshal pm: %w", err)
	}

	event := &framework.PrivateMessageEvent{
		CytubeEvent: base,
		FromUser:    pm.Username,
		ToUser:      pm.To,
		Message:     pm.Msg,
		MessageTime: pm.Time,
	}

	// Add metadata
	base.Metadata["from_user"] = pm.Username
	base.Metadata["to_user"] = pm.To
	base.Metadata["message_length"] = fmt.Sprintf("%d", len(pm.Msg))
	base.Metadata["timestamp"] = fmt.Sprintf("%d", pm.Time)

	return event, nil
}

func (p *Parser) parseQueueEvent(base framework.CytubeEvent, data json.RawMessage) (framework.Event, error) {
	// Queue events can be similar to playlist events
	// Try to parse as PlaylistPayload first
	var queue PlaylistPayload
	if err := json.Unmarshal(data, &queue); err != nil {
		// If that fails, log and return generic event
		logger.Warn("Cytube Parser", "Failed to parse queue event as PlaylistPayload: %v", err)
		return p.parseGenericEvent(base, "queue", data)
	}

	// Convert to playlist event format
	queueItems := make([]framework.QueueItem, len(queue.Items))
	for i, item := range queue.Items {
		queueItems[i] = framework.QueueItem{
			Position:  queue.Position + i,
			MediaID:   item.MediaID,
			MediaType: item.Type,
			Title:     item.Title,
			Duration:  item.Duration,
			QueuedBy:  item.AddedBy,
			QueuedAt:  time.Now().Unix(),
		}
	}

	event := &framework.PlaylistEvent{
		CytubeEvent: base,
		Action:      "queue", // Mark as queue action
		Items:       queueItems,
		Position:    queue.Position,
		User:        queue.User,
		ItemCount:   len(queue.Items),
	}

	// Add metadata
	base.Metadata["action"] = "queue"
	base.Metadata["user"] = queue.User
	base.Metadata["item_count"] = fmt.Sprintf("%d", len(queue.Items))

	return event, nil
}

func (p *Parser) parseDeleteEvent(base framework.CytubeEvent, data json.RawMessage) (framework.Event, error) {
	// Delete events typically contain position/uid of deleted item
	var deleteData struct {
		Position int         `json:"position"`
		UID      FlexibleUID `json:"uid"`
	}

	if err := json.Unmarshal(data, &deleteData); err != nil {
		logger.Warn("Cytube Parser", "Failed to parse delete event: %v", err)
		return p.parseGenericEvent(base, "delete", data)
	}

	event := &framework.PlaylistEvent{
		CytubeEvent: base,
		Action:      "delete",
		Position:    deleteData.Position,
		ItemCount:   0, // No items in delete
	}

	// Add metadata
	base.Metadata["action"] = "delete"
	base.Metadata["position"] = fmt.Sprintf("%d", deleteData.Position)
	// FlexibleUID has a String() method that handles both string and numeric values
	base.Metadata["uid"] = deleteData.UID.String()

	return event, nil
}

func (p *Parser) parseMoveVideoEvent(base framework.CytubeEvent, data json.RawMessage) (framework.Event, error) {
	// Move events contain from and to positions
	var moveData struct {
		From int `json:"from"`
		To   int `json:"to"`
	}

	if err := json.Unmarshal(data, &moveData); err != nil {
		logger.Warn("Cytube Parser", "Failed to parse moveVideo event: %v", err)
		return p.parseGenericEvent(base, "moveVideo", data)
	}

	event := &framework.PlaylistEvent{
		CytubeEvent: base,
		Action:      "move",
		FromPos:     moveData.From,
		ToPos:       moveData.To,
		ItemCount:   0, // No items in move
	}

	// Add metadata
	base.Metadata["action"] = "move"
	base.Metadata["from_pos"] = fmt.Sprintf("%d", moveData.From)
	base.Metadata["to_pos"] = fmt.Sprintf("%d", moveData.To)

	return event, nil
}

func (p *Parser) parseQueueFailEvent(base framework.CytubeEvent, data json.RawMessage) (framework.Event, error) {
	// Queue fail events contain error information
	var failData struct {
		Message string `json:"msg"`
		Link    string `json:"link"`
		ID      string `json:"id"`
	}

	if err := json.Unmarshal(data, &failData); err != nil {
		// Try simple string message
		var msg string
		if err2 := json.Unmarshal(data, &msg); err2 == nil {
			base.Metadata["error_message"] = msg
			return &base, nil
		}
		logger.Warn("Cytube Parser", "Failed to parse queueFail event: %v", err)
		return p.parseGenericEvent(base, "queueFail", data)
	}

	// Add metadata
	base.Metadata["error_message"] = failData.Message
	if failData.Link != "" {
		base.Metadata["failed_link"] = failData.Link
	}
	if failData.ID != "" {
		base.Metadata["failed_id"] = failData.ID
	}

	return &base, nil
}

// convertToMediaMetadata converts raw metadata map to structured MediaMetadata
func (p *Parser) convertToMediaMetadata(raw map[string]interface{}) *framework.MediaMetadata {
	if raw == nil {
		return nil
	}

	metadata := &framework.MediaMetadata{
		Extra: make(map[string]interface{}),
	}

	// Helper function to safely get string value
	getString := func(key string) string {
		if val, ok := raw[key]; ok {
			if str, ok := val.(string); ok {
				return str
			}
		}
		return ""
	}

	// Helper function to safely get int64 value
	getInt64 := func(key string) int64 {
		if val, ok := raw[key]; ok {
			switch v := val.(type) {
			case float64:
				return int64(v)
			case int64:
				return v
			case int:
				return int64(v)
			}
		}
		return 0
	}

	// Helper function to safely get int value
	getInt := func(key string) int {
		return int(getInt64(key))
	}

	// Common fields
	metadata.Description = getString("description")
	metadata.ThumbnailURL = getString("thumbnail")
	metadata.UploadDate = getString("upload_date")

	// Video-specific fields
	metadata.ChannelName = getString("channel")
	metadata.ChannelID = getString("channel_id")
	metadata.ViewCount = getInt64("view_count")
	metadata.LikeCount = getInt64("like_count")
	metadata.DislikeCount = getInt64("dislike_count")

	// Audio/Music-specific fields
	metadata.Artist = getString("artist")
	metadata.Album = getString("album")
	metadata.Genre = getString("genre")
	metadata.TrackNumber = getInt("track_number")
	metadata.Year = getInt("year")

	// Handle tags array
	if tagsVal, ok := raw["tags"]; ok {
		switch tags := tagsVal.(type) {
		case []interface{}:
			metadata.Tags = make([]string, 0, len(tags))
			for _, tag := range tags {
				if str, ok := tag.(string); ok {
					metadata.Tags = append(metadata.Tags, str)
				}
			}
		case []string:
			metadata.Tags = tags
		}
	}

	// Store any remaining fields in Extra
	knownFields := map[string]bool{
		"description": true, "thumbnail": true, "upload_date": true,
		"channel": true, "channel_id": true, "view_count": true,
		"like_count": true, "dislike_count": true, "artist": true,
		"album": true, "genre": true, "track_number": true,
		"year": true, "tags": true,
	}

	for key, value := range raw {
		if !knownFields[key] {
			metadata.Extra[key] = value
		}
	}

	// Only return metadata if it has some content
	hasContent := metadata.Description != "" || metadata.ThumbnailURL != "" ||
		metadata.ChannelName != "" || metadata.Artist != "" ||
		len(metadata.Tags) > 0 || len(metadata.Extra) > 0

	if !hasContent {
		return nil
	}

	return metadata
}

// parseUserListEvent parses the userlist event containing all users in the channel
func (p *Parser) parseUserListEvent(base framework.CytubeEvent, data json.RawMessage) (framework.Event, error) {
	// Return a generic event - the room manager will process this specially
	return &framework.GenericEvent{
		CytubeEvent: base,
		UnknownType: "userlist",
		RawJSON:     data,
		ParsedData:  make(map[string]framework.FieldValue),
	}, nil
}
