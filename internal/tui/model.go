// Package tui implements the Bills OS dashboard: WhatsApp link status,
// the chat-selection screen for choosing what to monitor, and a live log
// of captured messages, built with Bubble Tea.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"bills-os/internal/config"
	"bills-os/internal/monitor"
	"bills-os/internal/portal"
	"bills-os/internal/whatsapp"
)

// Mode is which screen is active. ModeMain is the dashboard; the other
// modes replace it with a full-screen view (linking, chat selection).
type Mode int

const (
	ModeMain Mode = iota
	ModeWhatsAppPhoneInput
	ModeWhatsAppPhoneCode
	ModeChatSelect
)

type Model struct {
	cfg    *config.Manager
	wa     *whatsapp.Client
	waEvts <-chan any
	logger *monitor.Logger

	// Portal automation: queue of customer-opening jobs and the channel
	// carrying ResultEvents back for the status line. Nil when no portal
	// URL is configured / the browser failed to open.
	portal    *portal.Runner
	portalEv  <-chan any
	portalOK  int
	portalErr int

	mode   Mode
	input  string
	errMsg string
	status string // transient one-line status

	// Session counters (card 2). In-memory only: they reset every start,
	// while the JSON file keeps the full history across restarts.
	msgsReceived int
	msgsSaved    int

	// Message log shown in the table (most recent last, bounded).
	logRows      []LogRow
	logRowCursor int
	logSeq       int // running row id, never reset (survives trims)

	// Chat selection screen state.
	chats        []whatsapp.ChatInfo
	sel          map[string]bool // checkbox state, keyed by chat JID
	selCursor    int
	chatsLoading bool

	// Active monitoring set (chat JID -> display name), mirrored from
	// config so the message handler can filter without taking the lock.
	monitored map[string]string

	// WhatsApp linking state.
	waConnected       bool
	waOwnNumber       string
	waPairCode        string
	waLoginErr        string
	waHasSavedSession bool // true if DB has device data from a prior linking

	width, height int
	quitting      bool

	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg *config.Manager, wa *whatsapp.Client, waEvts <-chan any, logger *monitor.Logger, runner *portal.Runner) Model {
	ctx, cancel := context.WithCancel(context.Background())
	monitored := make(map[string]string)
	for _, c := range cfg.MonitoredChats() {
		monitored[c.JID] = c.Name
	}
	return Model{
		cfg:       cfg,
		wa:        wa,
		waEvts:    waEvts,
		logger:    logger,
		portal:    runner,
		portalEv:  runner.Events(),
		mode:      ModeMain,
		monitored: monitored,
		ctx:       ctx,
		cancel:    cancel,
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

	cmds := []tea.Cmd{
		waitForWAEvent(m.waEvts),
		waitForPortalEvent(m.portalEv),
		startLoginIfNeeded(m.wa, needsLogin),
	}
	// Linked already and nothing selected yet → ask for chats right away.
	// (When a selection was saved in config, we go straight to monitoring.)
	if m.wa != nil && m.wa.IsLoggedIn() && len(m.monitored) == 0 {
		cmds = append(cmds, func() tea.Msg { return startChatSelectMsg{} })
	}
	return tea.Batch(cmds...)
}

// --- tea.Msg types ---

type startLoginMsg struct{}
type startChatSelectMsg struct{}

// chatsLoadedMsg carries the contact/group list back from ListChats.
type chatsLoadedMsg struct {
	chats []whatsapp.ChatInfo
	err   error
}

// messageSavedMsg reports the result of one JSON append.
type messageSavedMsg struct{ err error }

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

// waitForPortalEvent is the portal-result twin of waitForWAEvent: a
// blocking channel read wrapped as a tea.Cmd, re-armed by its handler
// after every portal event (and never for other msg types, so exactly
// one listener stays in flight).
func waitForPortalEvent(ch <-chan any) tea.Cmd {
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
	return func() tea.Msg { return startLoginMsg{} }
}
