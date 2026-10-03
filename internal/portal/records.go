package portal

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ActivationRecord is one attempt to open a customer in the portal, as
// stored in data/activation_log.json.
type ActivationRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	Username   string    `json:"username"`
	Chat       string    `json:"chat"`
	ChatName   string    `json:"chat_name,omitempty"`
	ProfileURL string    `json:"profile_url,omitempty"`
	Error      string    `json:"error,omitempty"`
	// Profile carries the scraped subscriber details (status, phone,
	// package, dates, …) for attempts where the scrape ran.
	Profile *Profile `json:"profile,omitempty"`
	// MissingFields lists required fields that came back empty (PARTIAL).
	MissingFields []string `json:"missing_fields,omitempty"`
}

// loadRecords reads the existing log; a corrupt file is renamed aside
// for inspection and a fresh log starts (history is never silently
// discarded, but a bad file never blocks startup either).
func loadRecords(path string) []ActivationRecord {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	var recs []ActivationRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		_ = os.Rename(path, path+".corrupt")
		return nil
	}
	return recs
}

// appendRecord adds one attempt to the log and rewrites the whole file
// atomically (temp + rename), keeping the JSON array valid after every
// append. Safe for concurrent use; records are also kept in memory so
// restarts don't lose history.
func (r *Runner) appendRecord(res ResultEvent) {
	rec := ActivationRecord{
		Timestamp:     res.At,
		Username:      res.Username,
		Chat:          res.Chat,
		ChatName:      res.ChatName,
		ProfileURL:    res.ProfileURL,
		Profile:       res.Profile,
		MissingFields: res.Missing,
	}
	if res.Err != nil {
		rec.Error = cleanErr(res.Err)
	}

	r.logMu.Lock()
	defer r.logMu.Unlock()

	r.records = append(r.records, rec)
	data, err := json.MarshalIndent(r.records, "", "  ")
	if err != nil {
		return
	}
	tmp := r.logPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, r.logPath); err != nil {
		_ = os.Remove(tmp)
	}
}

// LogPath is where activation attempts are recorded (shown in docs/TUI).
func (r *Runner) LogPath() string {
	if r == nil {
		return ""
	}
	return r.logPath
}

// Count returns how many attempts the log holds (in memory, including
// ones loaded from disk at startup).
func (r *Runner) Count() int {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return len(r.records)
}

// String formats a result for one-line status display. The error part is
// collapsed to its first line so Playwright's call log never wraps the
// TUI status into several rows.
func (res ResultEvent) String() string {
	if res.Err != nil {
		return fmt.Sprintf("✗ %s: %s", res.Username, cleanErr(res.Err))
	}
	if len(res.Missing) > 0 {
		return fmt.Sprintf("◐ %s (missing: %s)", res.Username, strings.Join(res.Missing, ", "))
	}
	return fmt.Sprintf("✓ opened %s", res.Username)
}
