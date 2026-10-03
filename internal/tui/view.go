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

	// cardStyle is the panel used by the dashboard cards: more inner
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
	case ModeChatSelect:
		return m.frame(m.viewChatSelect())
	}

	w := m.contentWidth()
	header := m.viewHeader(maxInt(0, m.width-4))
	footer := m.viewFooter()

	// Vertical budget: everything between the header and footer is split —
	// the top cards take ~30%, the log table fills the rest (minus a gap).
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
	main.WriteString(m.viewLogPanel(w, tableH))

	if m.errMsg != "" {
		main.WriteString("\n\n" + errStyle.Render("⚠  "+oneLine(m.errMsg)))
	}

	// Show transient WhatsApp errors (e.g. session expired, connect failed)
	// on the main dashboard so the user sees them without switching screens.
	if m.waLoginErr != "" && m.mode == ModeMain {
		main.WriteString("\n\n" + warnStyle.Render("⚠  "+oneLine(m.waLoginErr)))
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

// viewCards renders card 1 (WhatsApp + monitoring status) and card 2
// (session counters) side by side, each taking ~50% of the available width
// and the given height — both derive from the same sized() call, so they
// are always exactly the same size regardless of their content.
func (m Model) viewCards(w, h int) string {
	cardW := (w - cardGap) / 2
	if cardW < 20 {
		cardW = 20
	}
	st := sized(cardStyle, cardW, h)

	// --- card 1: WhatsApp + monitoring status ---
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
	monitorState := okStyle.Render(fmt.Sprintf("● monitoring %d chat(s)", len(m.monitored)))
	if len(m.monitored) == 0 {
		monitorState = warnStyle.Render("○ no chats selected")
	}
	c1.WriteString(monitorState + "\n")
	if m.status != "" {
		c1.WriteString(statusStyle.Render(m.status))
	} else {
		c1.WriteString(dimStyle.Render("no recent activity"))
	}

	// --- card 2: session counters ---
	inFile := 0
	if m.logger != nil {
		inFile = m.logger.Count()
	}
	var c2 strings.Builder
	c2.WriteString(labelStyle.Render("COUNTERS"))
	c2.WriteString("\n\n")
	c2.WriteString(counterLine("Received (all)", m.msgsReceived) + "\n")
	c2.WriteString(counterLine("Saved (session)", m.msgsSaved) + "\n")
	c2.WriteString(counterLine("In JSON file", inFile) + "\n")
	portalStat := fmt.Sprintf("%d ok %d fail", m.portalOK, m.portalErr)
	c2.WriteString(fmt.Sprintf("%-18s %s", "Portal", accentStyle.Render(portalStat)))
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

// logCols assigns intentional widths in row order ID · MESSAGE · SENDER ·
// TIME · PANEL · STATUS. The tail columns are FIXED to their target
// ranges (ID 7, SENDER 16, TIME 11, PANEL 9, STATUS 11) and MESSAGE
// flexes into whatever remains (~50-55% on a ~130-col terminal, shrinking
// on narrower ones) so it can never squeeze the others. The six widths
// always sum EXACTLY to usable — no leftover strip on the right.
func logCols(usable int) (idW, msgW, senderW, timeW, panelW, statusW int) {
	idW, senderW, timeW, panelW, statusW = 7, 16, 11, 9, 11
	msgW = usable - (idW + senderW + timeW + panelW + statusW)
	if msgW < 8 {
		msgW = 8
	}
	// Very narrow terminals: shave one cell at a time (fixed columns
	// first, never below 3) until everything fits exactly.
	for idW+senderW+timeW+panelW+statusW+msgW > usable {
		shrank := false
		for _, p := range []*int{&senderW, &timeW, &panelW, &statusW, &idW} {
			if *p > 3 {
				*p--
				shrank = true
				break
			}
		}
		if !shrank && msgW > 3 {
			msgW--
			shrank = true
		}
		if !shrank {
			break
		}
	}
	return
}

// viewLogPanel renders the message log: every captured message from the
// selected chats, newest at the bottom, scrolling to keep the cursor
// visible. All lines fill the card width exactly.
func (m Model) viewLogPanel(w, h int) string {
	content := w - cardStyle.GetHorizontalFrameSize() // cells between border and padding
	if content < 20 {
		content = 20
	}

	// Cursor column (2 cells) sits left of the columns; the rest is split
	// by logCols so the six columns sum exactly to the remaining width.
	const cursorCol = 2
	usable := content - cursorCol
	idW, msgW, senderW, timeW, panelW, statusW := logCols(usable)

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

	n := len(m.logRows)
	visible := availLines
	if visible > n {
		visible = n
	}

	// Scroll window: keep the selected row visible when rows overflow.
	cursor := m.logRowCursor
	if cursor >= n {
		cursor = maxInt(0, n-1)
	}
	offset := 0
	if n > visible {
		offset = cursor - visible/2
		if offset < 0 {
			offset = 0
		}
		if offset > n-visible {
			offset = n - visible
		}
	}

	// cell renders one column: Width-styled so every cell in a column has
	// the same width and rows stay aligned.
	cell := func(width int, text string, align lipgloss.Position) string {
		trim := width
		if align == lipgloss.Left {
			trim = width - 1
		}
		if trim < 1 {
			trim = 1
		}
		return lipgloss.NewStyle().Width(width).Align(align).Render(truncate(text, trim))
	}

	var lines []string

	// --- title line: "MESSAGE LOG" left, captured count right ---
	if full {
		info := fmt.Sprintf("%d captured", n)
		if n > visible {
			info = fmt.Sprintf("%d–%d of %d", offset+1, offset+visible, n)
		}
		left := labelStyle.Render("MESSAGE LOG")
		right := dimStyle.Render(info)
		gap := content - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 1 {
			gap = 1
		}
		lines = append(lines, left+strings.Repeat(" ", gap)+right, "")
	}

	// --- header + separator ---
	head := strings.Repeat(" ", cursorCol) +
		cell(idW, "ID", lipgloss.Left) +
		cell(msgW, "MESSAGE", lipgloss.Left) +
		cell(senderW, "SENDER", lipgloss.Left) +
		cell(timeW, "TIME", lipgloss.Left) +
		cell(panelW, "PANEL", lipgloss.Left) +
		cell(statusW, "STATUS", lipgloss.Right)
	lines = append(lines,
		headStyle.Render(head),
		appSubtitleStyle.Render(strings.Repeat("─", content)),
	)

	// --- rows ---
	if n == 0 {
		// Centered inside the card body — the panel height math above
		// still holds because Place emits exactly availLines lines.
		lines = append(lines, lipgloss.Place(content, availLines,
			lipgloss.Center, lipgloss.Center, dimStyle.Render("no messages yet")))
	} else {
		for i := offset; i < offset+visible && i < n; i++ {
			r := m.logRows[i]
			selected := i == cursor

			style := rowStyle
			cursorCol2 := strings.Repeat(" ", cursorCol)
			if selected {
				style = selRowStyle.Background(lipgloss.Color(colSelBg))
			}

			status := r.Status
			if status == "" {
				status = StatusWait
			}
			var stStyle lipgloss.Style
			switch status {
			case StatusDone:
				stStyle = okStyle
			case StatusFail:
				stStyle = badStyle
				status = StatusFail
			case StatusPartial:
				stStyle = warnStyle
			default:
				stStyle = warnStyle
				status = StatusWait
			}
			if selected {
				stStyle = stStyle.Background(lipgloss.Color(colSelBg))
			}

			// The MESSAGE column shows only the customer usernames —
			// the full text stays in monitored_messages.json.
			msg := strings.Join(r.Usernames, ", ")
			if msg == "" {
				msg = r.Text
			}

			cells := cell(idW, fmt.Sprintf("%d", r.ID), lipgloss.Left) +
				cell(msgW, msg, lipgloss.Left) +
				cell(senderW, r.Sender, lipgloss.Left) +
				cell(timeW, r.Time, lipgloss.Left) +
				cell(panelW, r.Panel, lipgloss.Left) +
				cell(statusW, stStyle.Render(status), lipgloss.Right)

			if selected {
				bar := cursorGlyph.Background(lipgloss.Color(colSelBg)).Render("▍")
				lines = append(lines, bar+style.Render(" "+cells))
			} else {
				lines = append(lines, cursorCol2+style.Render(cells))
			}
		}
	}

	return sized(cardStyle, w, h).Render(strings.Join(lines, "\n"))
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

// oneLine collapses multi-line status text to a single trimmed line.
// Library errors (Playwright timeouts, for instance) carry a multi-line
// "Call log" that would otherwise spill rows into the card layout.
func oneLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
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
		helpEntry("s", "select chats"),
		helpEntry("w", "link whatsapp"),
		helpEntry("↑↓", "scroll log"),
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
		b.WriteString("\n\n" + errStyle.Render("⚠  "+oneLine(m.errMsg)))
	}
	b.WriteString("\n\n" + helpEntry("Enter", "submit") + helpSepStyle.Render("   ") + helpEntry("Esc", "cancel"))
	return sized(panelStyle, m.contentWidth(), 0).Render(b.String())
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
	return sized(panelStyle, m.contentWidth(), 0).Render(b.String())
}

// viewFormPlain is the unboxed input line used inside screens that already
// sit inside their own panel, to avoid nesting a panel in a panel.
func (m Model) viewFormPlain(title, value string) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render(title) + "\n\n")
	b.WriteString(accentStyle.Render("❯ ") + value + dimStyle.Render("▏"))
	return b.String()
}

