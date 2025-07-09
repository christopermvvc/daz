# Plan A: Environment Variable Approach for Credential Security

## Overview
This document details the implementation plan for securing credentials in the daz project using environment variables. This approach will completely remove all hardcoded credentials from the codebase, configuration files, and scripts.

## Current Security Issues

### 1. Credential Exposure Points
- **config.json**: Contains plaintext credentials (not in .gitignore)
- **config.json.example**: Contains real credentials instead of placeholders
- **scripts/run-console.sh**: Hardcoded credentials in command line arguments
- **scripts/run.sh**: Hardcoded credentials in command line arguments
- **internal/config/config.go**: Default credentials in source code

### 2. Exposed Credentials
- CyTube: username="***REMOVED***", password="***REMOVED***"
- Database: user="***REMOVED***", password="***REMOVED***", database="daz"

## Implementation Steps

### Step 1: Update .gitignore
Add config.json to prevent accidental commits:
```bash
# Add to .gitignore
config.json
```

### Step 2: Update config.json.example
Replace all real credentials with clear placeholders:
```json
{
  "cytube": {
    "username": "YOUR_CYTUBE_USERNAME",
    "password": "YOUR_CYTUBE_PASSWORD",
    "channel": "YOUR_CHANNEL_NAME",
    "server": "cytu.be:443"
  },
  "database": {
    "type": "sqlite",
    "sqlite": {
      "path": "./daz.db"
    },
    "mysql": {
      "host": "localhost",
      "port": 3306,
      "user": "YOUR_DB_USERNAME",
      "password": "YOUR_DB_PASSWORD",
      "database": "YOUR_DB_NAME"
    }
  },
  "bot": {
    "command_prefix": "!",
    "response_delay_ms": 500,
    "rate_limit": {
      "messages_per_minute": 30,
      "cooldown_seconds": 60
    }
  },
  "logging": {
    "level": "info",
    "format": "text"
  }
}
```

### Step 3: Update Shell Scripts

#### run-console.sh
```bash
#!/bin/bash

# Source environment variables if .env file exists
if [ -f .env ]; then
    export $(cat .env | grep -v '^#' | xargs)
fi

# Validate required environment variables
if [ -z "$DAZ_CYTUBE_USERNAME" ]; then
    echo "Error: DAZ_CYTUBE_USERNAME environment variable is not set"
    exit 1
fi

if [ -z "$DAZ_CYTUBE_PASSWORD" ]; then
    echo "Error: DAZ_CYTUBE_PASSWORD environment variable is not set"
    exit 1
fi

if [ -z "$DAZ_CYTUBE_CHANNEL" ]; then
    echo "Error: DAZ_CYTUBE_CHANNEL environment variable is not set"
    exit 1
fi

# Build the daz binary if it doesn't exist
if [ ! -f ./bin/daz ]; then
    echo "Building daz binary..."
    ./scripts/build-daz.sh
fi

# Run daz with environment variables
./bin/daz \
    -channel "$DAZ_CYTUBE_CHANNEL" \
    -username "$DAZ_CYTUBE_USERNAME" \
    -password "$DAZ_CYTUBE_PASSWORD" \
    -db-type "${DAZ_DB_TYPE:-sqlite}" \
    -db-path "${DAZ_DB_PATH:-./daz.db}" \
    -db-host "${DAZ_DB_HOST:-localhost}" \
    -db-port "${DAZ_DB_PORT:-3306}" \
    -db-user "$DAZ_DB_USER" \
    -db-pass "$DAZ_DB_PASSWORD" \
    -db-name "${DAZ_DB_NAME:-daz}"
```

#### run.sh
Similar updates with environment variable usage and validation.

### Step 4: Update internal/config/config.go

Remove all hardcoded defaults and require credentials from environment or config:

