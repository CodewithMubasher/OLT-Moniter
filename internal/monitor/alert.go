package monitor

import (
	"context"
	"fmt"
	"time"

	"olt-monitor/internal/oltconfig"
)

// Sender is the minimal capability the alert manager needs from the WhatsApp
// client: send a text message to the configured target. Kept as an interface
// so monitor doesn't import the whatsapp package directly, and so tests can
// use a fake.
type Sender interface {
	SendTo(ctx context.Context, target, text string) error
}

// AlertManager turns Transition events into the exact WhatsApp message
// formats from the spec, sent at most once per state change.
type AlertManager struct {
	sender Sender
	target func() string
}

func NewAlertManager(sender Sender, target func() string) *AlertManager {
	return &AlertManager{sender: sender, target: target}
}

// HandleTransition is wired as monitor.Engine.OnTransition. It returns an
// error when the WhatsApp delivery fails so the engine can surface it to the
// TUI, instead of silently losing the alert.
func (a *AlertManager) HandleTransition(t Transition) error {
	target := a.target()
	if target == "" {
		return nil // no WhatsApp target configured yet; nothing to notify
	}

	var text string
	switch t.To {
	case oltconfig.StatusDown:
		text = fmt.Sprintf(
			"🚨 OLT DOWN\n\nName: %s\nIP: %s\nTime: %s",
			t.OLT.Name, t.OLT.IP, time.Now().Format("15:04:05"),
		)
	case oltconfig.StatusUp:
		text = fmt.Sprintf(
			"✅ OLT RECOVERED\n\nName: %s\nIP: %s\nTime: %s",
			t.OLT.Name, t.OLT.IP, time.Now().Format("15:04:05"),
		)
	default:
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.sender.SendTo(ctx, target, text)
}
