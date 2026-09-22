// Package tui implements the OLT Monitor dashboard: a live table of OLTs
// with add/edit/remove/enable/disable/ping actions and an interval editor,
// built with Bubble Tea in the same style as the reused nightcode-whatsapp
// TUI (same color palette, same "monitoring screen" feel).
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"olt-monitor/internal/monitor"
	"olt-monitor/internal/oltconfig"
	"olt-monitor/internal/whatsapp"
)

// Mode is which screen/overlay is active. The base table is always drawn;
// a mode other than ModeTable overlays a small form on top of it.
type Mode int

const (
	ModeTable Mode = iota
	ModeAddName
	ModeAddIP
	ModeEditName
	ModeEditIP
	ModeInterval
	ModeConfirmRemove
	ModeWhatsAppPhoneInput // pairing-code login only; QR login is disabled
	ModeWhatsAppPhoneCode
	ModeWhatsAppTarget // set which admin number receives alerts / can send commands
)

type Model struct {
	cfg    *oltconfig.Manager
	engine *monitor.Engine
	wa     *whatsapp.Client
	waEvts <-chan any

	olts     []*oltconfig.OLT
	cursor   int
	interval time.Duration
	lastTick time.Time

	mode   Mode
	input  string
	errMsg string
	status string // transient one-line status ("Pinged OLT-01: UP (12ms)")

	// Fields for the in-progress add/edit form.
	formName string
	formIP   string

	// WhatsApp linking state, mirrors nightcode's own login flow.
	waConnected     bool
	waOwnNumber     string
	waPhoneInput    string
	waPairCode      string
	waLoginErr      string
	waHasSavedSession bool // true if DB has device data from a prior linking

	width, height int
	quitting      bool

	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg *oltconfig.Manager, engine *monitor.Engine, wa *whatsapp.Client, waEvts <-chan any) Model {
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		cfg:      cfg,
		engine:   engine,
		wa:       wa,
		waEvts:   waEvts,
		olts:     cfg.Snapshot(),
		interval: cfg.Interval(),
		mode:     ModeTable,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// SetWAConnected is called from main after a successful auto-connect so
// the TUI starts in the "connected" state without waiting for an event.
func (m *Model) SetWAConnected(connected bool) {
	m.waConnected = connected
}

// SetWAHasSavedSession tells the TUI that a WhatsApp session exists on
// disk from a previous linking. Used to show "reconnecting…" instead of
// "press W to link" on startup.
func (m *Model) SetWAHasSavedSession(has bool) {
	m.waHasSavedSession = has
}

func (m Model) Init() tea.Cmd {
	// Only trigger the auto-login flow if the user has NEVER linked before
	// (no saved session in the DB). If a session exists, main.go already
	// called Connect(); the TUI will show "reconnecting…" until a
	// ConnectedEvent or LoggedOutEvent arrives.
	needsLogin := m.wa != nil && !m.wa.IsLoggedIn() && !m.waHasSavedSession
	return tea.Batch(
		waitForWAEvent(m.waEvts),
		tickEvery(),
		startLoginIfNeeded(m.wa, needsLogin),
	)
}

// --- tea.Msg types ---

type tickMsg time.Time

// PingResultMsg carries one completed ping into the TUI's event loop.
// Exported so main.go can forward monitor.Engine's OnResult callback into
// the running tea.Program via program.Send.
type PingResultMsg monitor.Result

type startLoginMsg struct{ needed bool }

func tickEvery() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func waitForWAEvent(ch <-chan any) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return nil
		}
		return evt
	}
}

func startLoginIfNeeded(c *whatsapp.Client, needed bool) tea.Cmd {
	if !needed || c == nil {
		return nil
	}
	return func() tea.Msg { return startLoginMsg{needed: true} }
}
