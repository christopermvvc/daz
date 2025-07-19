<div align="center">
  <img src="logo.png" alt="Daz Logo" width="200"/>
  
  # Daz - Modular Cytube Chat Bot
  
  [![Go Version](https://img.shields.io/badge/go-1.23+-00ADD8?style=for-the-badge&logo=go)](https://golang.org/)
  [![PostgreSQL](https://img.shields.io/badge/PostgreSQL-14+-316192?style=for-the-badge&logo=postgresql)](https://www.postgresql.org/)
  [![License](https://img.shields.io/badge/license-MIT-green?style=for-the-badge)](LICENSE)
  [![Docker](https://img.shields.io/badge/docker-ready-2496ED?style=for-the-badge&logo=docker)](https://www.docker.com/)
  
  *A resilient, plugin-based chat bot for Cytube channels built with Go*
</div>

## 🎯 Overview

Daz is a modular, event-driven chat bot designed for [Cytube](https://cytu.be) channels. Built with Go, it features a robust plugin architecture, PostgreSQL persistence, and multi-room support. Whether you're managing a single channel or multiple rooms, Daz provides the flexibility and reliability you need.

## ✨ Features

- **🔌 Plugin-Based Architecture** - Extend functionality with custom plugins
- **🏠 Multi-Room Support** - Connect to multiple Cytube channels simultaneously
- **🗄️ PostgreSQL Integration** - Persistent storage for events, users, and media
- **🔄 Resilient Connections** - Automatic reconnection with exponential backoff
- **📊 Analytics & Tracking** - Monitor user activity, media plays, and chat statistics
- **🎮 Command System** - Extensible command framework with cooldowns and permissions
- **🚀 High Performance** - Event-driven architecture with priority queuing
- **❤️ Health Monitoring** - Built-in health checks and Prometheus metrics
- **🔧 Easy Deployment** - Docker support and systemd service included

## 🚀 Quick Start

### Prerequisites

- Go 1.23 or higher
- PostgreSQL 14 or higher
- Network access to Cytube servers

### Installation

1. **Clone the repository**
   ```bash
   git clone https://github.com/hildolfr/daz.git
   cd daz
   ```

2. **Set up the database**
   ```bash
   createdb daz
   createuser ***REMOVED***
   # Grant permissions as needed
   ```

3. **Configure Daz**
   ```bash
   cp config.json.example config.json
   # Edit config.json with your settings
   ```

4. **Build and run**
   ```bash
   make build
   ./bin/daz
   ```

## 🔧 Configuration

Daz uses a JSON configuration file. Here's a minimal example:

```json
{
  "core": {
    "rooms": [{
      "channel": "your-channel-name",
      "username": "bot-username",
      "password": "bot-password",
      "enabled": true
    }],
    "database": {
      "host": "localhost",
      "port": 5432,
      "database": "daz",
      "user": "***REMOVED***",
      "password": "your-db-password"
    }
  },
  "plugins": {
    "eventfilter": {
      "command_prefix": "!"
    }
  }
}
```

See [config.json.example](config.json.example) for a complete configuration reference.

## 🏗️ Architecture

Daz follows an event-driven architecture with these core components:

- **Core Plugin** - Manages WebSocket connections to Cytube
- **EventBus** - Central message broker for inter-plugin communication
- **Plugin System** - Modular components for specific functionality
- **SQL Plugin** - Handles database persistence

For detailed architecture information, see [daz-chatbot-architecture.md](daz-chatbot-architecture.md).

## 📦 Built-in Plugins

### Core Plugins
- **SQL** - Database operations and event logging
- **EventFilter** - Event routing and command detection
- **Retry** - Persistent retry mechanism for failed operations

### Feature Plugins
- **UserTracker** - Track user sessions and activity
- **MediaTracker** - Monitor media plays and queue changes
- **Analytics** - Aggregate statistics and reporting

### Command Plugins
- **!help** - Display available commands
- **!about** - Show bot information
- **!uptime** - Report bot uptime
- **!debug** - Debug information (admin only)

## 🛠️ Development

### Building from Source

```bash
# Run tests
make test

# Format code
make fmt

# Run linters
make lint

# Build binary
make build
```

### Creating a Plugin

1. Implement the `framework.Plugin` interface
2. Register event handlers with the EventBus
3. Add configuration schema
4. Register in `main.go`

Example plugin structure:
```go
type MyPlugin struct {
    eventBus framework.EventBus
    config   MyPluginConfig
}

func (p *MyPlugin) Init(eventBus framework.EventBus, config json.RawMessage) error {
    p.eventBus = eventBus
    // Parse config, subscribe to events
    return nil
}
```

## 🐳 Docker Deployment

### Using Docker Compose

```bash
docker-compose up -d
```

### Building the Image

```bash
docker build -t daz:latest .
docker run -d --name daz -v ./config.json:/app/config.json daz:latest
```

## 📊 Monitoring

Daz provides health check endpoints and Prometheus metrics:

- `GET /health` - Overall system health
- `GET /health/live` - Liveness probe
- `GET /health/ready` - Readiness probe
- `GET /metrics` - Prometheus metrics

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guidelines](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- The Cytube community for their platform
- All contributors who have helped shape Daz
- The Go community for excellent libraries and tools

## 📚 Documentation

- [Architecture Guide](daz-chatbot-architecture.md)
- [Configuration Reference](config.json.example)
- [API Documentation](docs/api.md) *(coming soon)*
- [Plugin Development Guide](docs/plugins.md) *(coming soon)*

## 💬 Support

- **Issues**: [GitHub Issues](https://github.com/hildolfr/daz/issues)
- **Discussions**: [GitHub Discussions](https://github.com/hildolfr/daz/discussions)

---

<div align="center">
  Made with ❤️ by the Daz community
</div>