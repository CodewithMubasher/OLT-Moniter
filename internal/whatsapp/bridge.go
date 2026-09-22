package whatsapp

// bridge.go adds OLT Monitor-specific glue on top of the reused NightCode
// WhatsApp client (client.go, login.go are unmodified from the working
// nightcode-whatsapp project). It intentionally does not touch the pairing,
// reconnect, or event-draining logic above — only adds a convenience sender
// and a way for the app to react to inbound commands.

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow/types"
)

// StartFanOut launches the single goroutine allowed to read c.EventChan and
// copies every event to each subscriber channel returned by NewSubscriber,
// so multiple independent consumers (the TUI, for connection/QR display, and
// the command loop, for inbound WhatsApp commands) each see every event.
// Reading c.EventChan directly from more than one place would race, since a
// channel read is a one-time hand-off to whichever goroutine wins it — that
// is what StartFanOut/NewSubscriber avoid.
//
// Call NewSubscriber for every consumer BEFORE calling StartFanOut, then call
// StartFanOut exactly once. It stops when ctx is cancelled.
type fanOut struct {
	subs []chan any
}

func newFanOut() *fanOut { return &fanOut{} }

func (f *fanOut) NewSubscriber() <-chan any {
	ch := make(chan any, eventBufferSize)
	f.subs = append(f.subs, ch)
	return ch
}

func (f *fanOut) run(ctx context.Context, c *Client) {
	defer func() {
		for _, ch := range f.subs {
			close(ch)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-c.EventChan:
			if !ok {
				return
			}
			for _, ch := range f.subs {
				select {
				case ch <- evt:
				case <-ctx.Done():
					return
				default:
					// A slow subscriber drops the event rather than blocking
					// the others or the whatsmeow socket goroutine upstream.
				}
			}
		}
	}
}

// EventFanOut is the exported handle: create it, take as many subscriber
// channels as needed, then Start it once.
type EventFanOut struct{ f *fanOut }

func NewEventFanOut() *EventFanOut { return &EventFanOut{f: newFanOut()} }

func (e *EventFanOut) NewSubscriber() <-chan any { return e.f.NewSubscriber() }

func (e *EventFanOut) Start(ctx context.Context, c *Client) { go e.f.run(ctx, c) }

// SendTo sends a text message to a phone number in international format
// (e.g. "+923001234567"), building the JID the same way the rest of
// whatsmeow expects. Satisfies monitor.Sender.
func (c *Client) SendTo(ctx context.Context, target, text string) error {
	digits := DigitsOnly(target)
	if digits == "" {
		return fmt.Errorf("no WhatsApp target number configured")
	}
	jid := types.NewJID(digits, types.DefaultUserServer)
	_, err := c.SendTextMessage(ctx, jid, text)
	return err
}

// CommandExecutor is implemented by commands.Handler; kept as a small
// interface here so this package doesn't import commands (which imports
// monitor and oltconfig) and create a dependency cycle.
type CommandExecutor interface {
	Execute(ctx context.Context, text string) string
}

// RunCommandLoop consumes events from a subscriber channel (see
// EventFanOut.NewSubscriber) and reacts to
// MessageReceivedEvent by executing it as a command and sending the reply.
// All other event types are ignored here — the TUI's own loop, reading the
// same tapped channel, handles QR/connection-status display.
//
// It only ever sees messages from the configured target phone, because
// Client.handleIncomingMessage already filters by SetTarget before anything
// reaches EventChan.
func RunCommandLoop(ctx context.Context, c *Client, events <-chan any, exec CommandExecutor, onReply func(sentText string)) {
	for {
		select {
		case <-ctx.Done():
			return
		case raw, ok := <-events:
			if !ok {
				return
			}
			evt, ok := raw.(MessageReceivedEvent)
			if !ok {
				continue
			}
			reply := exec.Execute(ctx, evt.Text)
			if err := c.SendTo(ctx, evt.Sender, reply); err == nil && onReply != nil {
				onReply(reply)
			}
		}
	}
}
