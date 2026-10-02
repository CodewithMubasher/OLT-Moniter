// Package tui implements the Bills OS dashboard: WhatsApp link status and
// the auto-reply message settings, built with Bubble Tea.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"bills-os/internal/config"
	"bills-os/internal/whatsapp"
)

// Mode is which screen/overlay is active. The main status panel is always
// drawn; a mode other than ModeMain overlays a small form on top of it.
type Mode int

const (
	ModeMain Mode = iota
	ModeWhatsAppPhoneInput
	ModeWhatsAppPhoneCode
	ModeEditMessage
)

type Model struct {
	cfg    *config.Manager
	wa     *whatsapp.Client
	waEvts <-chan any

	mode   Mode
	input  string
	errMsg string
	status string // transient one-line status

	// Session message counters (card 2). In-memory only: they reset every
	// time the app starts.
	msgsReceived int
	msgsReplied  int
	msgsIgnored  int

	// Card 3 table: fake rows for now, plus the selection cursor.
	rows      []TableRow
	rowCursor int

	// WhatsApp linking state.
	waConnected       bool
	waOwnNumber       string
	waPhoneInput      string
	waPairCode        string
	waLoginErr        string
	waHasSavedSession bool // true if DB has device data from a prior linking

	width, height int
	quitting      bool

	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg *config.Manager, wa *whatsapp.Client, waEvts <-chan any) Model {
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		cfg:    cfg,
		wa:     wa,
		waEvts: waEvts,
		mode:   ModeMain,
		rows:   fakeRows(8),
		ctx:    ctx,
		cancel: cancel,
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
		startLoginIfNeeded(m.wa, needsLogin),
	)
}

// --- tea.Msg types ---

type startLoginMsg struct{ needed bool }

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
