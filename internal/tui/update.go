package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"olt-monitor/internal/commands"
	"olt-monitor/internal/oltconfig"
	"olt-monitor/internal/whatsapp"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.olts = m.cfg.Snapshot()
		m.interval = m.cfg.Interval()
		// Surface any alert delivery failure from the previous tick so the
		// operator sees it in the status bar instead of it being silently lost.
		if alertErr := m.engine.AlertError(); alertErr != nil {
			m.status = fmt.Sprintf("⚠  Alert failed: %s", alertErr)
		}
		return m, tickEvery()

	case PingResultMsg:
		m.olts = m.cfg.Snapshot()
		m.lastTick = msg.Timestamp
		if msg.Success {
			m.status = fmt.Sprintf("Pinged %s: UP (%s)", msg.Name, msg.RTT)
		} else {
			m.status = fmt.Sprintf("Pinged %s: DOWN", msg.Name)
		}
		return m, nil

	case startLoginMsg:
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
		if m.cfg.WhatsAppTarget() == "" {
			// Linked, but no admin number set yet to receive alerts/send
			// commands — ask for it now rather than silently going without.
			m.mode = ModeWhatsAppTarget
			m.input = ""
		} else {
			m.mode = ModeTable
		}
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.PairErrorEvent:
		m.waLoginErr = msg.Err.Error()
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.ConnectedEvent:
		m.waConnected = true
		if m.wa != nil {
			m.waOwnNumber = m.wa.GetOwnJID()
		}
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.DisconnectedEvent:
		m.waConnected = false
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.LoggedOutEvent:
		m.waConnected = false
		m.waLoginErr = "Logged out: " + msg.Reason
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.FatalEvent:
		m.waLoginErr = msg.Err.Error()
		return m, waitForWAEvent(m.waEvts)

	case waPhoneCodeMsg:
		m.waPairCode = string(msg)
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.MessageReceivedEvent:
		// Commands are executed by the separate RunCommandLoop goroutine
		// (see cmd/olt-monitor/main.go); the TUI just needs to keep
		// listening for further connection/QR events on this channel.
		return m, waitForWAEvent(m.waEvts)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case ModeTable:
		return m.handleTableKey(msg)
	case ModeAddName, ModeAddIP, ModeEditName, ModeEditIP, ModeInterval, ModeWhatsAppPhoneInput, ModeWhatsAppTarget:
		return m.handleTextInputKey(msg)
	case ModeConfirmRemove:
		return m.handleConfirmRemoveKey(msg)
	case ModeWhatsAppPhoneCode:
		return m.handleWALoginKey(msg)
	}
	return m, nil
}

func (m Model) handleTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		m.cancel()
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "down", "j":
		if m.cursor < len(m.olts)-1 {
			m.cursor++
		}
		return m, nil

	case "a":
		m.mode = ModeAddName
		m.formName, m.formIP, m.input, m.errMsg = "", "", "", ""
		return m, nil

	case "e":
		if o := m.selected(); o != nil {
			m.mode = ModeEditName
			m.formName, m.formIP = o.Name, o.IP
			m.input = o.Name
			m.errMsg = ""
		}
		return m, nil

	case "r":
		if m.selected() != nil {
			m.mode = ModeConfirmRemove
		}
		return m, nil

	case "d":
		if o := m.selected(); o != nil {
			_ = m.cfg.SetEnabled(o.Name, !o.Enabled)
			m.engine.WakeNow()
			m.olts = m.cfg.Snapshot()
		}
		return m, nil

	case "p":
		if o := m.selected(); o != nil {
			name := o.Name
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
				defer cancel()
				res, err := m.engine.CheckNow(ctx, name)
				if err != nil {
					return nil
				}
				return PingResultMsg(res)
			}
		}
		return m, nil

	case "i":
		m.mode = ModeInterval
		m.input = ""
		m.errMsg = ""
		return m, nil

	case "w":
		if m.wa == nil {
			return m, nil
		}
		if !m.wa.IsLoggedIn() {
			m.mode = ModeWhatsAppPhoneInput
			m.input, m.errMsg = "", ""
			return m, nil
		}
		// Already linked: let the admin (re)set which number gets alerts/commands.
		m.mode = ModeWhatsAppTarget
		m.input = m.cfg.WhatsAppTarget()
		m.errMsg = ""
		return m, nil
	}
	return m, nil
}

