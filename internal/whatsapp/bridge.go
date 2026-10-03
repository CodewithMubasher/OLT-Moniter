package whatsapp

// bridge.go adds Bills OS-specific glue on top of the WhatsApp client
// (client.go, login.go hold the pairing, reconnect, and event-draining
// logic). It intentionally does not touch that logic — only adds the
// event fan-out and a convenience sender.

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow/types"
)

// NewEventFanOut creates a fan-out: one goroutine reads c.EventChan and
// copies every event to each subscriber channel returned by NewSubscriber,
// so multiple independent consumers (the TUI, for linking/connection
// display and monitoring, plus future automation) each see every event.
// Reading c.EventChan directly from more than one place would race,
// since a channel read is a one-time hand-off to whichever goroutine wins
// it — that is what the fan-out avoids.
//
// Call NewSubscriber for every consumer BEFORE calling Start, then call
// Start exactly once. It stops when ctx is cancelled.
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
// whatsmeow expects. The monitoring phase never calls it (no replies);
// it exists for the upcoming billing-automation actions.
func (c *Client) SendTo(ctx context.Context, target, text string) error {
	digits := DigitsOnly(target)
	if digits == "" {
		return fmt.Errorf("no WhatsApp target number configured")
	}
	jid := types.NewJID(digits, types.DefaultUserServer)
	_, err := c.SendTextMessage(ctx, jid, text)
	return err
}
