package mvu

import (
	"bytes"
	"strings"

	"pkt.systems/lingon/internal/theme"
)

// DrawHelpBox renders a centered dialog box.
func DrawHelpBox(w *bytes.Buffer, cols, rows int, ui theme.TUITheme, lines []string) {
	DrawHelpBoxWithMinWidth(w, cols, rows, ui, lines, HelpBoxMinWidth(cols))
}

// DrawHelpBoxWithMinWidth renders a centered dialog box with an optional minimum width.
func DrawHelpBoxWithMinWidth(w *bytes.Buffer, cols, rows int, ui theme.TUITheme, lines []string, minWidth int) {
	if cols <= 0 || rows <= 0 || len(lines) == 0 {
		return
	}
	wrapped, boxWidth, boxHeight, ok := HelpBoxLayout(cols, rows, lines, minWidth)
	if !ok {
		return
	}
	left := (cols-boxWidth)/2 + 1
	top := (rows-boxHeight)/2 + 1
	if left < 1 {
		left = 1
	}
	if top < 1 {
		top = 1
	}

	colorOn := ui.DialogFg + ui.DialogBg
	colorOff := ui.Reset
	w.WriteString(ui.Reset)
	fill := strings.Repeat(" ", boxWidth)
	for i := 0; i < boxHeight; i++ {
		y := top + i
		if y > rows {
			break
		}
		w.WriteString("\x1b[")
		w.WriteString(itoa(y))
		w.WriteString(";")
		w.WriteString(itoa(left))
		w.WriteString("H")
		w.WriteString(colorOn)
		w.WriteString(fill)
	}
	for i := 0; i < boxHeight-2; i++ {
		y := top + 1 + i
		if y > rows {
			break
		}
		line := ""
		if i < len(wrapped) {
			line = wrapped[i]
		}
		if len(line) > boxWidth-2 {
			line = line[:boxWidth-2]
		}
		w.WriteString("\x1b[")
		w.WriteString(itoa(y))
		w.WriteString(";")
		w.WriteString(itoa(left + 1))
		w.WriteString("H")
		w.WriteString(colorOn)
		w.WriteString(line)
	}
	w.WriteString(colorOff)
}

// HelpBoxLayout computes wrapped help lines and layout measurements.
func HelpBoxLayout(cols, rows int, lines []string, minWidth int) ([]string, int, int, bool) {
	if cols <= 0 || rows <= 0 || len(lines) == 0 {
		return nil, 0, 0, false
	}
	boxWidth := helpBoxWidth(lines)
	maxWidth := HelpBoxMaxWidth(cols)
	if maxWidth > 0 && minWidth > maxWidth {
		minWidth = maxWidth
	}
	if minWidth > boxWidth {
		boxWidth = minWidth
	}
	if maxWidth > 0 && boxWidth > maxWidth {
		boxWidth = maxWidth
	}
	if boxWidth > cols-2 {
		boxWidth = cols - 2
		if boxWidth < 6 {
			return nil, 0, 0, false
		}
	}
	wrapped := wrapLines(lines, boxWidth-2)
	boxHeight := len(wrapped) + 2
	if boxHeight > rows-2 {
		boxHeight = rows - 2
	}
	return wrapped, boxWidth, boxHeight, true
}

// HelpBoxBounds reports the top/bottom row indices of the help box.
func HelpBoxBounds(cols, rows int, lines []string, minWidth int) (int, int, bool) {
	_, _, boxHeight, ok := HelpBoxLayout(cols, rows, lines, minWidth)
	if !ok {
		return 0, 0, false
	}
	top := (rows-boxHeight)/2 + 1
	if top < 1 {
		top = 1
	}
	bottom := top + boxHeight - 1
	if bottom > rows {
		bottom = rows
	}
	return top, bottom, true
}

func helpBoxWidth(lines []string) int {
	max := 0
	for _, line := range lines {
		if len(line) > max {
			max = len(line)
		}
	}
	return max + 4
}

// HelpBoxMinWidth returns a preferred minimum width for help dialogs.
func HelpBoxMinWidth(cols int) int {
	if cols <= 0 {
		return 0
	}
	minWidth := (cols * 3) / 5
	if minWidth < 40 {
		minWidth = 40
	}
	if minWidth > cols-2 {
		minWidth = cols - 2
	}
	if minWidth < 0 {
		minWidth = 0
	}
	return minWidth
}

// HelpBoxMaxWidth returns a preferred maximum width for help dialogs.
func HelpBoxMaxWidth(cols int) int {
	if cols <= 0 {
		return 0
	}
	maxWidth := (cols * 7) / 10
	if maxWidth < 50 {
		maxWidth = 50
	}
	if maxWidth > cols-2 {
		maxWidth = cols - 2
	}
	if maxWidth < 0 {
		maxWidth = 0
	}
	return maxWidth
}

