package framework

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// PluginWithDependencies extends the Plugin interface with dependency management
type PluginWithDependencies interface {
	Plugin
	// Dependencies returns a list of plugin names this plugin depends on
	Dependencies() []string
	// Ready returns true when the plugin is ready to accept requests
	Ready() bool
}

// PluginManager manages plugin lifecycle and dependencies
type PluginManager struct {
	plugins      map[string]Plugin
	startOrder   []string
	dependencies map[string][]string
	ready        map[string]bool
}

// NewPluginManager creates a new plugin manager
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins:      make(map[string]Plugin),
		dependencies: make(map[string][]string),
		ready:        make(map[string]bool),
	}
}

// RegisterPlugin registers a plugin with the manager
func (pm *PluginManager) RegisterPlugin(name string, plugin Plugin) error {
	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	pm.plugins[name] = plugin

	// Check if plugin supports dependencies
	if depPlugin, ok := plugin.(PluginWithDependencies); ok {
		pm.dependencies[name] = depPlugin.Dependencies()
	} else {
		pm.dependencies[name] = []string{}
	}

	return nil
}

// InitializeAll initializes all plugins with their configuration
func (pm *PluginManager) InitializeAll(configs map[string]json.RawMessage, eventBus EventBus) error {
	// Resolve startup order based on dependencies
	if err := pm.resolveStartupOrder(); err != nil {
		return fmt.Errorf("failed to resolve plugin dependencies: %w", err)
	}

	// Initialize plugins in dependency order
	for _, name := range pm.startOrder {
		plugin := pm.plugins[name]
		config := configs[name]

		log.Printf("Initializing %s plugin...", name)

		// Convert config to JSON if needed
		configJSON, err := convertToJSON(config)
		if err != nil {
			return fmt.Errorf("failed to convert config for %s: %w", name, err)
		}

		if err := plugin.Init(configJSON, eventBus); err != nil {
			return fmt.Errorf("failed to initialize %s plugin: %w", name, err)
		}
	}

	return nil
}

// StartAll starts all plugins in dependency order
func (pm *PluginManager) StartAll() error {
	for _, name := range pm.startOrder {
		plugin := pm.plugins[name]

		// Check dependencies are ready
		if err := pm.waitForDependencies(name, 30*time.Second); err != nil {
			return fmt.Errorf("dependencies not ready for %s: %w", name, err)
		}

		log.Printf("Starting %s plugin...", name)
		if err := plugin.Start(); err != nil {
			return fmt.Errorf("failed to start %s plugin: %w", name, err)
		}

		// Wait for plugin to be ready
		if err := pm.waitForReady(name, 10*time.Second); err != nil {
			return fmt.Errorf("%s plugin failed to become ready: %w", name, err)
		}

		pm.ready[name] = true
		log.Printf("%s plugin is ready", name)
	}

	return nil
}

// StopAll stops all plugins in reverse order
func (pm *PluginManager) StopAll() {
	// Stop in reverse order
	for i := len(pm.startOrder) - 1; i >= 0; i-- {
		name := pm.startOrder[i]
		plugin := pm.plugins[name]

		log.Printf("Stopping %s plugin...", name)
		if err := plugin.Stop(); err != nil {
			log.Printf("Error stopping %s plugin: %v", name, err)
		}
		pm.ready[name] = false
	}
}

// resolveStartupOrder performs topological sort to determine startup order
func (pm *PluginManager) resolveStartupOrder() error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	pm.startOrder = []string{}

	// Helper function for DFS
	var visit func(string) error
	visit = func(name string) error {
		if recStack[name] {
			return fmt.Errorf("circular dependency detected involving %s", name)
		}
		if visited[name] {
			return nil
		}

		visited[name] = true
		recStack[name] = true

		// Visit dependencies first
		for _, dep := range pm.dependencies[name] {
			if _, exists := pm.plugins[dep]; !exists {
				return fmt.Errorf("plugin %s depends on non-existent plugin %s", name, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}

		recStack[name] = false
		pm.startOrder = append(pm.startOrder, name)
		return nil
	}

	// Visit all plugins
	for name := range pm.plugins {
		if !visited[name] {
			if err := visit(name); err != nil {
				return err
			}
		}
	}

	return nil
}

// waitForDependencies waits for all dependencies to be ready
func (pm *PluginManager) waitForDependencies(pluginName string, timeout time.Duration) error {
	deps := pm.dependencies[pluginName]
	if len(deps) == 0 {
		return nil
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		allReady := true
		for _, dep := range deps {
			if !pm.ready[dep] {
				allReady = false
				break
			}
		}

		if allReady {
			return nil
		}

		<-ticker.C
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for dependencies: %v", deps)
		}
	}
}

// waitForReady waits for a plugin to report ready status
func (pm *PluginManager) waitForReady(pluginName string, timeout time.Duration) error {
	plugin := pm.plugins[pluginName]

	// Check if plugin supports readiness
	depPlugin, ok := plugin.(PluginWithDependencies)
	if !ok {
		// Legacy plugin - assume ready after Start() returns
		return nil
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if depPlugin.Ready() {
			return nil
		}

		<-ticker.C
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for plugin to be ready")
		}
	}
}

// convertToJSON converts various config types to JSON
func convertToJSON(config interface{}) ([]byte, error) {
	if config == nil {
		return []byte("{}"), nil
	}

	// If already []byte, assume it's JSON
	if jsonBytes, ok := config.([]byte); ok {
		return jsonBytes, nil
	}

	// Otherwise, marshal to JSON
	return json.Marshal(config)
}
