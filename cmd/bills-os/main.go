// Command bills-os runs the Bills OS TUI: it opens the ISP billing portal
// in Chrome, links one WhatsApp account (only asks when never linked
// before), lets the operator pick which chats to monitor, then captures
// every message from those chats into data/monitored_messages.json.
// No replies are ever sent in this phase.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"bills-os/internal/browser"
	"bills-os/internal/config"
	"bills-os/internal/monitor"
	"bills-os/internal/portal"
	"bills-os/internal/tui"
	"bills-os/internal/whatsapp"
)

// dataDirectory resolves where config and WhatsApp session data live.
//  1. BILLS_OS_DATA env var overrides everything.
//  2. Otherwise, the "data" folder next to the executable (not the working
//     directory), so double-clicking from any folder always uses the same
//     location.
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

	// --- Config: portal URL + monitored-chat selection ---
	cfg := config.NewManager(dataDir)
	if _, err := cfg.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- ISP portal in Chromium (Playwright, persistent profile) ---
	// Runs before the TUI starts so launch errors are visible on stderr.
	// The profile in <data>/browser-data keeps the portal login across
	// restarts; later phases drive the page through br.Page().
	br, err := browser.Launch(ctx, cfg.PortalURL(), dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open portal browser: %v\n", err)
	}
	defer br.Close()

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

	// Tap the event channel through a fan-out so additional consumers
	// (e.g. the upcoming browser automation) can subscribe without ever
	// racing the TUI on waClient.EventChan.
	fanOut := whatsapp.NewEventFanOut()
	tuiEvents := fanOut.NewSubscriber()
	fanOut.Start(ctx, waClient)

	// --- Message capture: only selected chats are appended to the JSON
	// log; nothing is ever sent back. ---
	logger := monitor.NewLogger(dataDir)

	// --- Portal automation: opens customers in the browser when monitored
	// messages mention a username (word with an underscore). ---
	var portalRunner *portal.Runner
	if br != nil {
		portalRunner = portal.NewRunner(br.Page(), cfg.PortalURL(), dataDir)
		portalRunner.Start(ctx)
	}

	// --- TUI ---
	model := tui.New(cfg, waClient, tuiEvents, logger, portalRunner)
	model.SetWAConnected(waAutoConnected)
	model.SetWAHasSavedSession(waHasSavedSession)
	program := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
