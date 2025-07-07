package eventbus

import (
	"fmt"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
)

// PluginRegistry manages plugin registration and lifecycle
type PluginRegistry struct {
	plugins map[string]framework.Plugin
	mu      sync.RWMutex
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]framework.Plugin),
	}
}

// Register adds a plugin to the registry
func (pr *PluginRegistry) Register(name string, plugin framework.Plugin) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	pr.plugins[name] = plugin
	return nil
}

// Get retrieves a plugin by name
func (pr *PluginRegistry) Get(name string) (framework.Plugin, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	plugin, exists := pr.plugins[name]
	return plugin, exists
}

// GetAll returns all registered plugins
func (pr *PluginRegistry) GetAll() map[string]framework.Plugin {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	// Create a copy to avoid race conditions
	result := make(map[string]framework.Plugin)
	for k, v := range pr.plugins {
		result[k] = v
	}
	return result
}

// Remove unregisters a plugin
func (pr *PluginRegistry) Remove(name string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.plugins[name]; !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	delete(pr.plugins, name)
	return nil
}
