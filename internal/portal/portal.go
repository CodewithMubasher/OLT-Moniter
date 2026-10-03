// Package portal drives the ISP subscriber panel inside the persistent
// Chromium window: open /subscribers, search a customer by username,
// open their profile page.
//
// Jobs arrive automatically from monitored WhatsApp messages (any word
// with an underscore is treated as a customer username) and are processed
// strictly one at a time — a single visible page can only do one thing at
// a time. Every attempt is persisted to data/activation_log.json and
// emitted as a ResultEvent for the TUI status line.
package portal

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	pw "github.com/mxschmitt/playwright-go"
)

// Selectors, locked from the DevTools recording of /subscribers
// (2026-10-03). The row body class identifies the DataTables rows of the
// subscriber table; the username cell is the div carrying title="Username"
// inside the profile link.
const (
	searchInput     = `input[placeholder="Type & Search"]`
	rowsSelector    = `tbody.text-gray-600.fw-bold tr`
	usernameCell    = `div[title="Username"]`
	profileListPath = "/subscribers"
	profilePath     = "/subscribers/profile/"

	gotoTimeout     = 30 * time.Second
	searchTimeout   = 15 * time.Second
	clickTimeout    = 15 * time.Second
	navigateWait    = 15 * time.Second
	eventBufferSize = 64
	queueSize       = 32
)

// Job is one customer to open in the portal.
type Job struct {
	Username string
	Chat     string // JID of the chat the request came from
	ChatName string
	MsgID    string
}

// ResultEvent is emitted after each job finishes. The TUI turns it into
// the status line and the portal counter; the same outcome (including
// the scraped profile when it ran) is always persisted to
// activation_log.json.
type ResultEvent struct {
	Username   string
	Chat       string
	ChatName   string
	ProfileURL string
	Profile    *Profile // scraped details, nil when the scrape didn't run
	Missing    []string // required fields that came back empty (PARTIAL)
	Err        error
	At         time.Time
}

// Runner serializes portal flows through one Playwright page.
type Runner struct {
	base string
	page pw.Page
	jobs chan Job
	evts chan any

	mu      sync.Mutex
	pending map[string]bool // usernames queued or running (dedupe)

	logMu        sync.Mutex
	logPath      string
	customersDir string // data/customers — one JSON file per customer
	records      []ActivationRecord
}

// NewRunner prepares a runner for the portal behind portalURL (its
// origin is what all flows navigate from) and loads the existing
// activation log from dataDir so history is preserved across restarts.
func NewRunner(page pw.Page, portalURL, dataDir string) *Runner {
	r := &Runner{
		base:         origin(portalURL),
		page:         page,
		jobs:         make(chan Job, queueSize),
		evts:         make(chan any, eventBufferSize),
		pending:      make(map[string]bool),
		logPath:      filepath.Join(dataDir, "activation_log.json"),
		customersDir: filepath.Join(dataDir, "customers"),
	}
	r.records = loadRecords(r.logPath)
	return r
}

// Events carries ResultEvent values for the TUI. Closed when the
// runner's context is cancelled (app shutdown).
func (r *Runner) Events() <-chan any {
	if r == nil {
		return nil
	}
	return r.evts
}

// Enqueue queues a username for processing. Returns false when the
// runner is nil, the queue is full, or this username is already
// queued/running (so a spammy group can't pile up duplicate work).
func (r *Runner) Enqueue(j Job) bool {
	if r == nil || j.Username == "" {
		return false
	}
	r.mu.Lock()
	if r.pending[j.Username] {
		r.mu.Unlock()
		return false
	}
	r.pending[j.Username] = true
	r.mu.Unlock()

	select {
	case r.jobs <- j:
		return true
	default:
		r.mu.Lock()
		delete(r.pending, j.Username)
		r.mu.Unlock()
		return false
	}
}

// Start launches the processing loop; it runs until ctx is cancelled.
func (r *Runner) Start(ctx context.Context) {
	go r.loop(ctx)
}

func (r *Runner) loop(ctx context.Context) {
	defer close(r.evts)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-r.jobs:
			res := r.runJob(job)
			select {
			case r.evts <- res:
			case <-ctx.Done():
				return
			}
		}
	}
}

