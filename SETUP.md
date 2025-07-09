# Daz Setup Guide

## Overview

This application uses environment variables for all sensitive configuration to ensure credentials are never stored in the repository.

## Prerequisites

- Go 1.21 or later
- PostgreSQL database
- CyTube account with appropriate channel access

## Environment Variables

### Required Variables
- `DAZ_CYTUBE_USERNAME`: Your CyTube username
- `DAZ_CYTUBE_PASSWORD`: Your CyTube password  
- `DAZ_CYTUBE_CHANNEL`: The CyTube channel to join

### Database Variables
- `DAZ_DB_TYPE`: Database type (postgres or sqlite, default: postgres)
- `DAZ_DB_USER`: PostgreSQL username (required if using PostgreSQL)
- `DAZ_DB_PASSWORD`: PostgreSQL password (required if using PostgreSQL)
- `DAZ_DB_NAME`: PostgreSQL database name (default: daz)
- `DAZ_DB_HOST`: PostgreSQL host (default: localhost)
- `DAZ_DB_PORT`: PostgreSQL port (default: 5432)
- `DAZ_DB_PATH`: SQLite file path (default: ./daz.db, only used if DAZ_DB_TYPE=sqlite)

## Quick Start

### 1. Clone the Repository
```bash
git clone <repository-url>
cd daz
```

### 2. Set Up Environment Variables

#### Option A: Using .env File (Recommended for Development)
```bash
# Copy the example environment file
cp .env.example .env

# Edit .env with your credentials
nano .env
```

#### Option B: Export Environment Variables
```bash
export DAZ_CYTUBE_USERNAME="your_username"
export DAZ_CYTUBE_PASSWORD="your_password"
export DAZ_CYTUBE_CHANNEL="your_channel"
export DAZ_DB_USER="your_db_user"
export DAZ_DB_PASSWORD="your_db_password"
export DAZ_DB_NAME="daz"
```

### 3. Set Up the Database

For PostgreSQL:
```bash
# Create the database
createdb daz

# Run the setup script
./scripts/setup_db.sh
```

For SQLite:
```bash
export DAZ_DB_TYPE=sqlite
# Database will be created automatically on first run
```

### 4. Build and Run

```bash
# Build the binary
./scripts/build-daz.sh

# Run with console output
./scripts/run-console.sh

# Or run in background with tmux
./scripts/run-persistent.sh
```

## Configuration File (Optional)

While environment variables are the primary method for credentials, you can also use a configuration file for non-sensitive settings:

1. Copy the example configuration:
   ```bash
   cp config.json.example config.json
   ```

2. Edit `config.json` with your settings (DO NOT add real credentials here)

3. The loading order is:
   1. Default values
   2. Configuration file (if present)
   3. Environment variables (highest priority)
   4. Command-line flags (override everything)

## Command-Line Flags

All settings can also be provided via command-line flags:

```bash
./bin/daz \
    -channel "your_channel" \
    -username "your_username" \
    -password "your_password" \
    -db-type postgres \
    -db-host localhost \
    -db-port 5432 \
    -db-user "db_user" \
    -db-pass "db_password" \
    -db-name "daz"
```

## Security Best Practices

1. **Never commit credentials**: The `.gitignore` file is configured to exclude `.env` and `config.json`
2. **Use unique passwords**: Don't reuse passwords across services
3. **Rotate credentials regularly**: Update passwords periodically
4. **Restrict file permissions**: 
   ```bash
   chmod 600 .env
   ```
5. **Use environment variables in production**: Avoid configuration files in production environments

## Troubleshooting

### Missing Environment Variables
If you see errors like:
```
Error: DAZ_CYTUBE_USERNAME environment variable is not set
```

Make sure you've:
1. Created a `.env` file with all required variables
2. OR exported the variables in your shell
3. Are running from the project root directory

### Database Connection Issues
- Verify PostgreSQL is running: `pg_isready`
- Check credentials and database exist
- Ensure the database user has proper permissions

### Build Errors
- Ensure Go 1.21+ is installed: `go version`
- Try cleaning and rebuilding: `make clean && make build`

## Production Deployment

For production environments:

1. Use a proper secret management system (e.g., HashiCorp Vault, AWS Secrets Manager)
2. Set environment variables through your deployment system
3. Never use `.env` files in production
4. Enable TLS for database connections
5. Use strong, unique passwords

## Migration from Old Configuration

If you're migrating from the old hardcoded credential system:

1. **Backup your current config.json**:
   ```bash
   cp config.json config.json.backup
   ```

2. **Create .env from existing credentials**:
   ```bash
   # Extract values from your backup and create .env
   echo "DAZ_CYTUBE_USERNAME=your_username" >> .env
   echo "DAZ_CYTUBE_PASSWORD=your_password" >> .env
   echo "DAZ_CYTUBE_CHANNEL=your_channel" >> .env
   echo "DAZ_DB_USER=your_db_user" >> .env
   echo "DAZ_DB_PASSWORD=your_db_password" >> .env
   echo "DAZ_DB_NAME=daz" >> .env
   ```

3. **Remove config.json from git** (if it was tracked):
   ```bash
   git rm --cached config.json
   git commit -m "Remove config.json with credentials"
   ```

4. **Test the new setup**:
   ```bash
   ./scripts/run-console.sh
   ```

## Support

For issues or questions:
- Check the logs in the `logs/` directory
- Review the configuration with debug logging enabled
- Open an issue in the project repository