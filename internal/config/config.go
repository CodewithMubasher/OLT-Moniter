// Package config owns Bills OS's persisted settings: the WhatsApp
// auto-reply message and whether it is active. It is written by the TUI
// and read by the auto-reply loop, so edits take effect on the very next
// incoming message — no restart needed.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultAutoReplyMessage is what a fresh install replies with until the
// operator customizes it in the TUI (key "m").
const DefaultAutoReplyMessage = "Thanks for your message! We have received it and will get back to you soon."

// WhatsAppSettings controls the auto-reply side of Bills OS.
type WhatsAppSettings struct {
	// AutoReplyMessage is sent back to every incoming message when replies
	// are active.
	AutoReplyMessage string `json:"auto_reply_message"`
	// AutoReplyPaused pauses replies: messages are still received but none
	// are sent until resumed. Defaults to true so a fresh install (or a
	// device reconnecting after a long time) never blasts old messages.
	AutoReplyPaused bool `json:"auto_reply_paused"`
}

// Config is the full persisted document (data/config.json).
type Config struct {
	WhatsApp WhatsAppSettings `json:"whatsapp"`
}

// Manager guards Config behind a mutex and persists every change atomically
// (write to a temp file, then rename) so a crash mid-write can never corrupt
// the on-disk config.
type Manager struct {
	dir      string
	filePath string

	mu  sync.RWMutex
	cfg *Config
}

func NewManager(dir string) *Manager {
	return &Manager{
		dir:      dir,
		filePath: filepath.Join(dir, "config.json"),
		cfg: &Config{WhatsApp: WhatsAppSettings{
			AutoReplyMessage: DefaultAutoReplyMessage,
			AutoReplyPaused:  true, // paused by default — operator resumes explicitly
		}},
	}
}

// Load reads config.json, tolerating a missing or corrupt file by starting
// with defaults (a corrupt file is renamed aside for inspection, never
// discarded silently).
func (m *Manager) Load() (*Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, cfg); err != nil {
			_ = os.Rename(m.filePath, m.filePath+".corrupt")
			cfg = &Config{}
		}
	}
	// Resolve pause state: prefer the current key; fall back to the legacy
	// "auto_reply_enabled" key from early builds; if neither is present the
	// file predates pausing, so treat it as paused (safe default — never
	// surprise anyone with replies after a reconnect).
	aux := struct {
		WhatsApp struct {
			AutoReplyPaused  *bool `json:"auto_reply_paused"`
			AutoReplyEnabled *bool `json:"auto_reply_enabled"`
		} `json:"whatsapp"`
	}{}
	_ = json.Unmarshal(data, &aux)
	switch {
	case aux.WhatsApp.AutoReplyPaused != nil:
		cfg.WhatsApp.AutoReplyPaused = *aux.WhatsApp.AutoReplyPaused
	case aux.WhatsApp.AutoReplyEnabled != nil:
		cfg.WhatsApp.AutoReplyPaused = !*aux.WhatsApp.AutoReplyEnabled
	default:
		if len(strings.TrimSpace(string(data))) > 0 {
			cfg.WhatsApp.AutoReplyPaused = true
		}
	}
	if cfg.WhatsApp.AutoReplyMessage == "" {
		cfg.WhatsApp.AutoReplyMessage = DefaultAutoReplyMessage
	}
	m.cfg = cfg
	return m.cfg, nil
}

func (m *Manager) save() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := m.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, m.filePath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Snapshot returns a copy of the WhatsApp settings for safe read-only use
// (TUI rendering, the auto-reply loop) without holding the lock.
func (m *Manager) Snapshot() WhatsAppSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.WhatsApp
}

// SetAutoReplyMessage replaces the auto-reply message and persists it.
func (m *Manager) SetAutoReplyMessage(msg string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Errorf("message cannot be empty")
	}
	m.mu.Lock()
	m.cfg.WhatsApp.AutoReplyMessage = msg
	err := m.save()
	m.mu.Unlock()
	return err
}

// SetAutoReplyPaused pauses or resumes auto-reply and persists the change.
func (m *Manager) SetAutoReplyPaused(paused bool) error {
	m.mu.Lock()
	m.cfg.WhatsApp.AutoReplyPaused = paused
	err := m.save()
	m.mu.Unlock()
	return err
}

func (m *Manager) FilePath() string {
	return m.filePath
}
