package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"olt-monitor/internal/oltconfig"
)

// Minimal, low-noise palette: one accent color, a neutral text scale, and
// the three status colors. Hierarchy comes from weight/spacing rather than
// piling on more colors.
const (
	colAccent = "#8B8FF7" // soft indigo — selection & small accents only
	colGreen  = "#4ADE80"
	colRed    = "#F87171"
	colYellow = "#FBBF24"
	colFg     = "#E4E4E7" // primary text
	colFg2    = "#A1A1AA" // secondary text
	colFg3    = "#6B7280" // tertiary / hint text
	colBorder = "#3F3F46"
)

var (
	appTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colFg))
	appSubtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colBorder))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colBorder)).
			Padding(1, 2)

	headStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colFg3))
	selRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg)).Bold(true)
	rowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg))
	dimRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg3))
	cursorGlyph = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)).Bold(true).Render("▍")

	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(colGreen)).Bold(true)
	badStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colRed)).Bold(true)
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg3))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colYellow)).Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg3))
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg2)).Bold(true)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colRed))
	helpKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg)).Bold(true)
	helpSepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colBorder))
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent))
	metaStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg2))

	qrStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#000000"))
	codeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colYellow)).Bold(true).
			Padding(0, 3).
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(colYellow))
)

// minContentWidth/maxContentWidth bound the content column so it neither
// collapses on a narrow terminal nor stretches into unreadably long rows on
// a large one.
const (
	minContentWidth = 58
	maxContentWidth = 86
)

func (m Model) contentWidth() int {
	w := m.width - 8 // breathing room on both sides
	if w > maxContentWidth {
		w = maxContentWidth
	}
	if w < minContentWidth {
		w = minContentWidth
	}
	return w
}

