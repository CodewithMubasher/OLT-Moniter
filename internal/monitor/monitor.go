// Package monitor runs the continuous ping loop against configured OLTs and
// reports state transitions (UP<->DOWN) so the alert layer can notify exactly
// once per transition, never repeatedly while an OLT stays down.
//
// pingHost in this file shells out to Windows' ping.exe and is Windows-only
// (see the syscall.SysProcAttr{HideWindow: true} use below). This project
// targets Windows cmd/PowerShell, so no build tag is needed, but if this
// ever needs to run on Linux/macOS, pingHost must be swapped for a
// platform-appropriate implementation.
package monitor

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"olt-monitor/internal/oltconfig"
)

// Result is one completed check, passed to the UI/event layer for live
// updates (independent of whether it caused a state transition).
type Result struct {
	Name      string
	IP        string
	Success   bool
	RTT       time.Duration
	Err       error
	Timestamp time.Time
}

// Transition is emitted only when an OLT's status actually changes between
// Up and Down (StatusUnknown does not itself trigger alerts).
type Transition struct {
	OLT  *oltconfig.OLT
	From oltconfig.Status
	To   oltconfig.Status
}

// Engine owns the ticking loop. It reads its interval and OLT list fresh
// from the config manager on every tick, so changes made via the TUI or
// WhatsApp (interval, add/remove/enable/disable) apply on the very next
// check with no restart required.
type Engine struct {
	cfg *oltconfig.Manager

	onResult     func(Result)
	onTransition func(Transition) error

	// lastAlertErr stores the most recent alert delivery failure so the TUI
	// can surface it. Cleared on each tick.
	lastAlertErr error

	// wake lets external code (interval change, "ping now") force an
	// immediate tick instead of waiting for the current interval to elapse.
	wake chan struct{}

	pingTimeout time.Duration
}

func NewEngine(cfg *oltconfig.Manager) *Engine {
	return &Engine{
		cfg:         cfg,
		wake:        make(chan struct{}, 1),
		pingTimeout: 4 * time.Second,
	}
}

// OnResult registers a callback fired after every individual ping (for live
// TUI updates). Optional.
func (e *Engine) OnResult(fn func(Result)) { e.onResult = fn }

// OnTransition registers a callback fired only on UP<->DOWN transitions,
// which is what the alert manager subscribes to. The callback returns an
// error so the engine can surface delivery failures to the TUI.
func (e *Engine) OnTransition(fn func(Transition) error) { e.onTransition = fn }

// AlertError returns the most recent alert delivery failure, or nil.
// The TUI should call this on each tick to surface transient WhatsApp errors.
func (e *Engine) AlertError() error {
	err := e.lastAlertErr
	e.lastAlertErr = nil
	return err
}

