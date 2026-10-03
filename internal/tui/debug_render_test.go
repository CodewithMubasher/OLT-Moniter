package tui

import (
	"os"
	"path/filepath"
	"testing"

	"bills-os/internal/config"
	"bills-os/internal/whatsapp"
)

// TestDebugRender prints the full dashboard at a given terminal size so
// layout issues can be inspected without a live terminal.
func TestDebugRender(t *testing.T) {
	m := Model{
		cfg:          config.NewManager(t.TempDir()),
		logRows:      sampleLogRows(8),
		width:        130,
		height:       40,
		waOwnNumber:  "+923483338308",
		waConnected:  true,
		monitored:    map[string]string{"123@g.us": "ISP Customers", "92300@s.whatsapp.net": "+92300"},
		msgsReceived: 12,
		msgsSaved:    8,
		status:       "Saved from ISP Customers",
	}
	out := m.View()
	path := filepath.Join(t.TempDir(), "render.txt")
	_ = os.WriteFile(path, []byte(out), 0o644)
	t.Log("\n" + out)
}

// TestDebugRenderChatSelect renders the chat picker so its layout can be
// inspected without a live WhatsApp connection.
func TestDebugRenderChatSelect(t *testing.T) {
	m := Model{
		cfg:       config.NewManager(t.TempDir()),
		mode:      ModeChatSelect,
		width:     100,
		height:    36,
		sel:       map[string]bool{"1@g.us": true},
		selCursor: 1,
		chats: []whatsapp.ChatInfo{
			{JID: "1@g.us", Name: "ISP Customers Karachi", IsGroup: true},
			{JID: "2@g.us", Name: "NOC Team", IsGroup: true},
			{JID: "92300@s.whatsapp.net", Name: "+923001234567", IsGroup: false},
		},
	}
	_ = m.View()
}
