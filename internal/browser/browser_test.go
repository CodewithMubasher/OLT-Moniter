package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLaunchPortal starts a real Chromium with a persistent profile,
// opens the portal URL, and checks the page actually loaded. Verifies
// the whole launch path end-to-end (install → run → profile → goto).
func TestLaunchPortal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real browser")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	url := "http://103.67.54.54/"
	b, err := Launch(ctx, url, t.TempDir())
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer b.Close()

	if b.Page() == nil {
		t.Fatal("Page() is nil after launch")
	}
	if got := b.Page().URL(); !strings.Contains(got, "103.67.54.54") {
		t.Errorf("page URL = %q, want it to contain the portal host", got)
	}

	// Close must be idempotent (called by defer and by ctx cancel).
	b.Close()
	b.Close()
}

// TestLaunchEmptyURLOpensNothing verifies the no-portal-configured path.
func TestLaunchEmptyURLOpensNothing(t *testing.T) {
	b, err := Launch(context.Background(), "", t.TempDir())
	if err != nil {
		t.Fatalf("Launch with empty URL: %v", err)
	}
	if b != nil {
		t.Fatal("want nil browser when no URL is configured")
	}
}
