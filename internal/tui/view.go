package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Minimal, low-noise palette: one dark-pink primary accent, a neutral text
// scale, and the three status colors. Hierarchy comes from weight/spacing
// rather than piling on more colors.
const (
	colAccent = "#FF1493" // dark pink — primary color: title & accents
	colGreen  = "#4ADE80"
	colRed    = "#F87171"
	colYellow = "#FBBF24"
	colFg     = "#E4E4E7" // primary text
	colFg2    = "#A1A1AA" // secondary text
	colFg3    = "#6B7280" // tertiary / hint text
	colBorder = "#3F3F46"
	colSelBg  = "#1F1F24" // subtle highlight behind the selected table row
)

var (
	appTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colAccent))
	appSubtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colBorder))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colBorder)).
			Padding(1, 2)

	// cardStyle is the panel used by the three dashboard cards: more inner
	// padding so content breathes inside the larger, terminal-filling boxes.
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colBorder)).
			Padding(1, 3)

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
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg2))

	headStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colFg3))
	selRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg)).Bold(true)
	rowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg))
	dimRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg3))
	cursorGlyph = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)).Bold(true)

	codeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colYellow)).Bold(true).
			Padding(0, 3).
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(colYellow))
)

// Sizing rules for the terminal-filling layout:
//   - content width = terminal minus a 2-cell margin on each side, never
//     capped, so cards scale with the window;
//   - minimums keep the layout readable in small windows.
const (
	minContentWidth = 40
	minMainHeight   = 20 // top cards + gap + table
	minTopHeight    = 10 // label + blank + 4 lines + padding + border
	minTableHeight  = 9  // header + separator + a few rows + padding + border
	cardGap         = 2  // cells between the two top cards
)

// contentWidth is the usable width for the cards: the terminal minus a
// 2-cell margin on each side. Never capped — cards stretch to fill wide
// terminals and shrink gracefully in narrow ones.
func (m Model) contentWidth() int {
	if m.width <= 0 {
		return minContentWidth
	}
	w := m.width - 4
	if w < minContentWidth {
		w = minContentWidth
	}
	return w
}

// sized returns a copy of st configured so the rendered block occupies
// exactly total cells horizontally and h cells vertically, borders
// included. In lipgloss, Width/Height cover content + padding; borders are
// drawn outside them, so they are subtracted here.
func sized(st lipgloss.Style, total, h int) lipgloss.Style {
	w := total - st.GetHorizontalBorderSize()
	if w < 1 {
		w = 1
	}
	st = st.Width(w)
	if h > 0 {
		v := h - st.GetVerticalBorderSize()
		if v < 1 {
			v = 1
		}
		st = st.Height(v)
	}
	return st
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
	}

	w := m.contentWidth()
	header := m.viewHeader(maxInt(0, m.width-4))
	footer := m.viewFooter()

	// Vertical budget: everything between the header and footer is split —
	// the top cards take ~30%, the table fills the rest (minus a gap).
	availH := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 2
	if availH < minMainHeight {
		availH = minMainHeight
	}
	topH := availH * 30 / 100
	if topH < minTopHeight {
		topH = minTopHeight
	}
	tableH := availH - topH - cardGap
	if tableH < minTableHeight {
		tableH = minTableHeight
	}

	var main strings.Builder
	main.WriteString(m.viewCards(w, topH))
	main.WriteString("\n\n")
	main.WriteString(m.viewRowsPanel(w, tableH))

	switch m.mode {
	case ModeEditMessage:
		main.WriteString("\n\n" + m.viewForm(w, "Edit auto-reply message  (sent back to every incoming message)", m.input))
	}

	if m.errMsg != "" {
		main.WriteString("\n\n" + errStyle.Render("⚠  "+m.errMsg))
	}

	// Show transient WhatsApp errors (e.g. session expired, connect failed)
	// on the main dashboard so the user sees them without switching screens.
	if m.waLoginErr != "" && m.mode == ModeMain {
		main.WriteString("\n\n" + warnStyle.Render("⚠  "+m.waLoginErr))
	}

	return m.dashboard(header, main.String(), footer)
}

// dashboard keeps the persistent parts anchored: the title at the top, the
// status panel in the available middle space, and the helpers at the bottom.
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
	title := appTitleStyle.Render("BILLS OS")
	rule := appSubtitleStyle.Render(strings.Repeat("─", maxInt(0, w-lipgloss.Width(title)-1)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, " "+rule)
}

