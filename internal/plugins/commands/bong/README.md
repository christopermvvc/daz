# bong command

Rip a cone and track daily per-channel counts.

Config (optional)
- `cooldown_seconds` (default: 300)
- `cooldown_message` (default: "easy on the cones mate, ya lungs need {time}s to recover from that last rip")
- `max_runes` (default: 500)

Notes
- Cooldown is per user per channel.
- Daily counts are per channel (based on current date).
- Uses `daz_bong_sessions` from the legacy user-state migration.