// runJob performs one full flow (search → open profile), records the
// result, and always releases the username back from the pending set.
func (r *Runner) runJob(job Job) (res ResultEvent) {
	res = ResultEvent{
		Username: job.Username,
		Chat:     job.Chat,
		ChatName: job.ChatName,
		At:       time.Now(),
	}
	defer func() {
		if rec := recover(); rec != nil {
			res.Err = fmt.Errorf("portal flow panicked: %v", rec)
		}
		r.mu.Lock()
		delete(r.pending, job.Username)
		r.mu.Unlock()
		r.appendRecord(res)
	}()

	if err := r.openProfile(job.Username); err != nil {
		// Collapse to a single short line here — playwright errors embed
		// a multi-line "Call log" that must never leak into the TUI
		// status line or the activation log.
		res.Err = errors.New(cleanErr(err))
		return res
	}
	res.ProfileURL = r.page.URL()

	// Profile opened → copy the page text (Ctrl+A → Ctrl-C equivalent)
	// and parse it into JSON. Missing required fields make the result
	// PARTIAL (res.Missing), never an error.
	prof, missing, err := scrapeProfile(r.page)
	if err != nil {
		res.Err = errors.New(cleanErr(err))
		return res
	}
	res.Profile = prof
	res.Missing = missing
	r.saveCustomer(job.Username, res.ProfileURL, prof)
	return res
}

// cleanErr collapses a (possibly multi-line) error to its first line,
// trimmed. Playwright timeouts look like:
//
//	playwright: timeout: Timeout 15000ms exceeded.
//	Call log:
//	  - waiting for locator(...)
//
// Only the first line is useful to the operator.
func cleanErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(msg)
}

// openProfile runs the recorded flow: /subscribers → fill search →
// click the matching row's username cell → wait for the profile page.
func (r *Runner) openProfile(username string) error {
	if _, err := r.page.Goto(r.base+profileListPath, pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateDomcontentloaded,
		Timeout:   pw.Float(float64(gotoTimeout / time.Millisecond)),
	}); err != nil {
		return fmt.Errorf("open subscribers page: %w", err)
	}

	search := r.page.Locator(searchInput)
	if err := search.WaitFor(pw.LocatorWaitForOptions{
		State:   pw.WaitForSelectorStateVisible,
		Timeout: pw.Float(float64(searchTimeout / time.Millisecond)),
	}); err != nil {
		return fmt.Errorf("search box not found: %w", err)
	}
	if err := search.Fill(username, pw.LocatorFillOptions{
		Timeout: pw.Float(float64(searchTimeout / time.Millisecond)),
	}); err != nil {
		return fmt.Errorf("type customer id: %w", err)
	}

	// The table filters as you type; wait for a row containing the
	// username. (The search can return several rows — first match wins
	// for now; exact-match disambiguation is a follow-up discussion.)
	row := r.page.Locator(rowsSelector).Filter(pw.LocatorFilterOptions{
		HasText: username,
	}).First()
	if err := row.WaitFor(pw.LocatorWaitForOptions{
		State:   pw.WaitForSelectorStateVisible,
		Timeout: pw.Float(float64(searchTimeout / time.Millisecond)),
	}); err != nil {
		return fmt.Errorf("no result row for %q: %w", username, err)
	}

	cell := row.Locator(usernameCell)
	if err := cell.Click(pw.LocatorClickOptions{
		Timeout: pw.Float(float64(clickTimeout / time.Millisecond)),
	}); err != nil {
		return fmt.Errorf("click username cell: %w", err)
	}

	if err := r.page.WaitForURL("**"+profilePath+"**", pw.PageWaitForURLOptions{
		Timeout: pw.Float(float64(navigateWait / time.Millisecond)),
	}); err != nil {
		return fmt.Errorf("profile page did not open: %w", err)
	}
	if !strings.Contains(r.page.URL(), profilePath) {
		return fmt.Errorf("landed on unexpected page: %s", r.page.URL())
	}
	return nil
}

// --- username extraction ---

// usernamePattern matches words that contain an underscore and start
// with a letter: hp_wasif, sk_ali, kts_xyz, ss_abc, mm_wasif,
// hp_shahzad_ahmad_Mkund. (The operator's rule: any word with an
// underscore is a customer username.)
var usernamePattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*_[A-Za-z0-9_]+`)

// ExtractUsernames returns every distinct customer username mentioned in
// a message, in order of appearance, trimmed of trailing underscores.
func ExtractUsernames(text string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range usernamePattern.FindAllString(text, -1) {
		m = strings.Trim(m, "_")
		if m == "" {
			continue
		}
		key := strings.ToLower(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}

// origin reduces a portal URL (http://host/path) to scheme://host so
// flows can build their own paths. Falls back to trimming trailing
// slashes when the URL can't be parsed.
func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	return u.Scheme + "://" + u.Host
}