// viewCards renders card 1 (WhatsApp status) and card 2 (session message
// counters) side by side, each taking ~50% of the available width and the
// given height — both derive from the same sized() call, so they are always
// exactly the same size regardless of their content.
func (m Model) viewCards(w, h int) string {
	cardW := (w - cardGap) / 2
	if cardW < 20 {
		cardW = 20
	}
	st := sized(cardStyle, cardW, h)

	// --- card 1: WhatsApp status (same data as before) ---
	var waStatus string
	switch {
	case m.waConnected:
		waStatus = okStyle.Render("● connected")
	case m.waHasSavedSession:
		waStatus = warnStyle.Render("○ reconnecting…")
	default:
		waStatus = mutedStyle.Render("○ not linked")
	}
	number := m.waOwnNumber
	if number == "" {
		number = "—"
	}

	var c1 strings.Builder
	c1.WriteString(labelStyle.Render("WHATSAPP"))
	c1.WriteString("\n\n")
	c1.WriteString(waStatus + "\n")
	c1.WriteString(mutedStyle.Render(number) + "\n")
	settings := m.settings()
	replyState := okStyle.Render("● replying")
	if settings.AutoReplyPaused {
		replyState = warnStyle.Render("⏸ paused")
	}
	c1.WriteString(replyState + "\n")
	if m.status != "" {
		c1.WriteString(statusStyle.Render(m.status))
	} else {
		c1.WriteString(dimStyle.Render("no recent activity"))
	}

	// --- card 2: session counters ---
	var c2 strings.Builder
	c2.WriteString(labelStyle.Render("COUNTERS"))
	c2.WriteString("\n\n")
	c2.WriteString(counterLine("Messages received", m.msgsReceived) + "\n")
	c2.WriteString(counterLine("Replies sent", m.msgsReplied) + "\n")
	c2.WriteString(counterLine("Ignored (paused)", m.msgsIgnored) + "\n")
	// Trailing blank line keeps both cards exactly the same height.
	c2.WriteString("")

	return lipgloss.JoinHorizontal(lipgloss.Top,
		st.Render(c1.String()),
		strings.Repeat(" ", cardGap),
		st.Render(c2.String()))
}

func counterLine(label string, n int) string {
	return fmt.Sprintf("%-18s %s", label, accentStyle.Render(fmt.Sprintf("%3d", n)))
}

// tableCols splits the usable width across the four columns: CUSTOMER 30%,
// PACKAGE 22%, PANEL 20%, STATUS 28%. Any rounding remainder goes to STATUS
// so the columns always sum EXACTLY to usable — never leftover space on the
// right.
func tableCols(usable int) (custW, pkgW, panelW, statusW int) {
	custW = usable * 30 / 100
	pkgW = usable * 22 / 100
	panelW = usable * 20 / 100
	statusW = usable - custW - pkgW - panelW
	return
}

