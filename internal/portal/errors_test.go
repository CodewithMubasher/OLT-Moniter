package portal

import (
	"errors"
	"strings"
	"testing"
)

// rawTimeout mimics what playwright-go returns on a locator timeout:
// a useful first line followed by a multi-line "Call log" block.
const rawTimeout = "no result row for \"sk_x\": playwright: timeout: Timeout 15000ms exceeded.\n" +
	"Call log:\n" +
	"  - waiting for locator('tbody.text-gray-600.fw-bold tr')"

// TestCleanErrKeepsFirstLine: the Call log block must never survive.
func TestCleanErrKeepsFirstLine(t *testing.T) {
	got := cleanErr(errors.New(rawTimeout))
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("cleanErr leaked a newline: %q", got)
	}
	if strings.Contains(got, "Call log") {
		t.Errorf("cleanErr leaked the call log: %q", got)
	}
	want := `no result row for "sk_x": playwright: timeout: Timeout 15000ms exceeded.`
	if got != want {
		t.Errorf("cleanErr = %q, want %q", got, want)
	}
	if cleanErr(nil) != "" {
		t.Errorf("cleanErr(nil) = %q, want empty", cleanErr(nil))
	}
}

// TestResultEventStringIsSingleLine: the TUI status line gets this string
// — it must always be one line.
func TestResultEventStringIsSingleLine(t *testing.T) {
	got := ResultEvent{Username: "sk_x", Err: errors.New(rawTimeout)}.String()
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("String() leaked a newline: %q", got)
	}
	if strings.Contains(got, "Call log") {
		t.Errorf("String() leaked the call log: %q", got)
	}
	if !strings.HasPrefix(got, "✗ sk_x: ") {
		t.Errorf("String() = %q, want it to start with ✗ sk_x:", got)
	}
}
