package eventbus

// Event type constants for consistent routing
const (
	// Cytube events
	EventCytubeChatMsg     = "cytube.event.chatMsg"
	EventCytubePM          = "cytube.event.pm"
	EventCytubeUserJoin    = "cytube.event.userJoin"
	EventCytubeUserLeave   = "cytube.event.userLeave"
	EventCytubeVideoChange = "cytube.event.changeMedia"
	EventCytubeMediaUpdate = "cytube.mediaUpdate"
	EventCytubeConnect     = "cytube.event.connect"
	EventCytubeDisconnect  = "cytube.event.disconnect"

	// SQL events
	EventSQLRequest  = "sql.request"
	EventSQLResponse = "sql.response"
	EventSQLExec     = "sql.exec"
	EventSQLQuery    = "sql.query"

	// Plugin communication events
	EventPluginRequest  = "plugin.request"
	EventPluginResponse = "plugin.response"

	// Plugin-specific event prefixes
	EventPluginCommand = "plugin.command"
	EventPluginFilter  = "plugin.filter"
)

// GetEventTypePrefix extracts the prefix from an event type
// e.g., "cytube.event.chatMsg" -> "cytube.event"
func GetEventTypePrefix(eventType string) string {
	// Find the last dot
	lastDot := -1
	for i := len(eventType) - 1; i >= 0; i-- {
		if eventType[i] == '.' {
			lastDot = i
			break
		}
	}

	if lastDot > 0 {
		return eventType[:lastDot]
	}
	return eventType
}
