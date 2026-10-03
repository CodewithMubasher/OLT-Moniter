package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSpaceKeyDoesNotInsertNULIntoMessage(t *testing.T) {
	m := Model{mode: ModeWhatsAppPhoneInput, input: "Hello"}
	next, _ := m.handleTextInputKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{0}})
	updated := next.(Model)

	if strings.ContainsRune(updated.input, 0) {
		t.Fatalf("space key inserted a NUL character into %q", updated.input)
	}
	if updated.input != "Hello " {
		t.Fatalf("input = %q, want a single ordinary trailing space", updated.input)
	}
}
