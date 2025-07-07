package cytube

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type Parser struct {
	channel string
}

func NewParser(channel string) *Parser {
	return &Parser{
		channel: channel,
	}
}

func (p *Parser) ParseEvent(event Event) (framework.Event, error) {
	timestamp := time.Now()

	baseEvent := framework.CytubeEvent{
		EventType:   event.Type,
		EventTime:   timestamp,
		ChannelName: p.channel,
		RawData:     event.Data,
		Metadata:    make(map[string]string),
	}

	switch event.Type {
	case "chatMsg":
		return p.parseChatMessage(baseEvent, event.Data)
	case "userJoin":
		return p.parseUserJoin(baseEvent, event.Data)
	case "userLeave":
		return p.parseUserLeave(baseEvent, event.Data)
	case "changeMedia":
		return p.parseVideoChange(baseEvent, event.Data)
	default:
		return &baseEvent, nil
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
		Duration:    media.Duration,
		Title:       media.Title,
	}

	return event, nil
}
