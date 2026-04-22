//go:build integration
// +build integration

package integrationptysession_test

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostNormalModeNoUnexpectedTrailingSpacesInCopyModel(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	const (
		cols = 100
		rows = 26
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "copy_model_trailing_normal",
		SessionName: "copy_model_trailing_normal",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})

	waitForHost(t, h, "copy_model_trailing_normal", 3*time.Second)
	_ = host.DrainRaw()

	host.Send("i=1; while [ $i -le 320 ]; do c=$((31 + (i % 6))); if [ $((i % 2)) -eq 0 ]; then printf '\\033[%smLONG-%03d-abcdefghijklmnopqrstuvwxyz0123456789\\033[0m\\n' \"$c\" \"$i\"; else printf '\\033[%smS%03d\\033[0m\\n' \"$c\" \"$i\"; fi; i=$((i+1)); done; echo __COPY_MODEL_DONE__\n")

	raw := waitAndCollectRawUntil(t, host, "__COPY_MODEL_DONE__", 5*time.Second, 25*time.Millisecond)
	model := parseANSICopyModel(raw, cols)

	type issue struct {
		row      int
		trailing int
		line     string
	}
	var issues []issue
	for row := 1; row <= model.maxRow; row++ {
		line, rightExplicit, rightNonSpace := model.lineSummary(row)
		if !strings.Contains(line, "LONG-") && !strings.Contains(line, "S") {
			continue
		}
		if rightExplicit <= 0 {
			continue
		}
		trailing := rightExplicit - rightNonSpace
		if trailing > 0 {
			issues = append(issues, issue{
				row:      row,
				trailing: trailing,
				line:     line[:rightExplicit],
			})
		}
	}
	if len(issues) > 0 {
		first := issues[0]
		t.Fatalf("copy-model trailing spaces reproduced row=%d trailing=%d line=%q issues=%d", first.row, first.trailing, first.line, len(issues))
	}
}

func TestHostNormalModeLSColorNoUnexpectedTrailingSpacesInCopyModel(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	const (
		cols = 100
		rows = 26
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "copy_model_trailing_ls",
		SessionName: "copy_model_trailing_ls",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})

	waitForHost(t, h, "copy_model_trailing_ls", 3*time.Second)
	_ = host.DrainRaw()

	host.Send("COLUMNS=100 ls -la --color=always | sed -n '1,30p'; echo __LS_COPY_MODEL_DONE__\n")

	raw := waitAndCollectRawUntil(t, host, "__LS_COPY_MODEL_DONE__", 5*time.Second, 25*time.Millisecond)
	model := parseANSICopyModel(raw, cols)

	type issue struct {
		row      int
		trailing int
		line     string
	}
	var issues []issue
	for row := 1; row <= model.maxRow; row++ {
		line, rightExplicit, rightNonSpace := model.lineSummary(row)
		trim := strings.TrimLeft(line, " ")
		if trim == "" || strings.HasPrefix(trim, "total ") || strings.Contains(trim, "__LS_COPY_MODEL_DONE__") {
			continue
		}
		// ls -la rows usually start with permissions like drwx or -rw.
		if !(strings.HasPrefix(trim, "d") || strings.HasPrefix(trim, "-") || strings.HasPrefix(trim, "l")) {
			continue
		}
		if rightExplicit <= 0 {
			continue
		}
		trailing := rightExplicit - rightNonSpace
		if trailing > 0 {
			issues = append(issues, issue{
				row:      row,
				trailing: trailing,
				line:     line[:rightExplicit],
			})
		}
	}
	if len(issues) > 0 {
		first := issues[0]
		t.Fatalf("ls copy-model trailing spaces reproduced row=%d trailing=%d line=%q issues=%d", first.row, first.trailing, first.line, len(issues))
	}
}

func waitAndCollectRawUntil(t *testing.T, sess *ptytest.PTYSession, marker string, timeout, step time.Duration) string {
	t.Helper()
	var b strings.Builder
	deadline := sess.Clock().Now().Add(timeout)
	for sess.Clock().Now().Before(deadline) {
		chunk := sess.DrainRaw()
		if chunk != "" {
			b.WriteString(chunk)
			if strings.Contains(b.String(), marker) {
				return b.String()
			}
		}
		advanceTestClock(sess.Clock(), step)
	}
	t.Fatalf("timed out waiting for marker %q", marker)
	return ""
}

type copyCell struct {
	r        rune
	explicit bool
}