func (m Model) selected() *oltconfig.OLT {
	if m.cursor < 0 || m.cursor >= len(m.olts) {
		return nil
	}
	return m.olts[m.cursor]
}

func (m Model) handleTextInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeTable
		m.errMsg = ""
		return m, nil

	case tea.KeyEnter:
		return m.submitTextInput()

	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	case tea.KeyTab:
		if m.mode == ModeAddName && m.input != "" {
			m.formName = m.input
			m.mode = ModeAddIP
			m.input = ""
		} else if m.mode == ModeEditName {
			m.formName = m.input
			m.mode = ModeEditIP
			m.input = m.formIP
		}
		return m, nil

	case tea.KeyRunes:
		m.input += string(msg.Runes)
		return m, nil

	case tea.KeySpace:
		// KeySpace carries a zero rune in Bubble Tea. Converting its Runes
		// slice to a string inserted a hidden NUL character into URL targets.
		m.input += " "
		return m, nil
	}
	return m, nil
}

func (m Model) submitTextInput() (tea.Model, tea.Cmd) {
	switch m.mode {
	case ModeAddName:
		if m.input == "" {
			m.errMsg = "Name cannot be empty"
			return m, nil
		}
		m.formName = m.input
		m.mode = ModeAddIP
		m.input = ""
		m.errMsg = ""
		return m, nil

	case ModeAddIP:
		if m.input == "" {
			m.errMsg = "Target cannot be empty"
			return m, nil
		}
		_, err := m.cfg.AddOLT(m.formName, m.input)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.engine.WakeNow()
		m.olts = m.cfg.Snapshot()
		m.mode = ModeTable
		m.status = fmt.Sprintf("Added %s (%s)", m.formName, m.input)
		return m, nil

	case ModeEditName:
		m.formName = m.input
		m.mode = ModeEditIP
		m.input = m.formIP
		return m, nil

	case ModeEditIP:
		o := m.selected()
		if o == nil {
			m.mode = ModeTable
			return m, nil
		}
		if err := m.cfg.EditOLT(o.Name, m.formName, m.input); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.olts = m.cfg.Snapshot()
		m.mode = ModeTable
		m.status = "Updated " + m.formName
		return m, nil

	case ModeInterval:
		d, err := commands.ParseDuration(m.input)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		if err := m.cfg.SetInterval(d); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.engine.WakeNow()
		m.interval = d
		m.mode = ModeTable
		m.status = "Interval set to " + d.String()
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

	case ModeWhatsAppTarget:
		if m.input == "" {
			m.errMsg = "Enter the admin phone number in international format"
			return m, nil
		}
		normalized := oltconfig.NormalizePhoneNumber(m.input)
		if !oltconfig.IsValidPhoneNumber(normalized) {
			m.errMsg = "Invalid number — use international format, e.g. +923001234567"
			return m, nil
		}
		if err := m.cfg.SetWhatsAppTarget(normalized); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		if m.wa != nil {
			m.wa.SetTarget(normalized)
		}
		m.mode = ModeTable
		m.status = "WhatsApp alerts/commands will use " + normalized
		return m, nil
	}
	return m, nil
}

type waPhoneCodeMsg string

func (m Model) handleConfirmRemoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if o := m.selected(); o != nil {
			_ = m.cfg.RemoveOLT(o.Name)
			m.olts = m.cfg.Snapshot()
			if m.cursor >= len(m.olts) && m.cursor > 0 {
				m.cursor--
			}
			m.status = "Removed " + o.Name
		}
		m.mode = ModeTable
		return m, nil
	case "n", "esc":
		m.mode = ModeTable
		return m, nil
	}
	return m, nil
}

func (m Model) handleWALoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = ModeTable
		return m, nil
	}
	return m, nil
}
