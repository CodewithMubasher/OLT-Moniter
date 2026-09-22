package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSpaceKeyDoesNotInsertNULIntoTarget(t *testing.T) {
	m := Model{mode: ModeAddIP, input: "http://127.0.0.1:8000"}
	next, _ := m.handleTextInputKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{0}})
	updated := next.(Model)

	if strings.ContainsRune(updated.input, 0) {
		t.Fatalf("space key inserted a NUL character into %q", updated.input)
	}
	if updated.input != "http://127.0.0.1:8000 " {
		t.Fatalf("target = %q, want a single ordinary trailing space", updated.input)
	}
}