// --- Chat selection screen ---

// viewChatSelect is the multi-select picker: every group and contact on
// the account, checkbox per row, only the checked ones get monitored.
func (m Model) viewChatSelect() string {
	w := m.contentWidth()
	var b strings.Builder
	b.WriteString(appTitleStyle.Render("SELECT CHATS TO MONITOR"))
	b.WriteString("\n" + mutedStyle.Render("Only checked chats are captured to the JSON file — nothing is ever sent back"))
	b.WriteString("\n\n")

	if m.chatsLoading {
		b.WriteString(dimStyle.Render("Loading groups and contacts…"))
	} else if len(m.chats) == 0 {
		b.WriteString(dimStyle.Render("No groups or contacts found on this account"))
	} else {
		// Scroll window sized to what fits in the terminal: the panel adds
		// title/subtitle/count/error/keys lines around the list.
		avail := m.height - 16
		if avail < 3 {
			avail = 3
		}
		visible := avail
		if visible > len(m.chats) {
			visible = len(m.chats)
		}
		offset := 0
		if len(m.chats) > visible {
			offset = m.selCursor - visible/2
			if offset < 0 {
				offset = 0
			}
			if offset > len(m.chats)-visible {
				offset = len(m.chats) - visible
			}
		}
		nameW := w - 12 // checkbox + glyph + spaces
		if nameW < 8 {
			nameW = 8
		}
		for i := offset; i < offset+visible; i++ {
			c := m.chats[i]
			checked := m.sel[c.JID]

			box := "[ ]"
			boxStyle := dimStyle
			if checked {
				box = "[x]"
				boxStyle = okStyle
			}
			glyph, glyphStyle := "○", mutedStyle
			if c.IsGroup {
				glyph, glyphStyle = "●", accentStyle
			}

			row := boxStyle.Render(box) + " " + glyphStyle.Render(glyph) + " " + truncate(c.Name, nameW)
			if i == m.selCursor {
				b.WriteString(cursorGlyph.Render("▍") + selRowStyle.Background(lipgloss.Color(colSelBg)).Render(" "+row))
			} else {
				b.WriteString("  " + row)
			}
			b.WriteString("\n")
		}
	}

	selected := 0
	for _, ok := range m.sel {
		if ok {
			selected++
		}
	}
	b.WriteString("\n" + fmt.Sprintf("%s of %d selected",
		accentStyle.Render(fmt.Sprint(selected)), len(m.chats)))

	if m.errMsg != "" {
		b.WriteString("\n" + errStyle.Render("⚠  "+oneLine(m.errMsg)))
	}

	sep := helpSepStyle.Render("   ")
	keys := []string{
		helpEntry("space", "toggle"),
		helpEntry("a", "all"),
		helpEntry("enter", "start monitoring"),
	}
	if m.errMsg != "" && !m.chatsLoading {
		keys = append(keys, helpEntry("r", "retry"))
	}
	keys = append(keys,
		helpEntry("esc", "back"),
		helpEntry("q", "quit"),
	)
	b.WriteString("\n\n" + strings.Join(keys, sep))

	return sized(panelStyle, w, 0).Render(b.String())
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
