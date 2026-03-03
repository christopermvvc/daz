# pissing_contest/piss command

Challenge another user to a public contest with optional stake.

Config (optional)
- `challenge_duration_seconds` (default: `30`)
- `cooldown_minutes` (default: `5`)
- `bot_username` (default: empty)
  - if set, prevents challenging the configured bot user.

Commands
- `!piss <amount> <user>` to wager coins
- `!piss <user>` for bragging rights
- aliases: `!pissing_contest`, `!pissingcontest`

Notes
- Integrates with the economy API via `economy.GetBalance`, `economy.Transfer`, and `economy.Debit`.
- Uses SQL-backed tables from migration `032_user_state_foundation.sql`.
- Gameplay elements are dictionary-driven in these files:
  - `characteristics.go`
  - `conditions.go`
  - `locations.go`
  - `weather.go`
  - `dialogues.go`
