# Daz prerequisites (Ubuntu)

This is a quick checklist for setting up Daz on a fresh Ubuntu host.

## System packages
- PostgreSQL server + client
- Go toolchain (for building `bin/daz`)
- Build tools
- Optional: `fortune-mod` if you want the fortune command

Install:
```bash
sudo apt-get update
sudo apt-get install -y postgresql postgresql-contrib golang-go build-essential
# Optional:
sudo apt-get install -y fortune-mod
```

## PostgreSQL bootstrap
Create a dedicated role and database (example uses `daz`/`daz_local_dev`):
```bash
sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='daz'" | grep -q 1 || \
  sudo -u postgres psql -c "CREATE ROLE daz LOGIN PASSWORD 'daz_local_dev';"

sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='daz'" | grep -q 1 || \
  sudo -u postgres createdb -O daz daz

# Role expected by gallery migration (NOLOGIN):
sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='daz_user'" | grep -q 1 || \
  sudo -u postgres psql -c "CREATE ROLE daz_user NOLOGIN;"
```

## Config file
Create `config.json` in the repo root (multi-room uses `core.rooms`):
```json
{
  "core": {
    "rooms": [
      {
        "id": "always_always_sunny",
        "channel": "always_always_sunny",
        "username": "dazza",
        "password": "sammy2",
        "enabled": true,
        "reconnect_attempts": 10,
        "cooldown_minutes": 30
      }
    ],
    "database": {
      "host": "localhost",
      "port": 5432,
      "database": "daz",
      "user": "daz",
      "password": "daz_local_dev"
    }
  },
  "plugins": {
    "sql": {
      "database": {
        "host": "localhost",
        "port": 5432,
        "database": "daz",
        "user": "daz",
        "password": "daz_local_dev",
        "max_connections": 10,
        "max_conn_lifetime": 3600,
        "connect_timeout": 30
      },
      "logger_rules": [
        {
          "event_pattern": "cytube.event.*",
          "enabled": true,
          "table": "daz_core_events"
        }
      ]
    },
    "eventfilter": {
      "command_prefix": "!",
      "default_cooldown": 5,
      "admin_users": []
    }
  }
}
```

## SQL migrations (required)
Run all SQL migrations once:
```bash
for f in /path/to/daz/scripts/sql/*.sql; do
  PGPASSWORD="daz_local_dev" psql -h localhost -U daz -d daz -f "$f"
done
```

## Build and run
```bash
cd /path/to/daz
./scripts/build-daz.sh
./bin/daz -config ./config.json
```

## Optional services
- Ollama: only required if you enable the Ollama plugin; run on `localhost:11434`.
