package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"bills-os/internal/config"
	"bills-os/internal/monitor"
	"bills-os/internal/portal"
	"bills-os/internal/whatsapp"
)

// loadChatsTimeout bounds the ListChats round-trip (group list from the
// WhatsApp server).
const loadChatsTimeout = 30 * time.Second

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case startLoginMsg:
		// Only show the phone-input screen for genuinely first-time linking.
		// If a saved session exists, main.go already called Connect() and
		// the TUI waits for ConnectedEvent / LoggedOutEvent instead.
		// (No listener re-arm here: this msg comes from a tea.Cmd, the
		// in-flight waitForWAEvent is untouched.)
		if m.waHasSavedSession {
			return m, nil
		}
		m.mode = ModeWhatsAppPhoneInput
		m.input, m.errMsg = "", ""
		return m, nil

	case startChatSelectMsg:
		// First run after linking with nothing selected yet: open the
		// picker. If a selection exists in config, stay on the dashboard.
		if m.mode == ModeMain && len(m.monitored) == 0 && m.waConnected {
			return m.enterChatSelect()
		}
		return m, nil

	case chatsLoadedMsg:
		m.chatsLoading = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.chats = msg.chats
		if len(m.chats) == 0 {
			m.errMsg = "No groups or contacts found on this account"
		} else {
			m.errMsg = ""
		}
		return m, nil

	case messageSavedMsg:
		if msg.err != nil {
			m.errMsg = "Save failed: " + msg.err.Error()
		}
		return m, nil

	case portal.ResultEvent:
		// Finished opening one customer in the browser (consumed from the
		// portal channel, so THAT listener is the one re-armed here).
		if msg.Err != nil {
			m.portalErr++
			m.errMsg = "Portal: " + msg.String()
		} else {
			m.portalOK++
			m.status = "Portal: " + msg.String()
		}
		// Reflect the outcome in every log row that queued this username:
		// FAIL on error, PARTIAL when the profile opened but required
		// fields were missing, otherwise DONE.
		newStatus := StatusDone
		switch {
		case msg.Err != nil:
			newStatus = StatusFail
		case len(msg.Missing) > 0:
			newStatus = StatusPartial
		}
		for i, row := range m.logRows {
			for _, u := range row.Usernames {
				if strings.EqualFold(u, msg.Username) {
					row.Status = newStatus
					m.logRows[i] = row
					break
				}
			}
		}
		return m, waitForPortalEvent(m.portalEv)

	case whatsapp.QRCodeEvent:
		// Pairing-code login deliberately never renders a QR code. This event
		// can still be emitted by a connected WhatsApp client, so keep the
		// event loop alive without changing the current screen.
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.PairSuccessEvent:
		m.waConnected = true
		m.waOwnNumber = "+" + msg.JID
		m.status = "WhatsApp linked"
		return m.maybePromptChatSelect()

	case whatsapp.PairErrorEvent:
		m.waLoginErr = msg.Err.Error()
		return m, waitForWAEvent(m.waEvts)

	case whatsapp.ConnectedEvent:
		m.waConnected = true
		if m.wa != nil {
			m.waOwnNumber = m.wa.GetOwnJID()
		}
		m.status = "WhatsApp connected"
		return m.maybePromptChatSelect()

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
		return m, nil

	case waPhoneLoginErrMsg:
		m.waLoginErr = msg.err.Error()
		if m.mode == ModeWhatsAppPhoneInput {
			m.mode = ModeWhatsAppPhoneCode
		}
		return m, nil

	case whatsapp.MessageReceivedEvent:
		m.msgsReceived++
		chat := msg.Chat.String()
		chatName, monitored := m.monitored[chat]
		if !monitored {
			// Not on the operator's list — counted as received, never saved.
			return m, waitForWAEvent(m.waEvts)
		}
		// Capture filter: only messages that mention a customer username
		// are stored in the JSON log, shown in the table, and queued for
		// the portal. Ordinary chatter and "[non-text message]" markers
		// are ignored entirely.
		if msg.Text == "" || msg.Text == "[non-text message]" {
			return m, waitForWAEvent(m.waEvts)
		}
		usernames := portal.ExtractUsernames(msg.Text)
		if len(usernames) == 0 {
			return m, waitForWAEvent(m.waEvts)
		}
		if chatName == "" || chatName == chat {
			chatName = msg.ChatName
		}
		if chatName == "" {
			chatName = chat
		}

		m.msgsSaved++
		m.status = "Saved from " + chatName
		// Status starts as WAIT and flips to DONE/FAIL when the portal
		// ResultEvent for this username arrives (FAIL immediately when
		// there is no browser to ever produce a result).
		status := StatusWait
		if m.portal == nil {
			status = StatusFail
		}
		sender := msg.Sender
		if msg.SenderName != "" {
			sender = msg.SenderName
		}
		m.logSeq++
		m.logRows = appendLogRow(m.logRows, LogRow{
			ID:        m.logSeq,
			Time:      msg.Timestamp.Format("15:04"),
			Sender:    sender,
			Text:      msg.Text,
			Panel:     m.cfg.Panel(),
			Status:    status,
			Usernames: usernames,
		})
		cmds := []tea.Cmd{
			waitForWAEvent(m.waEvts),
			m.saveMessageCmd(msg, chatName),
		}
		if s := m.queuePortalJobs(msg, chatName, usernames); s != "" {
			m.status = s
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// queuePortalJobs queues each already-extracted customer username for the
// browser flow (search → open profile). Returns a status line when
// something was queued, or when usernames were found but the portal
// browser isn't running.
func (m Model) queuePortalJobs(msg whatsapp.MessageReceivedEvent, chatName string, usernames []string) string {
	if len(usernames) == 0 {
		return ""
	}
	if m.portal == nil {
		return "Found " + strings.Join(usernames, ", ") + " — portal browser is not open"
	}
	last := ""
	for _, u := range usernames {
		job := portal.Job{
			Username: u,
			Chat:     msg.Chat.String(),
			ChatName: chatName,
			MsgID:    msg.MsgID,
		}
		if m.portal.Enqueue(job) {
			last = "Queued " + u + " for activation"
		}
	}
	return last
}

// maybePromptChatSelect opens the chat picker right after linking when no
// selection has been saved yet; otherwise it just keeps the event loop
// alive. This path consumed a WhatsApp event, so the listener is
// re-armed here alongside the picker's chat load.
func (m Model) maybePromptChatSelect() (tea.Model, tea.Cmd) {
	if m.mode == ModeMain && len(m.monitored) == 0 {
		next, cmd := m.enterChatSelect()
		return next, tea.Batch(waitForWAEvent(m.waEvts), cmd)
	}
	return m, waitForWAEvent(m.waEvts)
}

// enterChatSelect switches to the picker and loads groups + contacts.
// Callers must re-arm the event listener themselves when they consumed a
// WhatsApp event (see maybePromptChatSelect) — this helper adds no
// listener, so repeated open/close cycles never accumulate goroutines.
func (m Model) enterChatSelect() (tea.Model, tea.Cmd) {
	m.mode = ModeChatSelect
	m.errMsg = ""
	m.chatsLoading = true
	m.chats = nil
	m.selCursor = 0
	m.sel = make(map[string]bool, len(m.monitored))
	for jid := range m.monitored {
		m.sel[jid] = true
	}
	return m, m.loadChatsCmd()
}

// loadChatsCmd fetches groups + contacts off the UI thread.
func (m Model) loadChatsCmd() tea.Cmd {
	if m.wa == nil {
		return func() tea.Msg { return chatsLoadedMsg{err: fmt.Errorf("WhatsApp client not available")} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, loadChatsTimeout)
		defer cancel()
		chats, err := m.wa.ListChats(ctx)
		return chatsLoadedMsg{chats: chats, err: err}
	}
}

// saveMessageMsg persists one captured message to monitored_messages.json.
func (m Model) saveMessageCmd(evt whatsapp.MessageReceivedEvent, chatName string) tea.Cmd {
	if m.logger == nil {
		return nil
	}
	return func() tea.Msg {
		err := m.logger.Append(monitor.Message{
			Timestamp:  evt.Timestamp,
			Chat:       evt.Chat.String(),
			ChatName:   chatName,
			Sender:     evt.Sender,
			SenderName: evt.SenderName,
			Text:       evt.Text,
			MsgID:      evt.MsgID,
			IsGroup:    evt.IsGroup,
		})
		return messageSavedMsg{err: err}
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case ModeMain:
		return m.handleMainKey(msg)
	case ModeChatSelect:
		return m.handleChatSelectKey(msg)
	case ModeWhatsAppPhoneInput:
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
		if m.logRowCursor > 0 {
			m.logRowCursor--
		}
		return m, nil

	case "down", "j":
		if m.logRowCursor < len(m.logRows)-1 {
			m.logRowCursor++
		}
		return m, nil

	case "s":
		if m.wa == nil || !m.wa.IsLoggedIn() {
			m.errMsg = "Link WhatsApp first (W)"
			return m, nil
		}
		return m.enterChatSelect()

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
	}
	return m, nil
}

// handleChatSelectKey drives the multi-select list: ↑↓ move, space
// toggles, a selects everything, enter confirms, esc backs out.
func (m Model) handleChatSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		m.cancel()
		return m, tea.Quit

	case "r":
		// Retry the chat load (e.g. it failed because the socket was
		// still connecting right after startup).
		if m.chatsLoading {
			return m, nil
		}
		m.errMsg = ""
		m.chatsLoading = true
		m.chats = nil
		return m, m.loadChatsCmd()

	case "esc":
		m.mode = ModeMain
		m.errMsg = ""
		if len(m.monitored) == 0 {
			m.status = "No chats selected — press S to choose what to monitor"
		}
		return m, nil

	case "up", "k":
		if m.selCursor > 0 {
			m.selCursor--
		}
		return m, nil

	case "down", "j":
		if m.selCursor < len(m.chats)-1 {
			m.selCursor++
		}
		return m, nil

	case " ":
		if len(m.chats) == 0 {
			return m, nil
		}
		jid := m.chats[m.selCursor].JID
		m.sel[jid] = !m.sel[jid]
		return m, nil

	case "a":
		allSelected := len(m.chats) > 0
		for _, c := range m.chats {
			if !m.sel[c.JID] {
				allSelected = false
				break
			}
		}
		for _, c := range m.chats {
			m.sel[c.JID] = !allSelected
		}
		return m, nil

	case "enter":
		if m.chatsLoading {
			m.errMsg = "Still loading chats…"
			return m, nil
		}
		var picked []config.MonitoredChat
		for _, c := range m.chats {
			if m.sel[c.JID] {
				picked = append(picked, config.MonitoredChat{JID: c.JID, Name: c.Name})
			}
		}
		if len(picked) == 0 {
			m.errMsg = "Select at least one chat (space)"
			return m, nil
		}
		if err := m.cfg.SetMonitoredChats(picked); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.monitored = make(map[string]string, len(picked))
		for _, p := range picked {
			m.monitored[p.JID] = p.Name
		}
		m.mode = ModeMain
		m.errMsg = ""
		m.status = fmt.Sprintf("Monitoring %d chat(s) — messages are saved, no replies are sent", len(picked))
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
	if m.mode != ModeWhatsAppPhoneInput {
		return m, nil
	}
	if m.input == "" {
		m.errMsg = "Enter a phone number in international format"
		return m, nil
	}
	phone := m.input
	m.mode = ModeWhatsAppPhoneCode
	return m, func() tea.Msg {
		res := <-m.wa.StartPhoneLogin(m.ctx, phone)
		if res.Err != nil {
			return waPhoneLoginErrMsg{err: res.Err}
		}
		return waPhoneCodeMsg(res.Code)
	}
}

type waPhoneCodeMsg string
type waPhoneLoginErrMsg struct{ err error }

func (m Model) handleWALoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = ModeMain
		return m, nil
	}
	return m, nil
}