// viewRowsPanel renders card 3: a compact customer table (fake data for now)
// with a cursor that moves with up/down.
//
//   - rows are exactly one line tall, so the pink cursor bar is one cell high
//     and always matches the selected row;
//   - the selected row also gets a subtle full-width highlight;
//   - a title line (with count / scroll position) matches the WHATSAPP and
//     COUNTERS cards above;
//   - when rows don't fit, the list scrolls to keep the selection visible.
func (m Model) viewRowsPanel(w, h int) string {
	content := w - cardStyle.GetHorizontalFrameSize() // cells between border and padding
	if content < 20 {
		content = 20
	}

	// Cursor column (2 cells) sits left of the columns; the rest is split
	// by tableCols so the four columns sum exactly to the remaining width.
	const cursorCol = 2
	usable := content - cursorCol
	custW, pkgW, panelW, statusW := tableCols(usable)

	// Vertical budget. Frame = border (2) + padding (2). Full layout adds
	// title + blank + header + separator (4 lines). In short windows fall
	// back to header + separator only.
	frame := cardStyle.GetVerticalFrameSize()
	full := h-frame-4 >= 3
	availLines := h - frame - 2
	if full {
		availLines = h - frame - 4
	}
	if availLines < 1 {
		availLines = 1
	}

	n := len(m.rows)
	visible := availLines
	if visible > n {
		visible = n
	}

	// Scroll window: keep the selected row visible when rows overflow.
	offset := 0
	if n > visible {
		offset = m.rowCursor - visible/2
		if offset < 0 {
			offset = 0
		}
		if offset > n-visible {
			offset = n - visible
		}
	}

	// cell renders one column: Width-styled so every cell in a column has
	// the same width and rows stay aligned. Non-final columns keep a
	// 1-cell gap before the next column.
	cell := func(width int, text string, last bool) string {
		trim := width
		if !last {
			trim = width - 1
		}
		if trim < 1 {
			trim = 1
		}
		return lipgloss.NewStyle().Width(width).Render(truncate(text, trim))
	}

	var lines []string

	// --- title line: "CUSTOMERS" left, count / scroll position right ---
	if full {
		info := fmt.Sprintf("%d total", n)
		if n > visible {
			info = fmt.Sprintf("%d–%d of %d", offset+1, offset+visible, n)
		}
		left := labelStyle.Render("CUSTOMERS")
		right := dimStyle.Render(info)
		gap := content - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 1 {
			gap = 1
		}
		lines = append(lines, left+strings.Repeat(" ", gap)+right, "")
	}

	// --- header + separator ---
	head := strings.Repeat(" ", cursorCol) +
		cell(custW, "CUSTOMER", false) +
		cell(pkgW, "PACKAGE", false) +
		cell(panelW, "PANEL", false) +
		// STATUS is right-aligned so its content hugs the right border.
		lipgloss.NewStyle().Width(statusW).Align(lipgloss.Right).Render(truncate("STATUS", statusW))
	lines = append(lines,
		headStyle.Render(head),
		appSubtitleStyle.Render(strings.Repeat("─", content)),
	)

	// --- rows ---
	if n == 0 {
		lines = append(lines, dimStyle.Render("  No rows"))
	} else {
		for i := offset; i < offset+visible && i < n; i++ {
			r := m.rows[i]
			selected := i == m.rowCursor

			style := rowStyle
			cursor := strings.Repeat(" ", cursorCol)
			if selected {
				style = selRowStyle.Background(lipgloss.Color(colSelBg))
			}

			// Status: dot + label, colored per state. Carries the row
			// background so the highlight is unbroken to the right edge.
			var stStyle lipgloss.Style
			switch r.Status {
			case "DONE":
				stStyle = okStyle
			case "PENDING":
				stStyle = warnStyle
			default:
				stStyle = mutedStyle
			}
			if selected {
				stStyle = stStyle.Background(lipgloss.Color(colSelBg))
			}
			st := stStyle.Render("● " + r.Status)

			cells := cell(custW, r.Customer, false) +
				cell(pkgW, r.Package, false) +
				cell(panelW, r.Panel, false) +
				lipgloss.NewStyle().Width(statusW).Align(lipgloss.Right).Render(st)

			if selected {
				bar := cursorGlyph.Background(lipgloss.Color(colSelBg)).Render("▍")
				lines = append(lines, bar+style.Render(" "+cells))
			} else {
				lines = append(lines, cursor+style.Render(cells))
			}
		}
	}

	return sized(cardStyle, w, h).Render(strings.Join(lines, "\n"))
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

func (m Model) viewForm(w int, title, value string) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render(title) + "\n")
	b.WriteString("\n" + accentStyle.Render("❯ ") + value + dimStyle.Render("▏"))
	return sized(panelStyle, w, 0).Render(b.String())
}

func helpEntry(key, label string) string {
	return helpKeyStyle.Render(key) + statusStyle.Render(" "+label)
}

// viewFooter pins the key helpers to the far left corner and the linked
// number to the far right corner of the terminal, with a full-width rule
// filling everything between them.
func (m Model) viewFooter() string {
	sep := helpSepStyle.Render("   ")
	help := strings.Join([]string{
		helpEntry("w", "link whatsapp"),
		helpEntry("m", "edit reply"),
		helpEntry("t", "pause / resume"),
		helpEntry("↑↓", "select"),
		helpEntry("q", "quit"),
	}, sep)

	meta := "Bills OS"
	if m.waOwnNumber != "" && m.waConnected {
		meta = m.waOwnNumber
	}
	metaText := metaStyle.Render(meta)

	// Same 2-space margin on both sides as the heading above.
	total := m.width - 4
	if total <= 0 {
		total = minContentWidth
	}
	gap := total - lipgloss.Width(help) - lipgloss.Width(metaText) - 4 // 2 spaces on each side of the rule
	if gap < 3 {
		gap = 3
	}
	ruler := dimStyle.Render(strings.Repeat("─", gap))
	return help + "  " + ruler + "  " + metaText
}

var metaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colFg))

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