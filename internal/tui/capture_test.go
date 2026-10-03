package tui

import (
	"errors"
	"testing"
	"time"

	"bills-os/internal/config"
	"bills-os/internal/portal"
	"bills-os/internal/whatsapp"

	"go.mau.fi/whatsmeow/types"
)

// feedMsg runs one MessageReceivedEvent through Update on a minimal
// dashboard model and returns the updated model.
func feedMsg(t *testing.T, m Model, chat types.JID, text string) Model {
	t.Helper()
	next, _ := m.Update(whatsapp.MessageReceivedEvent{
		Sender:    "+923001234567",
		Chat:      chat,
		ChatName:  "ISP Customers",
		Text:      text,
		MsgID:     "msg-1",
		Timestamp: time.Now(),
	})
	return next.(Model)
}

// TestOnlyUsernameMessagesCaptured checks the dashboard capture filter:
// only monitored messages containing a customer username reach the log
// table (and, by the same code path, the JSON file) — plain chatter and
// "[non-text message]" markers are ignored, and messages from chats that
// aren't selected are ignored too.
func TestOnlyUsernameMessagesCaptured(t *testing.T) {
	monitored := types.NewJID("123", types.DefaultUserServer)
	unmonitored := types.NewJID("999", types.DefaultUserServer)

	base := Model{
		cfg:       config.NewManager(t.TempDir()),
		monitored: map[string]string{monitored.String(): "ISP Customers"},
	}

	m := feedMsg(t, base, monitored, "please activate hp_wasif")
	if m.msgsSaved != 1 || len(m.logRows) != 1 {
		t.Fatalf("username message: saved=%d rows=%d, want 1/1", m.msgsSaved, len(m.logRows))
	}
	// No portal runner in this model → the flow can never succeed, so the
	// row starts as FAIL instead of waiting forever.
	if m.logRows[0].Status != StatusFail {
		t.Errorf("status without browser = %q, want %q", m.logRows[0].Status, StatusFail)
	}

	m = feedMsg(t, m, monitored, "hello everyone how are you")
	m = feedMsg(t, m, monitored, "[non-text message]")
	m = feedMsg(t, m, monitored, "")
	if m.msgsSaved != 1 || len(m.logRows) != 1 {
		t.Errorf("chatter/non-text leaked in: saved=%d rows=%d, want still 1/1", m.msgsSaved, len(m.logRows))
	}

	// Username in an unselected chat: counted as received, never captured.
	m = feedMsg(t, m, unmonitored, "activate mm_wasif please")
	if m.msgsSaved != 1 || len(m.logRows) != 1 {
		t.Errorf("unmonitored chat captured: saved=%d rows=%d, want 1/1", m.msgsSaved, len(m.logRows))
	}
	if m.msgsReceived != 5 {
		t.Errorf("msgsReceived = %d, want 5 (all events counted)", m.msgsReceived)
	}
}

// TestRowFields checks the captured row carries the new display fields:
// running id, HH:MM time, contact name instead of the phone, panel
// label, and WAIT as the initial status.
func TestRowFields(t *testing.T) {
	monitored := types.NewJID("123", types.DefaultUserServer)
	m := Model{
		cfg:       config.NewManager(t.TempDir()),
		monitored: map[string]string{monitored.String(): "ISP Customers"},
		portal:    portal.NewRunner(nil, "http://127.0.0.1/", t.TempDir()),
	}
	next, _ := m.Update(whatsapp.MessageReceivedEvent{
		Sender:     "+923445561767",
		SenderName: "Humaira",
		Chat:       monitored,
		ChatName:   "ISP Customers",
		Text:       "activate hp_wasif",
		MsgID:      "msg-9",
		Timestamp:  time.Now(),
	})
	m = next.(Model)

	if len(m.logRows) != 1 {
		t.Fatalf("rows = %d, want 1", len(m.logRows))
	}
	r := m.logRows[0]
	if r.ID != 1 {
		t.Errorf("ID = %d, want 1", r.ID)
	}
	if len(r.Time) != 5 || r.Time[2] != ':' {
		t.Errorf("Time = %q, want HH:MM", r.Time)
	}
	if r.Sender != "Humaira" {
		t.Errorf("Sender = %q, want contact name Humaira", r.Sender)
	}
	if r.Panel != config.DefaultPanel {
		t.Errorf("Panel = %q, want %q", r.Panel, config.DefaultPanel)
	}
	if r.Status != StatusWait {
		t.Errorf("Status = %q, want %q", r.Status, StatusWait)
	}
	if len(r.Usernames) != 1 || r.Usernames[0] != "hp_wasif" {
		t.Errorf("Usernames = %v, want [hp_wasif]", r.Usernames)
	}

	// Fallback: no saved contact name → show the phone.
	next, _ = m.Update(whatsapp.MessageReceivedEvent{
		Sender:    "+923009999999",
		Chat:      monitored,
		Text:      "activate mm_two",
		Timestamp: time.Now(),
	})
	m = next.(Model)
	if got := m.logRows[1].Sender; got != "+923009999999" {
		t.Errorf("Sender fallback = %q, want +923009999999", got)
	}
	if m.logRows[1].ID != 2 {
		t.Errorf("ID = %d, want 2", m.logRows[1].ID)
	}
}

// TestRowStatusUpdatesFromPortalResult: rows start WAIT and flip to
// DONE/FAIL as portal results arrive — status is never hardcoded.
func TestRowStatusUpdatesFromPortalResult(t *testing.T) {
	monitored := types.NewJID("123", types.DefaultUserServer)
	base := Model{
		cfg:       config.NewManager(t.TempDir()),
		monitored: map[string]string{monitored.String(): "ISP Customers"},
		portal:    portal.NewRunner(nil, "http://127.0.0.1/", t.TempDir()),
	}
	m := feedMsg(t, base, monitored, "activate hp_wasif")
	if m.logRows[0].Status != StatusWait {
		t.Fatalf("initial status = %q, want %q", m.logRows[0].Status, StatusWait)
	}

	next, _ := m.Update(portal.ResultEvent{Username: "hp_wasif", At: time.Now()})
	m = next.(Model)
	if m.logRows[0].Status != StatusDone {
		t.Errorf("after ok result: status = %q, want %q", m.logRows[0].Status, StatusDone)
	}

	next, _ = m.Update(portal.ResultEvent{
		Username: "hp_wasif", Err: errors.New("search timed out"), At: time.Now(),
	})
	m = next.(Model)
	if m.logRows[0].Status != StatusFail {
		t.Errorf("after error result: status = %q, want %q", m.logRows[0].Status, StatusFail)
	}

	// Profile opened but required fields missing → PARTIAL, not FAIL.
	next, _ = m.Update(portal.ResultEvent{
		Username: "hp_wasif", Missing: []string{"phone"}, At: time.Now(),
	})
	m = next.(Model)
	if m.logRows[0].Status != StatusPartial {
		t.Errorf("after partial result: status = %q, want %q", m.logRows[0].Status, StatusPartial)
	}
}