// frame centers content in the full terminal area reported by
// tea.WindowSizeMsg, instead of letting it sit pinned to the top-left corner
// with the rest of a large terminal left blank.
func (m Model) frame(content string) string {
	if m.width <= 0 || m.height <= 0 {
		return content
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) View() string {
	if m.quitting {
		return m.frame(dimStyle.Render("Goodbye 👋"))
	}

	switch m.mode {
	case ModeWhatsAppPhoneInput:
		return m.frame(m.viewPhoneInput())
	case ModeWhatsAppPhoneCode:
		return m.frame(m.viewPhoneCode())
	case ModeWhatsAppTarget:
		return m.frame(m.viewWhatsAppTarget())
	}

	w := m.contentWidth()
	var main strings.Builder

	main.WriteString(m.viewTable(w))

	switch m.mode {
	case ModeAddName:
		main.WriteString("\n\n" + m.viewForm(w, "Add OLT · name", m.input, ""))
	case ModeAddIP:
		main.WriteString("\n\n" + m.viewForm(w, "Add OLT · target (IP, host, or URL)", m.input, "Name   "+m.formName))
	case ModeEditName:
		main.WriteString("\n\n" + m.viewForm(w, "Edit OLT · name  (Tab to keep, edit IP next)", m.input, ""))
	case ModeEditIP:
		main.WriteString("\n\n" + m.viewForm(w, "Edit OLT · target (IP, host, or URL)", m.input, "Name   "+m.formName))
	case ModeInterval:
		main.WriteString("\n\n" + m.viewForm(w, "Set monitoring interval  (10s, 30s, 5m, or seconds)", m.input, "Current   "+m.interval.String()))
	case ModeConfirmRemove:
		if o := m.selected(); o != nil {
			warn := warnStyle.Render(fmt.Sprintf("Remove %s (%s)?", o.Name, o.IP)) + "   " +
				mutedStyle.Render("y / n")
			main.WriteString("\n\n" + panelStyle.Width(w-4).Render(warn))
		}
	}

	if m.errMsg != "" {
		main.WriteString("\n\n" + errStyle.Render("⚠  "+m.errMsg))
	}

	// Show transient WhatsApp errors (e.g. session expired, connect failed)
	// on the main dashboard so the user sees them without switching screens.
	if m.waLoginErr != "" && m.mode == ModeTable {
		main.WriteString("\n\n" + warnStyle.Render("⚠  "+m.waLoginErr))
	}

	return m.dashboard(m.viewHeader(maxInt(0, m.width-4)), main.String(), m.viewFooter(w))
}

// dashboard keeps the persistent parts of the monitor anchored: the title at
// the top, the OLT table in the available middle space, and its helpers at
// the bottom. Forms remain attached to the table when they are open.
func (m Model) dashboard(header, main, footer string) string {
	if m.width <= 0 || m.height <= 0 {
		return header + "\n\n" + main + "\n\n" + footer
	}

	header = "  " + header
	headerH, footerH := lipgloss.Height(header), lipgloss.Height(footer)
	mainH := m.height - headerH - footerH
	if mainH < lipgloss.Height(main) {
		return m.frame(header + "\n\n" + main + "\n\n" + footer)
	}

	top := lipgloss.Place(m.width, headerH, lipgloss.Left, lipgloss.Top, header)
	middle := lipgloss.Place(m.width, mainH, lipgloss.Center, lipgloss.Center, main)
	bottom := lipgloss.Place(m.width, footerH, lipgloss.Center, lipgloss.Bottom, footer)
	return top + "\n" + middle + "\n" + bottom
}

func (m Model) viewHeader(w int) string {
	title := appTitleStyle.Render("OLT MONITOR")
	rule := appSubtitleStyle.Render(strings.Repeat("─", maxInt(0, w-lipgloss.Width(title)-1)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, " "+rule)
}

func (m Model) viewTable(w int) string {
	// panelStyle.Width controls the panel's outside width. Its border (2) and
	// horizontal padding (4) leave this much space for the table itself.
	panelWidth := w - 4
	inner := panelWidth - 2 - 4
	if len(m.olts) == 0 {
		return panelStyle.Width(panelWidth).Render(mutedStyle.Render("No OLTs configured yet.  Press  a  to add one."))
	}

	// The table has two leading cursor columns and three column separators.
	// Subtract the cursor width so the data columns fit inside the panel.
	cursorW := 2
	inner -= cursorW
	nameW, ipW, statusW, stateW := 14, 15, 7, 8
	if extra := inner - (3 + nameW + ipW + statusW + stateW); extra > 0 {
		share := extra / 4
		nameW += share
		ipW += share
		statusW += share
		stateW += extra - 3*share
	}

	var rows strings.Builder
	head := fmt.Sprintf("  %-*s %-*s %-*s %-*s", nameW, "NAME", ipW, "IP ADDRESS", statusW, "STATUS", stateW, "STATE")
	rows.WriteString(headStyle.Render(head))
	rows.WriteString("\n")
	rows.WriteString(appSubtitleStyle.Render(strings.Repeat("─", lipgloss.Width(head))))
	rows.WriteString("\n")

	for i, o := range m.olts {
		cursor := "  "
		style := rowStyle
		if !o.Enabled {
			style = dimRowStyle
		}
		if i == m.cursor {
			cursor = cursorGlyph + " "
			style = selRowStyle
		}

		var statusText string
		switch {
		case !o.Enabled:
			statusText = mutedStyle.Render("○ --")
		case o.Status == oltconfig.StatusUp:
			statusText = okStyle.Render("● UP")
		case o.Status == oltconfig.StatusDown:
			statusText = badStyle.Render("● DOWN")
		default:
			statusText = mutedStyle.Render("○ --")
		}

		state := okStyle.Render("Enabled")
		if !o.Enabled {
			state = mutedStyle.Render("Disabled")
		}

		nameIP := style.Render(fmt.Sprintf("%-*s %-*s", nameW, truncate(o.Name, nameW), ipW, o.IP))
		rows.WriteString(cursor + nameIP + " " + padVisible(statusText, statusW) + " " + padVisible(state, stateW) + "\n\n")
	}

	return panelStyle.Width(panelWidth).Render(strings.TrimRight(rows.String(), "\n"))
}

// padVisible right-pads a string that already contains ANSI styling, using
// its visible (rune) width rather than byte length.
func padVisible(s string, width int) string {
	vis := lipgloss.Width(s)
	if vis >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vis)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func (m Model) viewForm(w int, title, value, subtitle string) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render(title) + "\n")
	if subtitle != "" {
		b.WriteString(mutedStyle.Render(subtitle) + "\n")
	}
	b.WriteString("\n" + accentStyle.Render("❯ ") + value + dimStyle.Render("▏"))
	return panelStyle.Width(w - 4).Render(b.String())
}