```go
package config

import (
    "encoding/json"
    "fmt"
    "os"
)

// DefaultConfig returns a config with safe defaults (no credentials)
func DefaultConfig() *Config {
    return &Config{
        CyTube: CyTubeConfig{
            Server: "cytu.be:443",
            // No default credentials
            Username: "",
            Password: "",
            Channel:  "",
        },
        Database: DatabaseConfig{
            Type: "sqlite",
            SQLite: SQLiteConfig{
                Path: "./daz.db",
            },
            MySQL: MySQLConfig{
                Host:     "localhost",
                Port:     3306,
                // No default credentials
                User:     "",
                Password: "",
                Database: "",
            },
        },
        Bot: BotConfig{
            CommandPrefix:   "!",
            ResponseDelayMS: 500,
            RateLimit: RateLimitConfig{
                MessagesPerMinute: 30,
                CooldownSeconds:   60,
            },
        },
        Logging: LoggingConfig{
            Level:  "info",
            Format: "text",
        },
    }
}

// LoadConfig loads configuration with environment variable overrides
func LoadConfig(path string) (*Config, error) {
    cfg := DefaultConfig()
    
    // Load from file if it exists
    if path != "" {
        data, err := os.ReadFile(path)
        if err != nil && !os.IsNotExist(err) {
            return nil, fmt.Errorf("reading config: %w", err)
        }
        if err == nil {
            if err := json.Unmarshal(data, cfg); err != nil {
                return nil, fmt.Errorf("parsing config: %w", err)
            }
        }
    }
    
    // Override with environment variables
    if v := os.Getenv("DAZ_CYTUBE_USERNAME"); v != "" {
        cfg.CyTube.Username = v
    }
    if v := os.Getenv("DAZ_CYTUBE_PASSWORD"); v != "" {
        cfg.CyTube.Password = v
    }
    if v := os.Getenv("DAZ_CYTUBE_CHANNEL"); v != "" {
        cfg.CyTube.Channel = v
    }
    if v := os.Getenv("DAZ_DB_USER"); v != "" {
        cfg.Database.MySQL.User = v
    }
    if v := os.Getenv("DAZ_DB_PASSWORD"); v != "" {
        cfg.Database.MySQL.Password = v
    }
    if v := os.Getenv("DAZ_DB_NAME"); v != "" {
        cfg.Database.MySQL.Database = v
    }
    
    // Validate required fields
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }
    
    return cfg, nil
}

// Validate ensures required credentials are provided
func (c *Config) Validate() error {
    if c.CyTube.Username == "" {
        return fmt.Errorf("CyTube username is required")
    }
    if c.CyTube.Password == "" {
        return fmt.Errorf("CyTube password is required")
    }
    if c.CyTube.Channel == "" {
        return fmt.Errorf("CyTube channel is required")
    }
    
    if c.Database.Type == "mysql" {
        if c.Database.MySQL.User == "" {
            return fmt.Errorf("MySQL user is required")
        }
        if c.Database.MySQL.Password == "" {
            return fmt.Errorf("MySQL password is required")
        }
    }
    
    return nil
}
```

### Step 5: Update Command Line Flag Handling

Modify main.go to handle environment variables properly:

```go
// In main.go or wherever flags are parsed
func parseFlags(cfg *config.Config) {
    // Use environment variables as defaults for flags
    flag.StringVar(&cfg.CyTube.Username, "username", 
        getEnvOrDefault("DAZ_CYTUBE_USERNAME", cfg.CyTube.Username), 
        "CyTube username")
    flag.StringVar(&cfg.CyTube.Password, "password", 
        getEnvOrDefault("DAZ_CYTUBE_PASSWORD", cfg.CyTube.Password), 
        "CyTube password")
    // ... etc for other flags
}

func getEnvOrDefault(key, defaultValue string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return defaultValue
}
```

### Step 6: Create .env.example

Create a template for developers:

```bash
# CyTube Credentials
DAZ_CYTUBE_USERNAME=your_username
DAZ_CYTUBE_PASSWORD=your_password
DAZ_CYTUBE_CHANNEL=your_channel

# Database Credentials (for MySQL)
DAZ_DB_TYPE=sqlite
DAZ_DB_USER=your_db_user
DAZ_DB_PASSWORD=your_db_password
DAZ_DB_NAME=daz
DAZ_DB_HOST=localhost
DAZ_DB_PORT=3306

# SQLite Path (when using SQLite)
DAZ_DB_PATH=./daz.db
```

