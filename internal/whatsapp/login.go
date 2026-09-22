package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
)

// LoginTimeout bounds one whole linking attempt (WhatsApp stops emitting QR codes after ~160s).
const LoginTimeout = 170 * time.Second

// StartPhoneLogin runs the 8-character code flow. The code is delivered via the
// returned channel exactly once; errors come back on the same channel. WhatsApp
// still requires its QR channel to be acquired before connecting, even though
// this app deliberately does not show or use a QR code.
func (c *Client) StartPhoneLogin(ctx context.Context, phone string) <-chan PhoneCodeResult {
	out := make(chan PhoneCodeResult, 1)

	qrChan, err := c.GetQRChannel(ctx)
	if err != nil {
		out <- PhoneCodeResult{Err: fmt.Errorf("prepare pairing: %w", err)}
		return out
	}
	if err := c.Connect(); err != nil {
		out <- PhoneCodeResult{Err: fmt.Errorf("connect: %w", err)}
		return out
	}

	go c.drainQR(ctx, qrChan, func() {
		// First QR item means the socket is ready: request the pairing code now.
		pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		code, err := c.PairPhone(pctx, phone)
		out <- PhoneCodeResult{Code: code, Err: err}
	}, false)
	return out
}

type PhoneCodeResult struct {
	Code string
	Err  error
}

// drainQR consumes the QR channel until it closes. It MUST keep reading: whatsmeow
// closes the channel and disconnects the client if its non-blocking send finds it full.
func (c *Client) drainQR(ctx context.Context, ch <-chan whatsmeow.QRChannelItem, onFirst func(), emitCodes bool) {
	first := true
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-ch:
			if !ok {
				return
			}
			if first {
				first = false
				if onFirst != nil {
					go onFirst() // never block the drain loop
				}
			}
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				if emitCodes {
					c.emit(QRCodeEvent{Code: item.Code}, false)
				}
			case whatsmeow.QRChannelSuccess.Event:
				// PairSuccessEvent is emitted by the normal handler; nothing to add.
			case whatsmeow.QRChannelTimeout.Event:
				c.emit(PairErrorEvent{Err: errors.New("linking timed out — nothing was scanned/entered in time")}, true)
			case whatsmeow.QRChannelScannedWithoutMultidevice.Event:
				c.emit(PairErrorEvent{Err: errors.New("scanned by a phone without multi-device support; update WhatsApp")}, true)
			case whatsmeow.QRChannelClientOutdated.Event:
				c.emit(FatalEvent{Err: errors.New("WhatsApp says this client is outdated; update whatsmeow")}, true)
			case whatsmeow.QRChannelErrUnexpectedEvent.Event:
				c.emit(PairErrorEvent{Err: errors.New("unexpected connection state during linking (already linked?) — restart the app")}, true)
			case whatsmeow.QRChannelEventError:
				err := item.Error
				if err == nil {
					err = errors.New("unknown pairing error")
				}
				c.emit(PairErrorEvent{Err: err}, true)
			}
		}
	}
}
