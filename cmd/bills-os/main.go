// Command bills-os runs the Bills OS TUI: link one WhatsApp account and
// auto-reply to every incoming message with a message you configure —
// changeable at any time from the TUI, no restart needed.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"bills-os/internal/config"
	"bills-os/internal/tui"
	"bills-os/internal/whatsapp"
)

// dataDirectory resolves where config and WhatsApp session data live.
// 1. BILLS_OS_DATA env var overrides everything.
// 2. Otherwise, the "data" folder next to the executable (not the working
//    directory), so double-clicking from any folder always uses the same
//    location.
func dataDirectory() string {
	if d := os.Getenv("BILLS_OS_DATA"); d != "" {
		return d
	}
	// Resolve the directory the executable itself lives in, not cwd.
	exe, err := os.Executable()
	if err != nil {
		// Fallback: use cwd (shouldn't happen in normal operation).
		return "data"
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

func main() {
	dataDir, err := filepath.Abs(dataDirectory())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve data directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create data directory: %v\n", err)
		os.Exit(1)
	}

	// --- Config: single source of truth for the auto-reply message ---
	cfg := config.NewManager(dataDir)
	if _, err := cfg.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- WhatsApp client ---
	waClient := whatsapp.NewClient(dataDir)
	if err := waClient.Initialize(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize WhatsApp client: %v\n", err)
		os.Exit(1)
	}
	defer waClient.Close()

	// Determine the initial WhatsApp connection state.
	// If we have a saved session (DB has device data), try to reconnect
	// silently instead of asking for re-link.
	waHasSavedSession := waClient.HasSavedSession(ctx)
	waAutoConnected := false
	if waClient.IsLoggedIn() {
		if err := waClient.Connect(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: WhatsApp connect failed: %v\n", err)
		} else {
			waAutoConnected = true
		}
	}

	// Fan out WhatsApp events to two independent consumers: the TUI
	// (linking/connection status) and the auto-reply loop (inbound
	// messages). Neither reads waClient.EventChan directly to avoid racing
	// on that channel.
	fanOut := whatsapp.NewEventFanOut()
	tuiEvents := fanOut.NewSubscriber()
	replyEvents := fanOut.NewSubscriber()
	fanOut.Start(ctx, waClient)

	// --- Auto-reply: reply to every incoming message with the configured
	// text, unless replies are paused. The settings are re-read per message,
	// so TUI edits (and pause/resume) apply to the very next message. ---
	go whatsapp.RunAutoReplyLoop(ctx, waClient, replyEvents, func() (string, bool) {
		s := cfg.Snapshot()
		return s.AutoReplyMessage, !s.AutoReplyPaused
	})

	// --- TUI ---
	model := tui.New(cfg, waClient, tuiEvents)
	model.SetWAConnected(waAutoConnected)
	model.SetWAHasSavedSession(waHasSavedSession)
	program := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