### Step 7: Update Documentation

Create a new SETUP.md file:

```markdown
# Daz Setup Guide

## Environment Variables

This application uses environment variables for all sensitive configuration.

### Required Variables
- `DAZ_CYTUBE_USERNAME`: Your CyTube username
- `DAZ_CYTUBE_PASSWORD`: Your CyTube password  
- `DAZ_CYTUBE_CHANNEL`: The CyTube channel to join

### Optional Database Variables
- `DAZ_DB_TYPE`: Database type (sqlite or mysql, default: sqlite)
- `DAZ_DB_USER`: MySQL username (required if using MySQL)
- `DAZ_DB_PASSWORD`: MySQL password (required if using MySQL)
- `DAZ_DB_NAME`: MySQL database name (default: daz)
- `DAZ_DB_HOST`: MySQL host (default: localhost)
- `DAZ_DB_PORT`: MySQL port (default: 3306)
- `DAZ_DB_PATH`: SQLite file path (default: ./daz.db)

### Setup Instructions

1. Copy `.env.example` to `.env`
2. Edit `.env` with your credentials
3. Run `./scripts/run-console.sh`

### Alternative: Export Variables

```bash
export DAZ_CYTUBE_USERNAME="your_username"
export DAZ_CYTUBE_PASSWORD="your_password"
export DAZ_CYTUBE_CHANNEL="your_channel"
./scripts/run-console.sh
```
```

## Migration Process

### For Existing Users

1. **Backup current config.json**
   ```bash
   cp config.json config.json.backup
   ```

2. **Create .env file from existing config**
   ```bash
   # Extract values from config.json and create .env
   echo "DAZ_CYTUBE_USERNAME=***REMOVED***" >> .env
   echo "DAZ_CYTUBE_PASSWORD=***REMOVED***" >> .env
   echo "DAZ_CYTUBE_CHANNEL=***REMOVED***" >> .env
   echo "DAZ_DB_USER=***REMOVED***" >> .env
   echo "DAZ_DB_PASSWORD=***REMOVED***" >> .env
   echo "DAZ_DB_NAME=daz" >> .env
   ```

3. **Remove config.json from repository**
   ```bash
   git rm --cached config.json
   git commit -m "Remove config.json with credentials"
   ```

4. **Test with new scripts**
   ```bash
   ./scripts/run-console.sh
   ```

## Testing Plan

### 1. Unit Tests
- Test config loading without defaults
- Test environment variable override
- Test validation with missing credentials

### 2. Integration Tests
- Test script execution with environment variables
- Test config file + environment variable precedence
- Test error handling for missing credentials

### 3. Manual Testing Checklist
- [ ] Remove all .env and config.json files
- [ ] Run application - should fail with clear error
- [ ] Add only required env vars - should work
- [ ] Test with .env file
- [ ] Test with exported variables
- [ ] Test config.json override by env vars

## Security Benefits

1. **No credentials in source code** - Nothing compiled into binary
2. **No credentials in version control** - config.json is gitignored
3. **Flexible deployment** - Different credentials per environment
4. **Clear separation** - Credentials separate from configuration
5. **Secure command lines** - No visible passwords in process lists

## Rollback Plan

If issues arise:
1. Restore config.json from backup
2. Revert to previous version of scripts
3. Document any issues encountered

## Checklist for Implementation

- [ ] Update .gitignore to include config.json
- [ ] Update config.json.example with placeholders
- [ ] Modify run-console.sh to use environment variables
- [ ] Modify run.sh to use environment variables
- [ ] Update internal/config/config.go to remove defaults
- [ ] Add environment variable validation
- [ ] Create .env.example file
- [ ] Update documentation (README.md, SETUP.md)
- [ ] Test all changes thoroughly
- [ ] Remove config.json from repository
- [ ] Communicate changes to team