func wrapLines(lines []string, width int) []string {
	if width <= 0 || len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			out = append(out, "")
			continue
		}
		indent := ""
		for i := 0; i < len(line); i++ {
			if line[i] != ' ' {
				if i > 0 {
					indent = line[:i]
				}
				break
			}
		}
		if indent == "" {
			if idx := strings.Index(line, "  "); idx >= 0 {
				indent = strings.Repeat(" ", idx+2)
			}
		}
		cur := line
		first := true
		for len(cur) > width {
			limit := width
			if limit > len(cur) {
				limit = len(cur)
			}
			cut := strings.LastIndexByte(cur[:limit+1], ' ')
			if cut <= 0 || (indent != "" && cut <= len(indent)) {
				cut = limit
			}
			out = append(out, strings.TrimRight(cur[:cut], " "))
			rest := strings.TrimLeft(cur[cut:], " ")
			if rest == "" {
				cur = ""
				break
			}
			if !first && indent != "" {
				cur = indent + rest
			} else if first && indent != "" {
				cur = indent + rest
			} else {
				cur = rest
			}
			first = false
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

// DrawWallBox renders a centered non-blocking wall notification.
// Message content is wrapped and truncated to at most two lines.
func DrawWallBox(w *bytes.Buffer, cols, rows int, ui theme.TUITheme, title, message string) {
	title = strings.TrimSpace(title)
	message = strings.TrimSpace(message)
	if cols <= 0 || rows <= 0 || title == "" {
		return
	}
	boxWidth := HelpBoxMaxWidth(cols)
	if boxWidth <= 0 {
		return
	}
	contentWidth := boxWidth - 2
	if contentWidth <= 0 {
		return
	}
	wrapped := wrapLines([]string{message}, contentWidth)
	msgLines := make([]string, 0, 2)
	for i := 0; i < len(wrapped) && i < 2; i++ {
		msgLines = append(msgLines, wrapped[i])
	}
	if len(wrapped) > 2 && len(msgLines) > 0 {
		msgLines[len(msgLines)-1] = trimWithEllipsis(msgLines[len(msgLines)-1], contentWidth)
	}
	lines := []string{title}
	if len(msgLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, msgLines...)
	}
	DrawHelpBoxWithMinWidth(w, cols, rows, ui, lines, boxWidth)
}

func trimWithEllipsis(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) <= width {
		if len(s)+3 <= width {
			return s + "..."
		}
		if width <= 3 {
			return strings.Repeat(".", width)
		}
		return s[:width-3] + "..."
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	return s[:width-3] + "..."
}

// BannerStyle describes the banner color scheme.
type BannerStyle int

const (
	// BannerRed renders a red background banner.
	BannerRed BannerStyle = iota
	// BannerGreen renders a green background banner.
	BannerGreen
)

// DrawBanner draws a status banner on the top row, right-aligned.
func DrawBanner(w *bytes.Buffer, cols int, message string, style BannerStyle, ui theme.TUITheme) {
	DrawBannerAtRow(w, cols, 1, message, style, ui)
}

// DrawIndicator draws a right-aligned badge without clearing or owning the full row.
func DrawIndicator(w *bytes.Buffer, cols int, message string, style BannerStyle, ui theme.TUITheme) {
	DrawIndicatorAtRow(w, cols, 1, message, style, ui)
}

// ClearRow resets a row to spaces using the current theme reset style.
func ClearRow(w *bytes.Buffer, cols, row int, ui theme.TUITheme) {
	if row < 1 {
		row = 1
	}
	w.WriteString(ui.Reset)
	w.WriteString("\x1b[")
	w.WriteString(itoa(row))
	w.WriteString(";1H")
	w.WriteString("\x1b[2K")
	if cols > 0 {
		w.WriteString(strings.Repeat(" ", cols))
	}
	w.WriteString(ui.Reset)
}

// DrawBannerAtRow draws a status banner on the specified row, right-aligned.
func DrawBannerAtRow(w *bytes.Buffer, cols, row int, message string, style BannerStyle, ui theme.TUITheme) {
	if cols <= 0 || message == "" {
		return
	}
	colorOn := "\x1b[97;41m"
	if style == BannerGreen {
		colorOn = "\x1b[38;2;0;0;0;42m"
	}
	colorOff := ui.Reset
	text := message
	if len(text) > cols {
		text = text[len(text)-cols:]
	}
	startCol := cols - len(text) + 1
	if startCol < 1 {
		startCol = 1
	}
	if row < 1 {
		row = 1
	}
	w.WriteString(ui.Reset)
	w.WriteString("\x1b[")
	w.WriteString(itoa(row))
	w.WriteString(";")
	w.WriteString(itoa(startCol))
	w.WriteString("H")
	w.WriteString(colorOn)
	w.WriteString(text)
	w.WriteString(colorOff)
}

