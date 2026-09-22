// Command olt-monitor runs the OLT Monitor TUI: continuous ICMP monitoring
// of configured OLTs, WhatsApp alerts on state changes, and remote control
// via WhatsApp commands, all sharing one on-disk configuration.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"olt-monitor/internal/commands"
	"olt-monitor/internal/monitor"
	"olt-monitor/internal/oltconfig"
	"olt-monitor/internal/tui"
	"olt-monitor/internal/whatsapp"
)

// dataDirectory resolves where config and WhatsApp session data live.
// 1. OLT_MONITOR_DATA env var overrides everything.
// 2. Otherwise, the "data" folder next to the executable (not the working
//    directory), so double-clicking from any folder always uses the same
//    location.
func dataDirectory() string {
	if d := os.Getenv("OLT_MONITOR_DATA"); d != "" {
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

	// --- Config: single source of truth for the TUI and WhatsApp commands ---
	cfg := oltconfig.NewManager(dataDir)
	if _, err := cfg.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Monitoring engine ---
	engine := monitor.NewEngine(cfg)
	cfg.OnChange(engine.WakeNow) // interval/OLT changes from either the TUI or WhatsApp take effect immediately

	// --- WhatsApp client (reused nightcode-whatsapp packages, unmodified) ---
	waClient := whatsapp.NewClient(dataDir)
	if err := waClient.Initialize(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize WhatsApp client: %v\n", err)
		os.Exit(1)
	}
	defer waClient.Close()

	if target := cfg.WhatsAppTarget(); target != "" {
		waClient.SetTarget(target)
	}

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

	// Fan out WhatsApp events to two independent consumers: the TUI (QR /
	// connection status) and the command loop (inbound messages). Neither
	// reads waClient.EventChan directly to avoid racing on that channel.
	fanOut := whatsapp.NewEventFanOut()
	tuiEvents := fanOut.NewSubscriber()
	cmdEvents := fanOut.NewSubscriber()
	fanOut.Start(ctx, waClient)

	// --- Alerts: exactly one DOWN and one RECOVERED message per outage ---
	alerts := monitor.NewAlertManager(waClient, cfg.WhatsAppTarget)
	engine.OnTransition(alerts.HandleTransition)

	// --- WhatsApp remote-control commands, same code path as the TUI ---
	cmdHandler := commands.NewHandler(cfg, engine)
	go whatsapp.RunCommandLoop(ctx, waClient, cmdEvents, cmdHandler, nil)

	// --- TUI ---
	model := tui.New(cfg, engine, waClient, tuiEvents)
	model.SetWAConnected(waAutoConnected)
	model.SetWAHasSavedSession(waHasSavedSession)
	program := tea.NewProgram(model, tea.WithAltScreen())

	// Feed every completed ping into the TUI so background monitoring
	// (not just manual "p" pings) updates the table live.
	engine.OnResult(func(r monitor.Result) {
		program.Send(tui.PingResultMsg(r))
	})

	go engine.Run(ctx)

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
