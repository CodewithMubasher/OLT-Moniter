// Package monitor persists captured WhatsApp messages to
// data/monitored_messages.json — a single JSON array, appended in order.
// Only messages from the chats the operator selected are written here;
// nothing is ever sent back (no replies in this phase).
package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Message is one captured chat message as stored on disk.
type Message struct {
	Timestamp  time.Time `json:"timestamp"`
	Chat       string    `json:"chat"`                  // full JID of the chat it came from
	ChatName   string    `json:"chat_name"`             // display name ("" when unknown)
	Sender     string    `json:"sender"`                // "+<phone>" of the sender
	SenderName string    `json:"sender_name,omitempty"` // saved contact name when known
	Text       string    `json:"text"`
	MsgID      string    `json:"msg_id"`
	IsGroup    bool      `json:"is_group"`
}

// Logger owns monitored_messages.json. All methods are safe for concurrent
// use. The file is loaded once at startup (so history survives restarts)
// and rewritten atomically on every append.
type Logger struct {
	mu   sync.Mutex
	path string
	msgs []Message
}

// NewLogger loads (or prepares to create) the log file inside dir.
func NewLogger(dir string) *Logger {
	l := &Logger{path: filepath.Join(dir, "monitored_messages.json")}

	data, err := os.ReadFile(l.path)
	if err == nil && len(data) > 0 {
		// A corrupt log must never block startup: rename it aside and
		// start a fresh one (the old bytes stay on disk for inspection).
		if json.Unmarshal(data, &l.msgs) != nil {
			_ = os.Rename(l.path, l.path+".corrupt")
			l.msgs = nil
		}
	}
	return l
}

// Path is the file messages are written to (shown in the TUI).
func (l *Logger) Path() string {
	return l.path
}

// Count is how many messages the file currently holds (including ones
// loaded from disk at startup).
func (l *Logger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.msgs)
}

// Append adds one message and rewrites the whole file atomically
// (temp file + rename), so a crash mid-write can never corrupt the log.
// The JSON array stays valid after every append.
func (l *Logger) Append(m Message) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.msgs = append(l.msgs, m)
	if err := l.writeLocked(); err != nil {
		// Keep the file consistent with what was actually written: drop
		// the message we failed to persist so Count() never lies.
		l.msgs = l.msgs[:len(l.msgs)-1]
		return err
	}
	return nil
}

func (l *Logger) writeLocked() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	data, err := json.MarshalIndent(l.msgs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write log: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace log: %w", err)
	}
	return nil
}