// DrawIndicatorAtRow draws a right-aligned badge without clearing the base row.
func DrawIndicatorAtRow(w *bytes.Buffer, cols, row int, message string, style BannerStyle, ui theme.TUITheme) {
	if cols <= 0 || message == "" {
		return
	}
	colorOn := "\x1b[97;41m"
	if style == BannerGreen {
		colorOn = "\x1b[38;2;0;0;0;42m"
	}
	colorOff := ui.Reset
	text := message
	if len(text) > cols {
		text = text[len(text)-cols:]
	}
	startCol := cols - len(text) + 1
	if startCol < 1 {
		startCol = 1
	}
	if row < 1 {
		row = 1
	}
	w.WriteString(ui.Reset)
	w.WriteString("\x1b[")
	w.WriteString(itoa(row))
	w.WriteString(";")
	w.WriteString(itoa(startCol))
	w.WriteString("H")
	w.WriteString(colorOn)
	w.WriteString(text)
	w.WriteString(colorOff)
}

// DrawTabBasePadAtRow paints a right-aligned pad region in tab-base style.
// It is used to erase stale badge width without repainting the full tab row.
func DrawTabBasePadAtRow(w *bytes.Buffer, cols, row, padTotalLen, messageLen int, ui theme.TUITheme) {
	if cols <= 0 || padTotalLen <= 0 {
		return
	}
	pad := padTotalLen - messageLen
	if pad <= 0 {
		return
	}
	if row < 1 {
		row = 1
	}
	startCol := cols - padTotalLen + 1
	if startCol < 1 {
		pad += startCol - 1
		startCol = 1
	}
	if pad <= 0 {
		return
	}
	if startCol+pad-1 > cols {
		pad = cols - startCol + 1
	}
	if pad <= 0 {
		return
	}
	w.WriteString(ui.Reset)
	w.WriteString("\x1b[")
	w.WriteString(itoa(row))
	w.WriteString(";")
	w.WriteString(itoa(startCol))
	w.WriteString("H")
	w.WriteString(ui.TabBg)
	w.WriteString(ui.TabFg)
	w.WriteString(strings.Repeat(" ", pad))
	w.WriteString(ui.Reset)
}

// DrawTabBar draws a top tab bar with an outrun-inspired palette.
func DrawTabBar(w *bytes.Buffer, cols int, tabs []Tab, active int, ui theme.TUITheme) {
	if cols <= 0 || len(tabs) == 0 {
		return
	}
	baseBg := ui.TabBg
	activeBg := ui.TabActiveBg
	baseFg := ui.TabFg
	mutedFg := ui.TabMutedFg
	mutedActiveFg := ui.TabMutedActiveFg
	activeFg := ui.TabActiveFg
	off := ui.Reset

	w.WriteString("\x1b[1;1H")
	w.WriteString(ui.Reset)
	w.WriteString("\x1b[2K")
	w.WriteString(baseBg)
	w.WriteString(baseFg)
	w.WriteString(strings.Repeat(" ", cols))
	w.WriteString(off)

	col := 1
	for i, tab := range tabs {
		title := tabTitle(tab)
		if title == "" {
			continue
		}
		seg := " " + title + " "
		remain := cols - col + 1
		if remain <= 0 {
			break
		}
		if len(seg) > remain {
			seg = seg[:remain]
		}
		w.WriteString("\x1b[1;")
		w.WriteString(itoa(col))
		w.WriteString("H")
		dim := tab.Disabled || tab.Muted
		italic := tab.Disabled || tab.Muted
		if dim {
			w.WriteString("\x1b[2m")
		}
		if italic {
			w.WriteString("\x1b[3m")
		}
		if i == active {
			w.WriteString(activeBg)
		} else {
			w.WriteString(baseBg)
		}
		if tab.Disabled || tab.Muted {
			if i == active {
				w.WriteString(mutedActiveFg)
			} else {
				w.WriteString(mutedFg)
			}
		} else if i == active {
			w.WriteString(activeFg)
		} else {
			w.WriteString(baseFg)
		}
		w.WriteString(seg)
		w.WriteString(off)
		if dim {
			w.WriteString("\x1b[22m")
		}
		if italic {
			w.WriteString("\x1b[23m")
		}
		col += len(seg)
	}
}

func tabTitle(tab Tab) string {
	name := strings.TrimSpace(tab.Title)
	if name == "" {
		return itoa(tab.Index)
	}
	return name
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	buf := make([]byte, 0, 11)
	for v > 0 {
		buf = append(buf, byte('0'+v%10))
		v /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
