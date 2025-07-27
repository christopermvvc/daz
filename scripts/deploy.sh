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
    
    # Copy files
    echo "Installing files..."
    cp daz /opt/daz/
    chmod +x /opt/daz/daz
    chown -R daz:daz /opt/daz
    
    # Install systemd service
    echo "Installing systemd service..."
    cp "$SCRIPT_DIR/daz.service" /etc/systemd/system/
    systemctl daemon-reload
    
    # Configure service
    echo "Configuring service..."
    read -p "Enter Cytube channel name: " CHANNEL
    if [ -z "$CHANNEL" ]; then
        echo "Error: Channel name is required"
        exit 1
    fi
    
    # Create override file
    mkdir -p /etc/systemd/system/daz.service.d
    cat > /etc/systemd/system/daz.service.d/override.conf <<EOF
[Service]
ExecStart=
ExecStart=/opt/daz/daz -channel=$CHANNEL
EOF
    
    # Start service
    echo "Starting service..."
    systemctl enable daz.service
    systemctl start daz.service
    
    # Check status
    systemctl status daz.service --no-pager
    
    echo ""
    echo "Deployment complete!"
    echo "View logs with: journalctl -u daz -f"
    echo "Stop with: systemctl stop daz"
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