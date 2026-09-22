// Package commands parses and executes the WhatsApp text-command protocol
// described in the spec (status / add / remove / enable / disable / interval
// / ping). It calls into the same oltconfig.Manager and monitor.Engine the
// TUI uses, so a command issued over WhatsApp and an action taken in the TUI
// go through identical code paths and are indistinguishable to the rest of
// the system.
package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"olt-monitor/internal/monitor"
	"olt-monitor/internal/oltconfig"
)

type Handler struct {
	cfg    *oltconfig.Manager
	engine *monitor.Engine
}

func NewHandler(cfg *oltconfig.Manager, engine *monitor.Engine) *Handler {
	return &Handler{cfg: cfg, engine: engine}
}

// Execute parses one line of text and returns the reply to send back.
// It never returns an error itself — user-facing problems (bad syntax,
// unknown OLT) come back as a reply string, exactly what a WhatsApp bot
// should send instead of failing silently.
func (h *Handler) Execute(ctx context.Context, text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return h.help()
	}
	cmd := strings.ToLower(fields[0])
	args := fields[1:]

	switch cmd {
	case "status":
		return h.status()
	case "add":
		return h.add(args)
	case "remove", "rm", "delete":
		return h.remove(args)
	case "enable":
		return h.setEnabled(args, true)
	case "disable":
		return h.setEnabled(args, false)
	case "interval":
		return h.interval(args)
	case "ping":
		return h.ping(ctx, args)
	case "help":
		return h.help()
	default:
		return fmt.Sprintf("Unknown command %q. Send \"help\" for the list of commands.", fields[0])
	}
}

func (h *Handler) help() string {
	return "OLT Monitor commands:\n" +
		"status - list all OLTs and their state\n" +
		"add <name> <ip> - add and start monitoring\n" +
		"remove <name> - remove an OLT\n" +
		"enable <name> / disable <name>\n" +
		"interval <seconds|10s|5m> - change check interval\n" +
		"ping <name> - check one OLT immediately"
}

func (h *Handler) status() string {
	olts := h.cfg.Snapshot()
	if len(olts) == 0 {
		return "No OLTs configured yet. Add one with: add <name> <ip>"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("OLT Status (interval: %s)\n", h.cfg.Interval()))
	for _, o := range olts {
		icon := "○"
		state := "unknown"
		switch {
		case !o.Enabled:
			icon, state = "○", "disabled"
		case o.Status == oltconfig.StatusUp:
			icon, state = "●", "up"
		case o.Status == oltconfig.StatusDown:
			icon, state = "●", "down"
		}
		b.WriteString(fmt.Sprintf("%s %-12s %-15s %s\n", icon, o.Name, o.IP, state))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (h *Handler) add(args []string) string {
	if len(args) != 2 {
		return "Usage: add <name> <ip>"
	}
	olt, err := h.cfg.AddOLT(args[0], args[1])
	if err != nil {
		return "Could not add OLT: " + err.Error()
	}
	h.engine.WakeNow()
	return fmt.Sprintf("Added %s (%s) and started monitoring.", olt.Name, olt.IP)
}

func (h *Handler) remove(args []string) string {
	if len(args) != 1 {
		return "Usage: remove <name>"
	}
	if err := h.cfg.RemoveOLT(args[0]); err != nil {
		return "Could not remove OLT: " + err.Error()
	}
	return fmt.Sprintf("Removed %s.", args[0])
}

func (h *Handler) setEnabled(args []string, enabled bool) string {
	if len(args) != 1 {
		verb := "enable"
		if !enabled {
			verb = "disable"
		}
		return fmt.Sprintf("Usage: %s <name>", verb)
	}
	if err := h.cfg.SetEnabled(args[0], enabled); err != nil {
		return "Could not update OLT: " + err.Error()
	}
	if enabled {
		h.engine.WakeNow()
		return fmt.Sprintf("%s enabled and will be checked shortly.", args[0])
	}
	return fmt.Sprintf("%s disabled.", args[0])
}

func (h *Handler) interval(args []string) string {
	if len(args) != 1 {
		return "Usage: interval <seconds|10s|30s|5m>"
	}
	d, err := ParseDuration(args[0])
	if err != nil {
		return "Could not parse interval: " + err.Error()
	}
	if err := h.cfg.SetInterval(d); err != nil {
		return "Could not set interval: " + err.Error()
	}
	h.engine.WakeNow()
	return fmt.Sprintf("Monitoring interval set to %s.", d)
}

func (h *Handler) ping(ctx context.Context, args []string) string {
	if len(args) != 1 {
		return "Usage: ping <name>"
	}
	res, err := h.engine.CheckNow(ctx, args[0])
	if err != nil {
		return "Could not ping: " + err.Error()
	}
	if res.Success {
		return fmt.Sprintf("%s (%s) is UP - %s", res.Name, res.IP, res.RTT)
	}
	msg := fmt.Sprintf("%s (%s) is DOWN", res.Name, res.IP)
	if res.Err != nil {
		msg += " - " + res.Err.Error()
	}
	return msg
}

// ParseDuration accepts a bare number of seconds ("30") or a Go-style
// duration ("30s", "5m", "1m30s"), per the spec's examples.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if secs, err := strconv.Atoi(s); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("use a number of seconds (e.g. 30) or a duration like 30s, 1m, 5m")
	}
	return d, nil
}