func helpEntry(key, label string) string {
	return helpKeyStyle.Render(key) + statusStyle.Render(" "+label)
}

func (m Model) viewFooter(w int) string {
	last := "--"
	if !m.lastTick.IsZero() {
		last = m.lastTick.Format("15:04:05")
	}

	// WhatsApp status indicator
	var waStatus string
	switch {
	case m.waConnected:
		waStatus = okStyle.Render("WA ● connected")
	case m.waHasSavedSession:
		waStatus = warnStyle.Render("WA ○ reconnecting…")
	default:
		waStatus = mutedStyle.Render("WA ○ not linked")
	}

	sep := helpSepStyle.Render("   ")
	help := strings.Join([]string{
		helpEntry("a", "add"),
		helpEntry("e", "edit"),
		helpEntry("d", "toggle"),
		helpEntry("r", "remove"),
		helpEntry("i", "interval"),
		helpEntry("w", "whatsapp"),
		helpEntry("q", "quit"),
	}, sep)

	meta := metaStyle.Render(fmt.Sprintf("Last check %s", last))
	helpW := lipgloss.Width(help)
	metaW := lipgloss.Width(meta)
	gap := w - helpW - metaW - 4 // 2 spaces padding on each side of the separator
	if gap < 34 {
		gap = 34
	}
	ruler := dimStyle.Render(strings.Repeat("─", gap))
	return waStatus + "  " + help + "  " + ruler + "  " + meta
}

// --- WhatsApp pairing-code screens ---

func (m Model) viewPhoneInput() string {
	var b strings.Builder
	b.WriteString(appTitleStyle.Render("LINK WHATSAPP · PHONE NUMBER"))
	b.WriteString("\n\n")
	b.WriteString(m.viewFormPlain("Enter your number in international format, e.g. +923001234567", m.input))
	if m.errMsg != "" {
		b.WriteString("\n\n" + errStyle.Render("⚠  "+m.errMsg))
	}
	b.WriteString("\n\n" + helpEntry("Enter", "submit") + helpSepStyle.Render("   ") + helpEntry("Esc", "cancel"))
	return panelStyle.Render(b.String())
}

func (m Model) viewPhoneCode() string {
	var b strings.Builder
	b.WriteString(appTitleStyle.Render("LINK WHATSAPP · ENTER THIS CODE"))
	b.WriteString("\n\n")
	if m.waPairCode != "" {
		b.WriteString(codeStyle.Render(m.waPairCode))
	} else {
		b.WriteString(dimStyle.Render("Requesting code…"))
	}
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("On your phone: WhatsApp → Link a device → Link with phone number instead"))
	if m.waLoginErr != "" {
		b.WriteString("\n\n" + errStyle.Render(m.waLoginErr))
	}
	b.WriteString("\n\n" + helpEntry("Esc", "cancel"))
	return panelStyle.Render(b.String())
}

func (m Model) viewWhatsAppTarget() string {
	var b strings.Builder
	b.WriteString(appTitleStyle.Render("WHATSAPP LINKED · SET ADMIN NUMBER"))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("This number receives DOWN/RECOVERED alerts and can send commands\n(status, add, remove, interval, ping…)."))
	b.WriteString("\n\n")
	b.WriteString(m.viewFormPlain("Admin phone number, international format", m.input))
	if m.errMsg != "" {
		b.WriteString("\n\n" + errStyle.Render("⚠  "+m.errMsg))
	}
	b.WriteString("\n\n" + helpEntry("Enter", "save") + helpSepStyle.Render("   ") + helpEntry("Esc", "skip for now"))
	return panelStyle.Render(b.String())
}

// viewFormPlain is the unboxed version of viewForm used inside screens that
// already sit inside their own panel, to avoid nesting a panel in a panel.
func (m Model) viewFormPlain(title, value string) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render(title) + "\n\n")
	b.WriteString(accentStyle.Render("❯ ") + value + dimStyle.Render("▏"))
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
