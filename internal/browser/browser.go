// Package browser owns the Chrome window used for the ISP billing portal.
//
// It uses Playwright for Go with a PERSISTENT Chromium profile stored in
// <data>/browser-data: cookies, logins and local storage survive app
// restarts, so the operator logs into the portal once and stays logged
// in — the same setup as the isp-auto-pilot project. The page handle is
// kept for later phases that will fill forms and click buttons on the
// portal DOM.
package browser

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	pw "github.com/mxschmitt/playwright-go"
)

const (
	// profileDirName is the persistent Chromium profile directory, inside
	// the app's data dir (gitignored — the profile contains the portal
	// session and must never be committed).
	profileDirName = "browser-data"

	// navigateTimeout bounds the initial page load.
	navigateTimeout = 30 * time.Second
)

// Browser is a live Chromium instance with a persistent profile, pointed
// at the portal. Safe for repeated Close calls.
type Browser struct {
	engine *pw.Playwright
	ctx    pw.BrowserContext
	page   pw.Page

	closeOnce sync.Once
}

// Launch starts a visible Chromium with the persistent profile in
// dataDir, opens url, and waits for the page to load. parent should be
// the process-lifetime context (signal-aware): cancelling it closes the
// browser.
//
// Returns nil browser and nil error when url is empty — no portal
// configured means no browser is opened.
func Launch(parent context.Context, url, dataDir string) (*Browser, error) {
	if url == "" {
		return nil, nil
	}

	// Install fetches the matching Chromium build and driver once; it is a
	// fast no-op on every later run.
	if err := pw.Install(); err != nil {
		return nil, fmt.Errorf("install playwright browsers: %w", err)
	}
	engine, err := pw.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright: %w", err)
	}

	b := &Browser{engine: engine}
	b.ctx, err = engine.Chromium.LaunchPersistentContext(
		filepath.Join(dataDir, profileDirName),
		pw.BrowserTypeLaunchPersistentContextOptions{
			Headless: pw.Bool(false),
			Viewport: &pw.Size{Width: 1280, Height: 800},
		},
	)
	if err != nil {
		_ = engine.Stop()
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	// Reuse the tab Playwright opens (about:blank) rather than spawning a
	// second one.
	if pages := b.ctx.Pages(); len(pages) > 0 {
		b.page = pages[0]
	} else {
		b.page, err = b.ctx.NewPage()
		if err != nil {
			b.Close()
			return nil, fmt.Errorf("open tab: %w", err)
		}
	}

	if _, err := b.page.Goto(url, pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateDomcontentloaded,
		Timeout:   pw.Float(float64(navigateTimeout / time.Millisecond)),
	}); err != nil {
		b.Close()
		return nil, fmt.Errorf("open portal %s: %w", url, err)
	}

	// Tie the browser's lifetime to the process context.
	go func() {
		<-parent.Done()
		b.Close()
	}()
	return b, nil
}

// Page returns the portal tab. Future automation (locators, clicks,
// form fills) runs against it: page.Locator(...), etc.
func (b *Browser) Page() pw.Page {
	if b == nil {
		return nil
	}
	return b.page
}

// Close shuts down the browser context and the Playwright driver. Called
// on app exit and whenever the process context is cancelled; idempotent.
func (b *Browser) Close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		if b.ctx != nil {
			_ = b.ctx.Close()
		}
		if b.engine != nil {
			_ = b.engine.Stop()
		}
	})
}
