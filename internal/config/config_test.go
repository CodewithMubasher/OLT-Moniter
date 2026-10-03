package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortalURLAndMonitoredChatsPersist(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	if _, err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := m.PortalURL(); got != DefaultPortalURL {
		t.Errorf("PortalURL default = %q, want %q", got, DefaultPortalURL)
	}
	if got := m.MonitoredChats(); len(got) != 0 {
		t.Errorf("MonitoredChats default = %v, want empty", got)
	}

	// Persist through a manager reload.
	m.mu.Lock()
	m.cfg.Portal.URL = "https://portal.example.com/login"
	m.mu.Unlock()
	if err := m.SetMonitoredChats([]MonitoredChat{
		{JID: "123@g.us", Name: "ISP Customers"},
		{JID: "923001234567@s.whatsapp.net", Name: "+923001234567"},
	}); err != nil {
		t.Fatalf("SetMonitoredChats: %v", err)
	}

	m2 := NewManager(dir)
	if _, err := m2.Load(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := m2.PortalURL(); got != "https://portal.example.com/login" {
		t.Errorf("PortalURL = %q after reload", got)
	}
	chats := m2.MonitoredChats()
	if len(chats) != 2 || chats[0].JID != "123@g.us" || chats[1].Name != "+923001234567" {
		t.Errorf("MonitoredChats = %+v after reload", chats)
	}
}

func TestMonitoredChatsReturnsCopy(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.SetMonitoredChats([]MonitoredChat{{JID: "1@g.us", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	got := m.MonitoredChats()
	got[0].Name = "mutated"
	if m.MonitoredChats()[0].Name != "A" {
		t.Error("MonitoredChats leaked internal storage")
	}
}

func TestCorruptConfigIsSetAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	if _, err := m.Load(); err != nil {
		t.Fatalf("Load must tolerate corrupt config: %v", err)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Errorf("corrupt config not set aside: %v", err)
	}
}

func TestDefaultPortalURL(t *testing.T) {
	dir := t.TempDir()

	// No config file at all → default portal URL.
	m := NewManager(dir)
	if _, err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if got := m.PortalURL(); got != DefaultPortalURL {
		t.Errorf("fresh PortalURL = %q, want %q", got, DefaultPortalURL)
	}

	// Existing file without a portal section → default applied on load.
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"monitor":{"chats":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(dir)
	if _, err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := m2.PortalURL(); got != DefaultPortalURL {
		t.Errorf("PortalURL without portal section = %q, want %q", got, DefaultPortalURL)
	}

	// Explicit empty url means "no browser" and must be honored.
	if err := os.WriteFile(path, []byte(`{"portal":{"url":""}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m3 := NewManager(dir)
	if _, err := m3.Load(); err != nil {
		t.Fatal(err)
	}
	if got := m3.PortalURL(); got != "" {
		t.Errorf("explicit empty PortalURL = %q, want empty", got)
	}
}
