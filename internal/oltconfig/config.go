// Package oltconfig owns the single source of truth for OLT Monitor's
// configuration: the OLT list, the check interval, and WhatsApp settings.
//
// It is written to concurrently by the TUI (goroutine running the Bubble Tea
// program) and by the WhatsApp command handler (goroutine reading inbound
// messages). Every mutating method takes the same mutex and persists to disk
// before returning, so both callers always observe a consistent, saved state.
package oltconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// OLT is a single monitored device.
type OLT struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Enabled bool   `json:"enabled"`

	// Runtime status. Persisted too, so the TUI shows last-known state
	// immediately after a restart instead of blanking until the next check.
	Status          Status    `json:"status"`
	ConsecutiveFail int       `json:"consecutive_fail"`
	LastCheck       time.Time `json:"last_check,omitempty"`
	LastSuccess     time.Time `json:"last_success,omitempty"`
	LastFailure     time.Time `json:"last_failure,omitempty"`
	LastRTTMs       int64     `json:"last_rtt_ms,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

type Status string

const (
	StatusUnknown Status = "unknown" // never checked yet (or disabled)
	StatusUp      Status = "up"
	StatusDown    Status = "down"
)

// WhatsAppSettings controls the remote-control side of the app.
type WhatsAppSettings struct {
	// TargetNumber is who OLT Monitor talks to: alerts go out to it, and
	// commands are only accepted from it. International format, e.g. +9230...
	TargetNumber string `json:"target_number"`
}

// Config is the full persisted document.
type Config struct {
	OLTs             []*OLT           `json:"olts"`
	IntervalSeconds  int              `json:"interval_seconds"`
	FailureThreshold int              `json:"failure_threshold"`
	WhatsApp         WhatsAppSettings `json:"whatsapp"`
}

const (
	DefaultIntervalSeconds  = 10
	DefaultFailureThreshold = 3
	MinIntervalSeconds      = 1
)

// Manager guards Config behind a mutex and persists every change atomically
// (write to a temp file, then rename) so a crash mid-write can never corrupt
// the on-disk config, matching the pattern used by the WhatsApp client's own
// config manager.
type Manager struct {
	dir      string
	filePath string

	mu  sync.RWMutex
	cfg *Config

	// onChange is invoked (outside the lock) after every successful mutation,
	// so the monitoring engine can react immediately — e.g. an OLT added via
	// WhatsApp starts being checked without waiting for the next tick, and an
	// interval change takes effect without a restart.
	onChange func()
}

func NewManager(dir string) *Manager {
	return &Manager{
		dir:      dir,
		filePath: filepath.Join(dir, "olts.json"),
		cfg: &Config{
			IntervalSeconds:  DefaultIntervalSeconds,
			FailureThreshold: DefaultFailureThreshold,
		},
	}
}

// OnChange registers the callback fired after each successful mutation.
// Not safe to call concurrently with mutations; call it once during startup
// before monitoring begins.
func (m *Manager) OnChange(fn func()) {
	m.onChange = fn
}

// Load reads olts.json, tolerating a missing or corrupt file by starting
// fresh (renaming the corrupt file aside for inspection, never discarding it
// silently).
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
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = DefaultIntervalSeconds
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = DefaultFailureThreshold
	}
	for _, o := range cfg.OLTs {
		o.IP = cleanTarget(o.IP)
		if o.Status == "" {
			o.Status = StatusUnknown
		}
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

func (m *Manager) notify() {
	if m.onChange != nil {
		go m.onChange()
	}
}

// Snapshot returns a deep-enough copy of the OLT list for safe read-only use
// (TUI rendering, WhatsApp "status" replies) without holding the lock.
func (m *Manager) Snapshot() []*OLT {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OLT, len(m.cfg.OLTs))
	for i, o := range m.cfg.OLTs {
		cp := *o
		out[i] = &cp
	}
	return out
}

func (m *Manager) Interval() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Duration(m.cfg.IntervalSeconds) * time.Second
}

func (m *Manager) FailureThreshold() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.FailureThreshold
}

func (m *Manager) WhatsAppTarget() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.WhatsApp.TargetNumber
}

func (m *Manager) SetWhatsAppTarget(number string) error {
	m.mu.Lock()
	m.cfg.WhatsApp.TargetNumber = number
	err := m.save()
	m.mu.Unlock()
	if err == nil {
		m.notify()
	}
	return err
}

// SetInterval changes the monitoring interval at runtime. The monitoring
// engine picks this up on its next tick via OnChange — no restart needed.
func (m *Manager) SetInterval(d time.Duration) error {
	if d < MinIntervalSeconds*time.Second {
		return fmt.Errorf("interval must be at least %ds", MinIntervalSeconds)
	}
	m.mu.Lock()
	m.cfg.IntervalSeconds = int(d.Seconds())
	err := m.save()
	m.mu.Unlock()
	if err == nil {
		m.notify()
	}
	return err
}

// AddOLT adds a new OLT, enabled by default, and persists immediately.
// Returns an error if the name is already used (names are the addressing
// key for both the TUI and WhatsApp commands).
func (m *Manager) AddOLT(name, ip string) (*OLT, error) {
	name = strings.TrimSpace(name)
	ip = cleanTarget(ip)
	if name == "" || ip == "" {
		return nil, fmt.Errorf("name and IP are required")
	}

	m.mu.Lock()
	if existing := m.findLocked(name); existing != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("an OLT named %q already exists", name)
	}
	olt := &OLT{Name: name, IP: ip, Enabled: true, Status: StatusUnknown}
	m.cfg.OLTs = append(m.cfg.OLTs, olt)
	err := m.save()
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}
	m.notify()
	cp := *olt
	return &cp, nil
}

// findLocked returns the live *OLT pointer (caller must hold m.mu).
func (m *Manager) findLocked(name string) *OLT {
	for _, o := range m.cfg.OLTs {
		if strings.EqualFold(o.Name, name) {
			return o
		}
	}
	return nil
}

func (m *Manager) EditOLT(name, newName, newIP string) error {
	m.mu.Lock()
	o := m.findLocked(name)
	if o == nil {
		m.mu.Unlock()
		return fmt.Errorf("no OLT named %q", name)
	}
	if newName != "" {
		if other := m.findLocked(newName); other != nil && other != o {
			m.mu.Unlock()
			return fmt.Errorf("an OLT named %q already exists", newName)
		}
		o.Name = newName
	}
	if newIP != "" {
		o.IP = cleanTarget(newIP)
	}
	err := m.save()
	m.mu.Unlock()
	if err == nil {
		m.notify()
	}
	return err
}

// cleanTarget removes invisible control characters introduced by terminal
// input events, then trims ordinary surrounding whitespace. URLs containing a
// NUL character are rejected by net/url and otherwise look identical on screen.
func cleanTarget(target string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, target))
}

func (m *Manager) RemoveOLT(name string) error {
	m.mu.Lock()
	idx := -1
	for i, o := range m.cfg.OLTs {
		if strings.EqualFold(o.Name, name) {
			idx = i
			break
		}
	}
	if idx == -1 {
		m.mu.Unlock()
		return fmt.Errorf("no OLT named %q", name)
	}
	m.cfg.OLTs = append(m.cfg.OLTs[:idx], m.cfg.OLTs[idx+1:]...)
	err := m.save()
	m.mu.Unlock()
	if err == nil {
		m.notify()
	}
	return err
}

// SetEnabled toggles monitoring for one OLT. Disabling resets its failure
// streak and status so re-enabling starts clean rather than immediately
// alerting on stale state.
func (m *Manager) SetEnabled(name string, enabled bool) error {
	m.mu.Lock()
	o := m.findLocked(name)
	if o == nil {
		m.mu.Unlock()
		return fmt.Errorf("no OLT named %q", name)
	}
	o.Enabled = enabled
	if !enabled {
		o.Status = StatusUnknown
		o.ConsecutiveFail = 0
	}
	err := m.save()
	m.mu.Unlock()
	if err == nil {
		m.notify()
	}
	return err
}

// UpdateCheckResult records the outcome of one ping and returns the OLT's
// state *before* this check, so callers can detect UP<->DOWN transitions
// without a separate read-then-write race.
func (m *Manager) UpdateCheckResult(name string, success bool, rtt time.Duration, checkErr error, threshold int) (prevStatus Status, newStatus Status, olt *OLT, err error) {
	m.mu.Lock()
	o := m.findLocked(name)
	if o == nil {
		m.mu.Unlock()
		return "", "", nil, fmt.Errorf("no OLT named %q", name)
	}
	prevStatus = o.Status
	now := time.Now()
	o.LastCheck = now

	if success {
		o.ConsecutiveFail = 0
		o.LastSuccess = now
		o.LastRTTMs = rtt.Milliseconds()
		o.LastError = ""
		o.Status = StatusUp
	} else {
		o.ConsecutiveFail++
		o.LastFailure = now
		if checkErr != nil {
			o.LastError = checkErr.Error()
		}
		if o.ConsecutiveFail >= threshold {
			o.Status = StatusDown
		}
		// Below threshold: status intentionally unchanged (still "up" or
		// "unknown") per the spec — a single failed ping must not flip state.
	}
	newStatus = o.Status
	saveErr := m.save()
	cp := *o
	m.mu.Unlock()

	return prevStatus, newStatus, &cp, saveErr
}

func (m *Manager) FilePath() string {
	return m.filePath
}

// NormalizePhoneNumber keeps only digits and returns them as +<digits>,
// converting a leading "00" international prefix to "+". Matches the
// nightcode-whatsapp config package's own normalization so a number entered
// here and one entered in that project behave identically.
func NormalizePhoneNumber(number string) string {
	number = strings.TrimSpace(number)
	hadPlus := strings.HasPrefix(number, "+")

	var b strings.Builder
	for _, r := range number {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if !hadPlus && strings.HasPrefix(digits, "00") {
		digits = digits[2:]
	}
	if digits == "" {
		return ""
	}
	return "+" + digits
}

// IsValidPhoneNumber reports whether number (as returned by
// NormalizePhoneNumber) looks like a plausible international number.
func IsValidPhoneNumber(number string) bool {
	if !strings.HasPrefix(number, "+") {
		return false
	}
	digits := strings.TrimPrefix(number, "+")
	if len(digits) < 7 || len(digits) > 15 || digits[0] == '0' {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
