// Package config owns the app's persisted settings (data/config.json):
// the ISP portal URL opened in Chrome, and which WhatsApp chats are
// selected for monitoring. The TUI writes it; every reader takes a
// snapshot under the lock, so edits apply without a restart.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultPortalURL is opened in Chrome when config.json has no portal
// section (fresh install, or a config written before the portal setting
// existed).
const DefaultPortalURL = "http://103.67.54.54/"

// PortalSettings controls the Chrome window opened at startup.
type PortalSettings struct {
	// URL of the ISP billing portal. Empty (only possible when explicitly
	// set — an absent key falls back to DefaultPortalURL) = no browser.
	URL string `json:"url"`
}

// MonitoredChat is one chat the operator chose to monitor. JID is the
// full WhatsApp JID (e.g. "1234567890@g.us" for a group,
// "923001234567@s.whatsapp.net" for a direct chat).
type MonitoredChat struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// DefaultPanel is the panel shown in the log's PANEL column when config
// doesn't say otherwise (currently the account only serves one panel).
const DefaultPanel = "PACE"

// MonitorSettings is the persisted selection: only messages from these
// chats (and containing a customer username) are written to
// monitored_messages.json. Empty = nothing selected yet (the TUI asks
// for a selection once WhatsApp is linked).
type MonitorSettings struct {
	Chats []MonitoredChat `json:"chats"`
	// Panel labels captured rows (PACE today; editable here so no code
	// change is needed when another panel comes online).
	Panel string `json:"panel"`
}

// Config is the full persisted document (data/config.json).
type Config struct {
	Portal  PortalSettings  `json:"portal"`
	Monitor MonitorSettings `json:"monitor"`
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
		cfg: &Config{
			Portal:  PortalSettings{URL: DefaultPortalURL},
			Monitor: MonitorSettings{Panel: DefaultPanel},
		},
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
	if err := json.Unmarshal(data, cfg); err != nil {
		_ = os.Rename(m.filePath, m.filePath+".corrupt")
		m.cfg = &Config{Portal: PortalSettings{URL: DefaultPortalURL}}
		return m.cfg, nil
	}
	// An absent "portal" section means the file predates the setting (or
	// was written by an older build): fall back to the default portal URL.
	// An explicitly empty "url" is honored as-is (disables the browser).
	if !hasPortalSection(data) {
		cfg.Portal.URL = DefaultPortalURL
	}
	if cfg.Monitor.Panel == "" {
		cfg.Monitor.Panel = DefaultPanel
	}
	m.cfg = cfg
	return m.cfg, nil
}

// hasPortalSection reports whether the raw config JSON contains a
// "portal" object at the top level.
func hasPortalSection(data []byte) bool {
	probe := struct {
		Portal json.RawMessage `json:"portal"`
	}{}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return len(probe.Portal) > 0 && string(probe.Portal) != "null"
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

// PortalURL returns the configured portal URL ("" when none).
func (m *Manager) PortalURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.Portal.URL
}

// MonitoredChats returns a copy of the current selection.
func (m *Manager) MonitoredChats() []MonitoredChat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MonitoredChat, len(m.cfg.Monitor.Chats))
	copy(out, m.cfg.Monitor.Chats)
	return out
}

// Panel returns the panel label for captured rows (default "PACE").
func (m *Manager) Panel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg.Monitor.Panel == "" {
		return DefaultPanel
	}
	return m.cfg.Monitor.Panel
}

// SetMonitoredChats replaces the selection and persists it.
func (m *Manager) SetMonitoredChats(chats []MonitoredChat) error {
	m.mu.Lock()
	m.cfg.Monitor.Chats = make([]MonitoredChat, len(chats))
	copy(m.cfg.Monitor.Chats, chats)
	err := m.save()
	m.mu.Unlock()
	return err
}

func (m *Manager) FilePath() string {
	return m.filePath
}
