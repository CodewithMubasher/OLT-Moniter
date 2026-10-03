package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)

	msg := Message{
		Timestamp: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
		Chat:      "123@g.us",
		ChatName:  "ISP Customers",
		Sender:    "+923001234567",
		Text:      "bill please",
		MsgID:     "abc",
		IsGroup:   true,
	}
	if err := l.Append(msg); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := l.Append(msg); err != nil {
		t.Fatalf("Append #2: %v", err)
	}
	if l.Count() != 2 {
		t.Fatalf("Count = %d, want 2", l.Count())
	}

	// A fresh logger must see everything that was written.
	reloaded := NewLogger(dir)
	if reloaded.Count() != 2 {
		t.Fatalf("reloaded Count = %d, want 2", reloaded.Count())
	}
	got := reloaded.msgs[0]
	if got.Chat != msg.Chat || got.Sender != msg.Sender || got.Text != msg.Text {
		t.Errorf("reloaded message = %+v, want %+v", got, msg)
	}
	if !got.Timestamp.Equal(msg.Timestamp) {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, msg.Timestamp)
	}
}

func TestCorruptLogFileIsSetAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "monitored_messages.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLogger(dir)
	if l.Count() != 0 {
		t.Fatalf("Count = %d, want 0 after corrupt load", l.Count())
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Errorf("corrupt file not set aside: %v", err)
	}

	// And appending still works, producing a valid file.
	if err := l.Append(Message{Text: "hi"}); err != nil {
		t.Fatalf("Append after corrupt: %v", err)
	}
	if reloaded := NewLogger(dir); reloaded.Count() != 1 {
		t.Errorf("reloaded Count = %d, want 1", reloaded.Count())
	}
}