type ansiCopyModel struct {
	cols   int
	row    int
	col    int
	rows   map[int][]copyCell
	maxRow int
}

func newANSICopyModel(cols int) *ansiCopyModel {
	return &ansiCopyModel{
		cols: cols,
		row:  1,
		col:  1,
		rows: make(map[int][]copyCell),
	}
}

func (m *ansiCopyModel) ensureRow(row int) []copyCell {
	if row < 1 {
		row = 1
	}
	cells, ok := m.rows[row]
	if !ok {
		cells = make([]copyCell, m.cols)
		m.rows[row] = cells
	}
	if row > m.maxRow {
		m.maxRow = row
	}
	return cells
}

func (m *ansiCopyModel) writeRune(r rune) {
	if m.row < 1 {
		m.row = 1
	}
	if m.col < 1 {
		m.col = 1
	}
	if m.col > m.cols {
		return
	}
	width := runewidth.RuneWidth(r)
	if width <= 0 {
		width = 1
	}
	cells := m.ensureRow(m.row)
	i := m.col - 1
	cells[i] = copyCell{r: r, explicit: true}
	for w := 1; w < width && m.col+w <= m.cols; w++ {
		cells[m.col+w-1] = copyCell{r: 0, explicit: true}
	}
	m.col += width
}

func (m *ansiCopyModel) clearLine(mode int) {
	cells := m.ensureRow(m.row)
	switch mode {
	case 1:
		end := m.col
		if end > m.cols {
			end = m.cols
		}
		for i := 0; i < end; i++ {
			cells[i] = copyCell{}
		}
	case 2:
		for i := range cells {
			cells[i] = copyCell{}
		}
	default:
		start := m.col - 1
		if start < 0 {
			start = 0
		}
		if start >= m.cols {
			return
		}
		for i := start; i < m.cols; i++ {
			cells[i] = copyCell{}
		}
	}
}

func (m *ansiCopyModel) lineSummary(row int) (line string, rightExplicit int, rightNonSpace int) {
	cells := m.ensureRow(row)
	var b strings.Builder
	b.Grow(m.cols)
	for i := 0; i < m.cols; i++ {
		ch := cells[i].r
		if ch == 0 {
			ch = ' '
		}
		b.WriteRune(ch)
		if cells[i].explicit {
			rightExplicit = i + 1
			if ch != ' ' {
				rightNonSpace = i + 1
			}
		}
	}
	return b.String(), rightExplicit, rightNonSpace
}

func parseANSICopyModel(raw string, cols int) *ansiCopyModel {
	m := newANSICopyModel(cols)
	data := []byte(raw)
	for i := 0; i < len(data); {
		b := data[i]
		if b == 0x1b {
			if i+1 >= len(data) {
				break
			}
			switch data[i+1] {
			case '[':
				j := i + 2
				for ; j < len(data); j++ {
					c := data[j]
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
				if j >= len(data) {
					return m
				}
				params := string(data[i+2 : j])
				final := data[j]
				switch final {
				case 'H', 'f':
					r, c := parseCSIPos(params)
					if r < 1 {
						r = 1
					}
					if c < 1 {
						c = 1
					}
					m.row, m.col = r, c
				case 'K':
					m.clearLine(parseCSIInt(params, 0))
				}
				i = j + 1
				continue
			case ']':
				// OSC ... BEL or ST
				j := i + 2
				for ; j < len(data); j++ {
					if data[j] == 0x07 {
						j++
						break
					}
					if data[j] == 0x1b && j+1 < len(data) && data[j+1] == '\\' {
						j += 2
						break
					}
				}
				i = j
				continue
			default:
				i += 2
				continue
			}
		}
		switch b {
		case '\r':
			m.col = 1
			i++
			continue
		case '\n':
			m.row++
			if m.row > m.maxRow {
				m.maxRow = m.row
			}
			i++
			continue
		}
		if b < 0x20 {
			i++
			continue
		}
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		m.writeRune(r)
		i += size
	}
	return m
}

func parseCSIInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

func parseCSIPos(s string) (int, int) {
	if s == "" {
		return 1, 1
	}
	parts := strings.Split(s, ";")
	row := 1
	col := 1
	if len(parts) > 0 && parts[0] != "" {
		if v, err := strconv.Atoi(parts[0]); err == nil {
			row = v
		}
	}
	if len(parts) > 1 && parts[1] != "" {
		if v, err := strconv.Atoi(parts[1]); err == nil {
			col = v
		}
	}
	return row, col
}
