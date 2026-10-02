package tui

import (
	"os"
	"path/filepath"
	"testing"

	"bills-os/internal/config"
)

// TestDebugRender prints the full dashboard at a given terminal size so
// layout issues can be inspected without a live terminal.
func TestDebugRender(t *testing.T) {
	m := Model{
		cfg:          config.NewManager(t.TempDir()),
		rows:         fakeRows(8),
		width:        120,
		height:       40,
		waOwnNumber:  "+923483338308",
		waConnected:  true,
		msgsReceived: 12,
		msgsReplied:  8,
		msgsIgnored:  4,
		status:       "Replied to +923001234567",
	}
	out := m.View()
	path := filepath.Join(t.TempDir(), "render.txt")
	_ = os.WriteFile(path, []byte(out), 0o644)
	t.Log("\n" + out)
}
