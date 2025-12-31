package config

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/unifyai-proxy/unifyai-proxy/internal/util"
)

// ConfigChangeListener is notified when configuration changes
type ConfigChangeListener interface {
	OnConfigChange(newConfig *Config)
}

// ConfigWatcher watches for configuration file changes and reloads
type ConfigWatcher struct {
	configPath string
	config     atomic.Value // *Config
	version    atomic.Uint64
	listeners  []ConfigChangeListener
	mu         sync.RWMutex
	stopCh     chan struct{}
	watcher    *fsnotify.Watcher
}

// NewConfigWatcher creates a new configuration watcher
func NewConfigWatcher(configPath string) (*ConfigWatcher, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	cw := &ConfigWatcher{
		configPath: configPath,
		stopCh:     make(chan struct{}),
		watcher:    watcher,
	}
	cw.config.Store(cfg)
	cw.version.Store(1)

	return cw, nil
}

// Start begins watching for configuration changes
func (cw *ConfigWatcher) Start() error {
	if err := cw.watcher.Add(cw.configPath); err != nil {
		return err
	}

	go cw.watchLoop()
	return nil
}

// Stop stops watching for configuration changes
func (cw *ConfigWatcher) Stop() {
	close(cw.stopCh)
	cw.watcher.Close()
}

// GetConfig returns the current configuration
func (cw *ConfigWatcher) GetConfig() *Config {
	return cw.config.Load().(*Config)
}

// GetVersion returns the current configuration version
func (cw *ConfigWatcher) GetVersion() uint64 {
	return cw.version.Load()
}

// AddListener adds a configuration change listener
func (cw *ConfigWatcher) AddListener(listener ConfigChangeListener) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.listeners = append(cw.listeners, listener)
}

// watchLoop watches for file changes and reloads configuration
func (cw *ConfigWatcher) watchLoop() {
	// Debounce timer to prevent rapid reloads
	var debounceTimer *time.Timer
	var debounceTimerMu sync.Mutex
	const debounceDelay = 500 * time.Millisecond

	scheduleReload := func() {
		debounceTimerMu.Lock()
		defer debounceTimerMu.Unlock()
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(debounceDelay, func() {
			cw.reload()
		})
	}

	for {
		select {
		case <-cw.stopCh:
			debounceTimerMu.Lock()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimerMu.Unlock()
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}

			// Handle write, create, and rename events
			// Some editors (vim, vscode) use rename+create instead of write
			if event.Op&fsnotify.Write == fsnotify.Write ||
				event.Op&fsnotify.Create == fsnotify.Create {
				scheduleReload()
			}

			// Handle file being renamed/removed - need to re-add watch
			if event.Op&fsnotify.Rename == fsnotify.Rename ||
				event.Op&fsnotify.Remove == fsnotify.Remove {
				// Try to re-add the watch after a short delay
				// The file might be recreated by the editor
				time.AfterFunc(100*time.Millisecond, func() {
					if err := cw.watcher.Add(cw.configPath); err == nil {
						scheduleReload()
					}
				})
			}

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("config watcher error: %v", err)
		}
	}
}

// reload reloads the configuration from file
func (cw *ConfigWatcher) reload() {
	newConfig, err := Load(cw.configPath)
	if err != nil {
		log.Printf("failed to reload config: %v (keeping current config)", err)
		return
	}

	// Atomically replace configuration
	cw.config.Store(newConfig)
	cw.version.Add(1)

	log.Printf("configuration reloaded (version %d)", cw.version.Load())

	// Notify listeners
	cw.mu.RLock()
	listeners := make([]ConfigChangeListener, len(cw.listeners))
	copy(listeners, cw.listeners)
	cw.mu.RUnlock()

	for _, listener := range listeners {
		go cw.notifyListener(listener, newConfig)
	}
}

// notifyListener safely notifies a single listener with panic recovery
func (cw *ConfigWatcher) notifyListener(listener ConfigChangeListener, newConfig *Config) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in config change listener: %v", r)
		}
	}()

	listener.OnConfigChange(newConfig)
}

// ResolveModel resolves a model name through the alias chain
func (c *Config) ResolveModel(model string) string {
	const maxDepth = 5
	current := model

	for i := 0; i < maxDepth; i++ {
		next, exists := c.ModelMapping[current]
		if !exists {
			return current
		}
		current = next
	}

	return current
}

// IsModelBlacklisted checks if a model is blacklisted
func (c *Config) IsModelBlacklisted(model string) bool {
	for _, blacklisted := range c.ModelBlacklist {
		if blacklisted == model || util.MatchWildcard(blacklisted, model) {
			return true
		}
	}
	return false
}

// GetAccountLimits returns limits for a specific provider or default
func (c *Config) GetAccountLimits(provider string, accountID string) AccountLimitEntry {
	// Check for account-specific override first
	for _, override := range c.AccountLimits.Overrides {
		if override.ID == accountID {
			return AccountLimitEntry{
				Daily: override.Daily,
				Rate:  override.Rate,
			}
		}
	}

	// Check for provider-specific limits
	if limits, exists := c.AccountLimits.Providers[provider]; exists {
		return limits
	}

	// Return default limits
	return c.AccountLimits.Default
}

