package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"bills-os/internal/config"
	"bills-os/internal/whatsapp"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case startLoginMsg:
		// Only show the phone-input screen for genuinely first-time linking.
		// If a saved session exists, main.go already called Connect() and
		// the TUI waits for ConnectedEvent / LoggedOutEvent instead.
		if m.waHasSavedSession {
			return m, waitForWAEvent(m.waEvts)
		}
		m.mode = ModeWhatsAppPhoneInput
		m.input, m.errMsg = "", ""
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.QRCodeEvent:
		// Pairing-code login deliberately never renders a QR code. This event
		// can still be emitted by a connected WhatsApp client, so keep the
		// event loop alive without changing the current screen.
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.PairSuccessEvent:
		m.waConnected = true
		m.waOwnNumber = "+" + msg.JID
		m.mode = ModeMain
		m.status = "WhatsApp linked"
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.PairErrorEvent:
		m.waLoginErr = msg.Err.Error()
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.ConnectedEvent:
		m.waConnected = true
		if m.wa != nil {
			m.waOwnNumber = m.wa.GetOwnJID()
		}
		m.status = "WhatsApp connected"
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.DisconnectedEvent:
		m.waConnected = false
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.LoggedOutEvent:
		m.waConnected = false
		m.waHasSavedSession = false // session was invalidated server-side
		m.waLoginErr = "Logged out: " + msg.Reason + "  — press W to re-link"
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.FatalEvent:
		m.waLoginErr = msg.Err.Error()
		return m, waitForWAEvent(m.waEvts)

	case waPhoneCodeMsg:
		m.waPairCode = string(msg)
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.MessageReceivedEvent:
		// The reply itself is sent by RunAutoReplyLoop (see main.go); the
		// TUI just counts the message and keeps listening for events.
		settings := m.cfg.Snapshot()
		m.msgsReceived++
		if settings.AutoReplyPaused {
			m.msgsIgnored++
			m.status = "Message from " + msg.Sender + " — paused, no reply sent"
		} else {
			m.msgsReplied++
			m.status = "Replied to " + msg.Sender
		}
		return m, waitForWAEvent(m.waEvts)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case ModeMain:
		return m.handleMainKey(msg)
	case ModeEditMessage, ModeWhatsAppPhoneInput:
		return m.handleTextInputKey(msg)
	case ModeWhatsAppPhoneCode:
		return m.handleWALoginKey(msg)
	}
	return m, nil
}

func (m Model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		m.cancel()
		return m, tea.Quit

	case "up", "k":
		if m.rowCursor > 0 {
			m.rowCursor--
		}
		return m, nil

	case "down", "j":
		if m.rowCursor < len(m.rows)-1 {
			m.rowCursor++
		}
		return m, nil

	case "w":
		if m.wa == nil {
			return m, nil
		}
		if !m.wa.IsLoggedIn() {
			m.mode = ModeWhatsAppPhoneInput
			m.input, m.errMsg, m.waLoginErr = "", "", ""
			return m, nil
		}
		m.status = "WhatsApp already linked as " + m.wa.GetOwnJID()
		return m, nil

	case "m":
		m.mode = ModeEditMessage
		m.input = m.cfg.Snapshot().AutoReplyMessage
		m.errMsg = ""
		return m, nil

	case "t":
		paused := !m.cfg.Snapshot().AutoReplyPaused
		if err := m.cfg.SetAutoReplyPaused(paused); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		if paused {
			m.status = "Auto-reply PAUSED — messages received but ignored"
		} else {
			m.status = "Auto-reply resumed"
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleTextInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeMain
		m.errMsg = ""
		return m, nil

	case tea.KeyEnter:
		return m.submitTextInput()

	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	case tea.KeyRunes:
		m.input += string(msg.Runes)
		return m, nil

	case tea.KeySpace:
		// KeySpace carries a zero rune in Bubble Tea. Converting its Runes
		// slice to a string inserted a hidden NUL character into the input.
		m.input += " "
		return m, nil
	}
	return m, nil
}

func (m Model) submitTextInput() (tea.Model, tea.Cmd) {
	switch m.mode {
	case ModeEditMessage:
		if err := m.cfg.SetAutoReplyMessage(m.input); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.mode = ModeMain
		m.status = "Auto-reply message updated"
		return m, nil

	case ModeWhatsAppPhoneInput:
		if m.input == "" {
			m.errMsg = "Enter a phone number in international format"
			return m, nil
		}
		phone := m.input
		m.waPhoneInput = phone
		m.mode = ModeWhatsAppPhoneCode
		return m, func() tea.Msg {
			res := <-m.wa.StartPhoneLogin(m.ctx, phone)
			if res.Err != nil {
				return whatsapp.PairErrorEvent{Err: res.Err}
			}
			return waPhoneCodeMsg(res.Code)
		}
	}
	return m, nil
}

type waPhoneCodeMsg string

func (m Model) handleWALoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = ModeMain
		return m, nil
	}
	return m, nil
}

// settings is a small helper used by the view layer.
func (m Model) settings() config.WhatsAppSettings {
	return m.cfg.Snapshot()
}
