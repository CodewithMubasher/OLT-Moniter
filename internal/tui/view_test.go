package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"bills-os/internal/config"
)

// TestCardsAreSameSize checks that card 1 and card 2 render as two boxes of
// identical width AND height: every rendered line of the joined pair must
// have the same visible width (a taller box would leave half-empty lines).
func TestCardsAreSameSize(t *testing.T) {
	m := Model{
		cfg:          config.NewManager(t.TempDir()),
		waOwnNumber:  "+923483338308",
		msgsReceived: 3,
		msgsReplied:  1,
		msgsIgnored:  2,
		status:       "Replied to +923001234567",
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

// TestTableFillsWidth checks problem 1's fix: every rendered line of the
// table card must span the full card width — no empty strip on the right.
func TestTableFillsWidth(t *testing.T) {
	m := Model{
		cfg:       config.NewManager(t.TempDir()),
		rows:      fakeRows(8),
		rowCursor: 0,
	}
	const cardW = 80
	table := m.viewRowsPanel(cardW, 24)
	for i, ln := range strings.Split(table, "\n") {
		if got := lipgloss.Width(ln); got != cardW {
			t.Errorf("line %d: width %d, want %d (columns do not fill the card)\n%s", i, got, cardW, table)
		}
	}
}