// WakeNow forces the next check cycle to start immediately, used when the
// interval changes so the person doesn't wait out the old interval first.
func (e *Engine) WakeNow() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Run blocks, ticking at the configured interval until ctx is cancelled.
// Call it in its own goroutine.
func (e *Engine) Run(ctx context.Context) {
	for {
		e.tick(ctx)

		interval := e.cfg.Interval()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-e.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// tick checks every enabled OLT concurrently and applies results.
func (e *Engine) tick(ctx context.Context) {
	olts := e.cfg.Snapshot()
	threshold := e.cfg.FailureThreshold()

	var wg sync.WaitGroup
	for _, o := range olts {
		if !o.Enabled {
			continue
		}
		o := o
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.checkOne(ctx, o, threshold)
		}()
	}
	wg.Wait()
}

// CheckNow runs a single immediate check of one named OLT (used by the "P"
// TUI key and the WhatsApp "ping <name>" command) and returns the result
// without waiting for the regular cycle.
func (e *Engine) CheckNow(ctx context.Context, name string) (Result, error) {
	olts := e.cfg.Snapshot()
	for _, o := range olts {
		if strings.EqualFold(o.Name, name) {
			threshold := e.cfg.FailureThreshold()
			return e.checkOne(ctx, o, threshold), nil
		}
	}
	return Result{}, errNotFound(name)
}

func (e *Engine) checkOne(ctx context.Context, o *oltconfig.OLT, threshold int) Result {
	success, rtt, err := probeTarget(ctx, o.IP, e.pingTimeout)

	res := Result{Name: o.Name, IP: o.IP, Success: success, RTT: rtt, Err: err, Timestamp: time.Now()}
	if e.onResult != nil {
		e.onResult(res)
	}

	prev, next, updated, saveErr := e.cfg.UpdateCheckResult(o.Name, success, rtt, err, threshold)
	_ = saveErr // surfaced via TUI status line if needed; not fatal to monitoring

	// Alert on any real UP<->DOWN transition, including the first-ever
	// result already crossing the failure threshold (e.g. an OLT added
	// while already unreachable) — StatusUnknown -> StatusDown counts too.
	// StatusUnknown -> StatusUp does not, since "first successful check"
	// is not a recovery from anything.
	transitioned := prev != next &&
		next != oltconfig.StatusUnknown &&
		!(prev == oltconfig.StatusUnknown && next == oltconfig.StatusUp)
	if transitioned && e.onTransition != nil && updated != nil {
		if err := e.onTransition(Transition{OLT: updated, From: prev, To: next}); err != nil {
			e.lastAlertErr = err
		}
	}
	return res
}

// probeTarget chooses a reachability check from the configured target:
//
//   - http:// and https:// URLs receive an HTTP GET request.
//   - host:port values receive a TCP connection check.
//   - plain IP addresses and hostnames use ICMP via Windows ping.exe.
//
// This lets the same monitor handle network equipment as well as local web
// services. An HTTP response of any status is considered reachable: a 404 or
// 500 still proves the web server answered. Use a path such as /health when
// a particular application's health endpoint should be checked.
func probeTarget(ctx context.Context, target string, timeout time.Duration) (bool, time.Duration, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, 0, fmt.Errorf("target is empty")
	}

	if strings.Contains(target, "://") {
		parsed, err := url.Parse(target)
		if err != nil {
			return false, 0, fmt.Errorf("invalid URL: %w", err)
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			if parsed.Host == "" {
				return false, 0, fmt.Errorf("HTTP URL is missing a host")
			}
			return probeHTTP(ctx, parsed.String(), timeout)
		default:
			return false, 0, fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
		}
	}

	if _, _, err := net.SplitHostPort(target); err == nil {
		return probeTCP(ctx, target, timeout)
	}
	return pingHost(ctx, target, timeout)
}

