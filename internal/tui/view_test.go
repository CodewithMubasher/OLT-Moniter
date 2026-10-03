package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"bills-os/internal/config"
)

// sampleLogRows builds n display rows so table layout can be tested
// without a live WhatsApp connection.
func sampleLogRows(n int) []LogRow {
	rows := make([]LogRow, 0, n)
	for i := 0; i < n; i++ {
		status := StatusDone
		switch i % 3 {
		case 1:
			status = StatusFail
		case 2:
			status = StatusWait
		}
		rows = append(rows, LogRow{
			ID:        i + 1,
			Time:      fmt.Sprintf("%02d:%02d", i, i),
			Sender:    "+923001234567",
			Text:      fmt.Sprintf("message body number %d", i),
			Panel:     config.DefaultPanel,
			Status:    status,
			Usernames: []string{fmt.Sprintf("hp_customer_%d", i+1)},
		})
	}
	return rows
}

// TestCardsAreSameSize checks that card 1 and card 2 render as two boxes of
// identical width AND height: every rendered line of the joined pair must
// have the same visible width (a taller box would leave half-empty lines).
func TestCardsAreSameSize(t *testing.T) {
	m := Model{
		cfg:          config.NewManager(t.TempDir()),
		waOwnNumber:  "+923483338308",
		msgsReceived: 3,
		msgsSaved:    2,
		monitored:    map[string]string{"123@g.us": "ISP Customers"},
		status:       "Saved from ISP Customers",
	}

	cards := m.viewCards(78, 12)
	lines := strings.Split(cards, "\n")
	if len(lines) < 5 {
		t.Fatalf("expected a two-row box layout, got %d lines", len(lines))
	}

	want := lipgloss.Width(lines[0])
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != want {
			t.Errorf("line %d: width %d, want %d (boxes are not the same size)\n%s", i, got, want, cards)
		}
	}
}

// TestCardsFillRequestedSize checks the scaling math: a card rendered for a
// target of `total` cells must come out exactly that wide, borders included.
func TestCardsFillRequestedSize(t *testing.T) {
	m := Model{cfg: config.NewManager(t.TempDir())}
	const total = 40
	cards := m.viewCards(total*2+cardGap, 12)
	lines := strings.Split(cards, "\n")
	got := lipgloss.Width(lines[0])
	// two cards of `total` plus the gap between them
	if want := total*2 + cardGap; got != want {
		t.Errorf("cards row width = %d, want %d\n%s", got, want, cards)
	}
}

// TestTableFillsWidth checks that every rendered line of the message-log
// card spans the full card width — no empty strip on the right.
func TestTableFillsWidth(t *testing.T) {
	m := Model{
		cfg:       config.NewManager(t.TempDir()),
		logRows:   sampleLogRows(8),
		monitored: map[string]string{"123@g.us": "ISP Customers"},
	}
	const cardW = 80
	table := m.viewLogPanel(cardW, 24)
	for i, ln := range strings.Split(table, "\n") {
		if got := lipgloss.Width(ln); got != cardW {
			t.Errorf("line %d: width %d, want %d (columns do not fill the card)\n%s", i, got, cardW, table)
		}
	}
}

// TestEmptyLogCentersPlaceholder: an empty table shows "no messages yet"
// centered in the card, and the old filter hint is gone.
func TestEmptyLogCentersPlaceholder(t *testing.T) {
	m := Model{cfg: config.NewManager(t.TempDir())}
	table := m.viewLogPanel(60, 14)
	if !strings.Contains(table, "no messages yet") {
		t.Errorf("empty table missing placeholder\n%s", table)
	}
	if strings.Contains(table, "Only messages containing") {
		t.Errorf("old empty-state hint still rendered\n%s", table)
	}
	// Still a fully filled card while empty.
	for i, ln := range strings.Split(table, "\n") {
		if got := lipgloss.Width(ln); got != 60 {
			t.Errorf("line %d: width %d, want 60\n%s", i, got, table)
		}
	}
}

// TestStatusErrorsAreSingleLine: a multi-line library error (Playwright
// timeout with its Call log) must render as one line in the status area
// so the card layout never breaks.
func TestStatusErrorsAreSingleLine(t *testing.T) {
	m := Model{
		cfg:    config.NewManager(t.TempDir()),
		width:  100,
		height: 40,
		errMsg: "Portal: ✗ sk_x: playwright: timeout: Timeout 15000ms exceeded.\nCall log:\n  - waiting for locator('tbody tr')",
	}
	out := m.View()
	if strings.Contains(out, "Call log") {
		t.Errorf("render leaked the playwright call log\n%s", out)
	}
	if strings.Contains(out, "waiting for locator") {
		t.Errorf("render leaked the locator trace\n%s", out)
	}
	if got := oneLine(m.errMsg); strings.ContainsAny(got, "\r\n") {
		t.Errorf("oneLine leaked a newline: %q", got)
	}
}
