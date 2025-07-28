#!/bin/bash

# Daz Deployment Script
# Supports both Docker and systemd deployment methods

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
DEPLOY_METHOD="${1:-docker}"

echo "Daz Deployment Script"
echo "===================="

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to deploy using Docker
deploy_docker() {
    echo "Deploying with Docker..."
    
    if ! command_exists docker; then
        echo "Error: Docker is not installed"
        exit 1
    fi
    
    if ! command_exists docker-compose; then
        echo "Error: docker-compose is not installed"
        exit 1
    fi
    
    cd "$PROJECT_ROOT"
    
    # Build and start containers
    echo "Building Docker image..."
    docker-compose build
    
    echo "Starting containers..."
    docker-compose up -d
    
    echo "Waiting for services to be ready..."
    sleep 5
    
    # Check status
    docker-compose ps
    
    echo ""
    echo "Deployment complete!"
    echo "View logs with: docker-compose logs -f daz"
    echo "Stop with: docker-compose down"
}

# Function to deploy using systemd
deploy_systemd() {
    echo "Deploying with systemd..."
    
    if [[ $EUID -ne 0 ]]; then
        echo "Error: systemd deployment must be run as root"
        exit 1
    fi
    
    # Build binary
    echo "Building Daz binary..."
    cd "$PROJECT_ROOT"
    make build
    
    # Create directories
    echo "Creating directories..."
    mkdir -p /opt/daz/logs
    
    # Create user if doesn't exist
    if ! id "daz" &>/dev/null; then
        echo "Creating daz user..."
        useradd -r -s /bin/false -d /opt/daz daz
    fi
    
    # Check if this is a new installation or update
    if [ -f /opt/daz/daz ]; then
        echo "Existing installation detected, updating..."
        
        # Stop service before copying
        echo "Stopping service..."
        systemctl stop daz.service
        
        # Copy files
        echo "Installing files..."
        cp ./bin/daz /opt/daz/daz
        chmod +x /opt/daz/daz
        
        # Copy config.json if it exists in source but not in destination
        if [ -f "$PROJECT_ROOT/config.json" ] && [ ! -f /opt/daz/config.json ]; then
            echo "Copying config.json..."
            cp "$PROJECT_ROOT/config.json" /opt/daz/config.json
        fi
        
        chown -R daz:daz /opt/daz
        
        # Start service
        echo "Starting service..."
        systemctl start daz.service
    else
        # New installation
        echo "New installation detected..."
        
        # Copy files
        echo "Installing files..."
        cp ./bin/daz /opt/daz/daz
        chmod +x /opt/daz/daz
        
        # Copy config.json if it exists
        if [ -f "$PROJECT_ROOT/config.json" ]; then
            echo "Copying config.json..."
            cp "$PROJECT_ROOT/config.json" /opt/daz/config.json
            chown -R daz:daz /opt/daz
        else
            echo "Warning: config.json not found in $PROJECT_ROOT"
            echo "Make sure to create /opt/daz/config.json before starting the service"
        fi
        
        # Install systemd service
        echo "Installing systemd service..."
        cp "$SCRIPT_DIR/daz.service" /etc/systemd/system/
        systemctl daemon-reload
        
        # Enable and start service
        echo "Enabling service..."
        systemctl enable daz.service
        
        echo "Starting service..."
        systemctl start daz.service
    fi
    
    # Check status
    echo ""
    echo "Checking service status..."
    systemctl status daz.service --no-pager
    
    # Show result
    if systemctl is-active --quiet daz.service; then
        echo ""
        echo "✅ Deployment successful!"
        echo ""
        echo "Useful commands:"
        echo "  View logs:    journalctl -u daz -f"
        echo "  Stop service: systemctl stop daz"
        echo "  Restart:      systemctl restart daz"
        echo "  Status:       systemctl status daz"
    else
        echo ""
        echo "❌ Service failed to start!"
        echo "Check logs with: journalctl -u daz -n 50"
        exit 1
    fi
}

# Function to show usage
show_usage() {
    echo "Usage: $0 [docker|systemd]"
    echo ""
    echo "Deployment methods:"
    echo "  docker   - Deploy using Docker and docker-compose (default)"
    echo "  systemd  - Deploy as systemd service (requires root)"
}

# Main logic
case "$DEPLOY_METHOD" in
    docker)
        deploy_docker
        ;;
    systemd)
        deploy_systemd
        ;;
    help|--help|-h)
        show_usage
        ;;
    *)
        echo "Error: Unknown deployment method: $DEPLOY_METHOD"
        show_usage
        exit 1
        ;;
esac