func probeHTTP(ctx context.Context, target string, timeout time.Duration) (bool, time.Duration, error) {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use the same Windows HTTP stack exposed by Invoke-WebRequest. The target
	// is supplied as an environment variable instead of being interpolated
	// into PowerShell code, so URL characters cannot change the command.
	timeoutSeconds := int(timeout.Round(time.Second) / time.Second)
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}
	script := fmt.Sprintf(`try {
    Invoke-WebRequest -Uri $env:OLT_MONITOR_TARGET_URL -UseBasicParsing -TimeoutSec %d -ErrorAction Stop | Out-Null
    exit 0
} catch {
    if ($_.Exception.Response) { exit 0 }
    Write-Error $_
    exit 1
}`, timeoutSeconds)
	cmd := exec.CommandContext(checkCtx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(cmd.Environ(), "OLT_MONITOR_TARGET_URL="+target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	started := time.Now()
	output, err := cmd.CombinedOutput()
	rtt := time.Since(started)
	if err != nil {
		if checkCtx.Err() != nil {
			return false, 0, checkCtx.Err()
		}
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return false, 0, fmt.Errorf("HTTP request: %s", detail)
	}
	return true, rtt, nil
}

func probeTCP(ctx context.Context, address string, timeout time.Duration) (bool, time.Duration, error) {
	dialer := net.Dialer{Timeout: timeout}
	started := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", address)
	rtt := time.Since(started)
	if err != nil {
		return false, 0, fmt.Errorf("TCP connection: %w", err)
	}
	conn.Close()
	return true, rtt, nil
}

// pingHost checks a host by shelling out to Windows' built-in ping.exe
// instead of sending raw ICMP ourselves. Windows only allows unprivileged
// processes to send ICMP through a real raw socket if they hold the
// SeAdministratorPrivilege, so any Go ICMP library (including pro-bing's
// "unprivileged" mode, which is Linux-only despite the name) silently fails
// to receive replies when run as a normal user — every host then reports as
// down even though it responds fine to a normal ping. Windows' own ping.exe
// does not have this restriction, so we drive it directly and parse its
// output. This also means no admin rights are required to run the monitor.
func pingHost(ctx context.Context, host string, timeout time.Duration) (bool, time.Duration, error) {
	timeoutMs := timeout.Milliseconds()
	if timeoutMs <= 0 {
		timeoutMs = 4000
	}

	cmd := exec.CommandContext(ctx, "ping",
		"-n", "1", // one echo request
		"-w", fmt.Sprintf("%d", timeoutMs), // per-reply timeout, ms
		host,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.CombinedOutput()
	text := string(out)

	if ctx.Err() != nil {
		return false, 0, ctx.Err()
	}
	if err != nil {
		// ping.exe returns non-zero on timeout/unreachable, not on a real
		// failure to run it, so still try to read something useful from
		// its output before giving up.
		if !looksLikePingOutput(text) {
			return false, 0, fmt.Errorf("run ping: %w", err)
		}
	}

	if rtt, ok := parseWindowsPingRTT(text); ok {
		return true, rtt, nil
	}
	if reason := windowsPingFailureReason(text); reason != "" {
		return false, 0, timeoutError(reason)
	}
	return false, 0, errTimeout
}

func looksLikePingOutput(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "ping statistics") ||
		strings.Contains(lower, "reply from") ||
		strings.Contains(lower, "request timed out") ||
		strings.Contains(lower, "destination host unreachable") ||
		strings.Contains(lower, "could not find host")
}

// parseWindowsPingRTT looks for a successful "Reply from ... time=Xms" (or
// "time<1ms") line and returns the round-trip time.
func parseWindowsPingRTT(text string) (time.Duration, bool) {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "reply from")
	if idx == -1 {
		return 0, false
	}
	line := lower[idx:]

	timeIdx := strings.Index(line, "time")
	if timeIdx == -1 {
		// No explicit time shown (can happen on some locales/hops); a reply
		// still means the host is up.
		return 0, true
	}
	rest := line[timeIdx+len("time"):]
	rest = strings.TrimPrefix(rest, "<")
	rest = strings.TrimPrefix(rest, "=")

	end := strings.IndexAny(rest, "m ")
	if end == -1 {
		end = len(rest)
	}
	numStr := strings.TrimSpace(rest[:end])
	if numStr == "" {
		return 0, true
	}

	var ms float64
	if _, err := fmt.Sscanf(numStr, "%f", &ms); err != nil {
		return 0, true
	}
	return time.Duration(ms * float64(time.Millisecond)), true
}

// windowsPingFailureReason turns ping.exe's plain-English failure lines into
// a short reason string for the TUI status line.
func windowsPingFailureReason(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "request timed out"):
		return "request timed out"
	case strings.Contains(lower, "destination host unreachable"):
		return "destination host unreachable"
	case strings.Contains(lower, "could not find host"), strings.Contains(lower, "unknown host"):
		return "could not resolve host"
	case strings.Contains(lower, "general failure"):
		return "network error (general failure)"
	case strings.Contains(lower, "transmit failed"):
		return "transmit failed"
	}
	return ""
}

type notFoundError string

func (e notFoundError) Error() string { return "no OLT named \"" + string(e) + "\"" }
func errNotFound(name string) error   { return notFoundError(name) }

type timeoutError string

func (e timeoutError) Error() string { return string(e) }

const errTimeout = timeoutError("request timed out")
