package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Settings struct {
	RTKEnabled     bool   `json:"rtk_enabled"`
	CavemanEnabled bool   `json:"caveman_enabled"`
	LogLevel       string `json:"log_level"`
	MaxRetries     int    `json:"max_retries"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxTokens      int    `json:"max_tokens"`
}

type Config struct {
	settings Settings
	mu       sync.RWMutex
	path     string
}

func New() *Config {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".config", "llm-router", "config.json")

	c := &Config{path: path}
	c.load()
	return c
}

func (c *Config) Get() Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}

func (c *Config) GetPath() string {
	return c.path
}

func (c *Config) Set(s Settings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = s
	c.save()
}

func (c *Config) SetRTK(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings.RTKEnabled = enabled
	c.save()
}

func (c *Config) SetCaveman(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings.CavemanEnabled = enabled
	c.save()
}

func (c *Config) SetLogLevel(level string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings.LogLevel = level
	c.save()
}

func (c *Config) SetMaxRetries(retries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings.MaxRetries = retries
	c.save()
}

func (c *Config) SetTimeout(seconds int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings.TimeoutSeconds = seconds
	c.save()
}

func (c *Config) SetMaxTokens(tokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings.MaxTokens = tokens
	c.save()
}

func (c *Config) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		c.settings = Settings{
			RTKEnabled:     true,
			CavemanEnabled: true,
			LogLevel:       "info",
			MaxRetries:     2,
			TimeoutSeconds: 300,
			MaxTokens:      8192,
		}
		return
	}
	json.Unmarshal(data, &c.settings)
}

func (c *Config) save() {
	dir := filepath.Dir(c.path)
	os.MkdirAll(dir, 0755)

	data, err := json.MarshalIndent(c.settings, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(c.path, data, 0644)
}
