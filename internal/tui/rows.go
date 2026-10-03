package tui

// maxLogRows bounds the in-memory message log the table renders. The JSON
// file keeps everything; only the tail is held for display.
const maxLogRows = 500

// Status values shown in the STATUS column. Rows start as WAIT and are
// updated to DONE / PARTIAL / FAIL by portal.ResultEvent as the browser
// flow runs: PARTIAL = profile opened but required fields were missing.
const (
	StatusWait    = "WAIT"
	StatusDone    = "DONE"
	StatusPartial = "PARTIAL"
	StatusFail    = "FAIL"
)

// LogRow is one line of the message-log table: a captured message from a
// monitored chat that mentioned at least one customer username.
type LogRow struct {
	ID        int      // running row number (1, 2, 3, …)
	Time      string   // HH:MM
	Sender    string   // contact name when saved, otherwise "+<phone>"
	Text      string   // message body (truncated for display only)
	Panel     string   // panel label, e.g. "PACE"
	Status    string   // WAIT → DONE / FAIL as the portal flow runs
	Usernames []string // extracted usernames (portal job keys)
}

// appendLogRow adds a row and trims the window to maxLogRows.
func appendLogRow(rows []LogRow, row LogRow) []LogRow {
	rows = append(rows, row)
	if len(rows) > maxLogRows {
		rows = rows[len(rows)-maxLogRows:]
	}
	return rows
}
