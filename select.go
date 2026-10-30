package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// The diff pane is selected with the mouse as a terminal would, but within the pane: a drag
// selects, a double click a word, a triple click a line. The selection is copied to the
// clipboard as the button is released, without the +, - or space that starts a line of a diff.

const (
	multiClick = 400 * time.Millisecond
	endOfLine  = math.MaxInt32
	tabWidth   = 4
)

// textPos is a cell of a line of m.lines, tabs expanded.
type textPos struct{ line, col int }

func (a textPos) before(b textPos) bool { return a.line < b.line || a.line == b.line && a.col < b.col }

type selection struct {
	from, to  textPos // to is excluded; nothing is selected when they are equal
	anchor    textPos // where the drag started
	dragging  bool
	edge      int  // -1 above the pane, 1 below it, while a drag is held there
	scrolling bool // a dragScrollMsg is on its way
	clickAt   time.Time
	clickPos  textPos
	clicks    int
}

func (s selection) empty() bool { return s.from == s.to }

// segment is a row of the diff pane: the part of a line shown there, from cell col.
type segment struct {
	line, col int
	indent    int // the space before the continuation of a wrapped line
	text      string
	style     lipgloss.Style
	last      bool // the last part of its line
}

type dragScrollMsg struct{}

type copiedMsg struct {
	text string
	err  error
}

// redrawMsg only makes the screen render again, once a notice has expired.
type redrawMsg struct{}

// copyToClipboard hands text to the terminal's clipboard with OSC 52, through tmux or screen
// when running inside them.
var copyToClipboard = func(text string) error {
	seq := osc52.New(text)
	switch {
	case os.Getenv("TMUX") != "":
		seq = seq.Tmux()
	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
		seq = seq.Screen()
	}
	_, err := seq.WriteTo(os.Stdout)
	return err
}

// setLines replaces the content of the diff pane; the selection goes with the text it was made in.
func (m *model) setLines(lines []diffLine, isDiff bool) {
	if isDiff != m.isDiff || !sameText(lines, m.lines) {
		m.sel = selection{}
	}
	m.lines, m.isDiff = lines, isDiff
}

func sameText(a, b []diffLine) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].text != b[i].text {
			return false
		}
	}
	return true
}

func (m *model) diffMouse(msg tea.MouseMsg) tea.Cmd {
	row, x := msg.Y-2, msg.X-m.leftWidth()-1
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return nil
		}
		p, ok := m.posAt(m.diff.YOffset+row, x)
		if !ok || row < 0 {
			m.clearSelection()
			return nil
		}
		now := time.Now()
		if now.Sub(m.sel.clickAt) < multiClick && p == m.sel.clickPos && m.sel.clicks < 3 {
			m.sel.clicks++
		} else {
			m.sel.clicks = 1
		}
		m.sel.clickAt, m.sel.clickPos = now, p
		switch m.sel.clicks {
		case 1:
			m.sel.anchor, m.sel.from, m.sel.to = p, p, p
			m.sel.dragging = true
		case 2:
			m.sel.from, m.sel.to = m.wordAt(p)
		default:
			m.sel.from, m.sel.to = textPos{p.line, 0}, textPos{p.line, endOfLine}
		}
		m.paintSelection()
		if m.sel.clicks > 1 {
			return m.copySelection()
		}
	case tea.MouseActionMotion:
		if !m.sel.dragging {
			return nil
		}
		m.sel.edge = 0
		if row < 0 {
			m.sel.edge = -1
		} else if row >= m.diff.Height {
			m.sel.edge = 1
		}
		m.dragTo(row, x)
		if m.sel.edge != 0 && !m.sel.scrolling {
			m.sel.scrolling = true
			return dragScroll()
		}
	case tea.MouseActionRelease:
		if !m.sel.dragging {
			return nil
		}
		m.sel.dragging, m.sel.edge = false, 0
		if !m.sel.empty() {
			return m.copySelection()
		}
	}
	return nil
}

func dragScroll() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return dragScrollMsg{} })
}

// dragScrolled scrolls the diff on while a drag is held above or below it.
func (m *model) dragScrolled() tea.Cmd {
	m.sel.scrolling = false
	if !m.sel.dragging || m.sel.edge == 0 {
		return nil
	}
	if m.sel.edge < 0 {
		m.diff.ScrollUp(1)
		m.dragTo(-1, 0)
	} else {
		m.diff.ScrollDown(1)
		m.dragTo(m.diff.Height, 0)
	}
	m.sel.scrolling = true
	return dragScroll()
}

// dragTo stretches the selection from its anchor to the cell under the pointer, both included.
// Above the pane, it reaches the start of the top row; below, the end of the bottom one.
func (m *model) dragTo(row, x int) {
	switch {
	case row < 0:
		row, x = 0, 0
	case row >= m.diff.Height:
		row, x = m.diff.Height-1, endOfLine
	}
	p, ok := m.posAt(m.diff.YOffset+row, x)
	if !ok {
		return
	}
	a := m.sel.anchor
	if p.before(a) {
		m.sel.from, m.sel.to = p, textPos{a.line, a.col + 1}
	} else {
		m.sel.from, m.sel.to = a, textPos{p.line, p.col + 1}
	}
	m.paintSelection()
}

// posAt is the cell of the text shown at column x of a row of the pane. Past the end of a
// line, it is the end of the line, even of the part a narrow pane cuts off.
func (m *model) posAt(row, x int) (textPos, bool) {
	if len(m.segs) == 0 {
		return textPos{}, false
	}
	if row >= len(m.segs) {
		return textPos{m.segs[len(m.segs)-1].line, endOfLine}, true
	}
	s := m.segs[max(row, 0)]
	c := max(0, x-s.indent)
	if w := runewidth.StringWidth(s.text); c >= w {
		if s.last {
			return textPos{s.line, endOfLine}, true
		}
		c = w
	}
	return textPos{s.line, s.col + c}, true
}

// wordAt is the run of letters and digits, of spaces or of the same punctuation around p.
func (m *model) wordAt(p textPos) (textPos, textPos) {
	runes := []rune(expandTabs(m.lines[p.line].text))
	cells := make([]int, len(runes)+1)
	at := -1
	for i, r := range runes {
		cells[i+1] = cells[i] + runewidth.RuneWidth(r)
		if at < 0 && p.col < cells[i+1] {
			at = i
		}
	}
	if at < 0 {
		return p, p
	}
	class := func(r rune) int {
		switch {
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			return 0
		case unicode.IsSpace(r):
			return 1
		}
		return 2 + int(r)
	}
	c := class(runes[at])
	i, j := at, at+1
	for i > 0 && class(runes[i-1]) == c {
		i--
	}
	for j < len(runes) && class(runes[j]) == c {
		j++
	}
	return textPos{p.line, cells[i]}, textPos{p.line, cells[j]}
}

// selectedCells is the part of a line within the selection, from a to b excluded. The
// first cell of a line of a diff, its +, - or space, is never selected.
func (m *model) selectedCells(line int) (a, b int) {
	s := m.sel
	if s.empty() || line < s.from.line || line > s.to.line {
		return 0, 0
	}
	a, b = 0, endOfLine
	if line == s.from.line {
		a = s.from.col
	}
	if line == s.to.line {
		b = s.to.col
	}
	if m.isDiff && isCode(m.lines[line].text) {
		a = max(a, 1)
	}
	return a, b
}

func isCode(text string) bool {
	return text != "" && (text[0] == '+' || text[0] == '-' || text[0] == ' ')
}

func (m *model) selectedText() string {
	var b strings.Builder
	for i := m.sel.from.line; i <= m.sel.to.line && i < len(m.lines); i++ {
		if i > m.sel.from.line {
			b.WriteByte('\n')
		}
		from, to := m.selectedCells(i)
		b.WriteString(cutCells(m.lines[i].text, from, to))
	}
	return b.String()
}

func (m *model) copySelection() tea.Cmd {
	text := m.selectedText()
	if text == "" {
		return nil
	}
	return func() tea.Msg { return copiedMsg{text, copyToClipboard(text)} }
}

func (m *model) copied(msg copiedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = "copy failed: " + msg.err.Error()
	} else if n := strings.Count(msg.text, "\n") + 1; n > 1 {
		m.notice = fmt.Sprintf("copied %d lines", n)
	} else {
		m.notice = fmt.Sprintf("copied %d characters", len([]rune(msg.text)))
	}
	m.noticeUntil = time.Now().Add(2 * time.Second)
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return redrawMsg{} })
}

func (m *model) clearSelection() {
	if !m.sel.empty() || m.sel.dragging {
		m.sel = selection{}
		m.paintSelection()
	}
}

// paintSelection shows the selection over the rows already rendered.
func (m *model) paintSelection() {
	out := make([]string, len(m.painted))
	copy(out, m.painted)
	if !m.sel.empty() {
		for i, s := range m.segs {
			if s.line >= m.sel.from.line && s.line <= m.sel.to.line {
				out[i] = m.renderSegment(s)
			}
		}
	}
	m.diff.SetContent(strings.Join(out, "\n"))
}

func (m *model) renderSegment(s segment) string {
	a, b := m.selectedCells(s.line)
	w := runewidth.StringWidth(s.text)
	la, lb := max(a-s.col, 0), min(b-s.col, w)
	pad := strings.Repeat(" ", s.indent)
	if la >= lb {
		return s.style.Render(pad + s.text)
	}
	out := pad
	if la > 0 {
		out += s.style.Render(cutCells(s.text, 0, la))
	}
	out += styleSelected.Render(cutCells(s.text, la, lb))
	if lb < w {
		out += s.style.Render(cutCells(s.text, lb, w))
	}
	return out
}

// cutCells returns the characters of s within the cells from a to b excluded, a tab
// counting for tabWidth cells, as it is shown.
func cutCells(s string, a, b int) string {
	var out strings.Builder
	pos, kept := 0, false
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = tabWidth
		}
		if w == 0 {
			if kept {
				out.WriteRune(r)
			}
			continue
		}
		kept = pos < b && pos+w > a
		if kept {
			out.WriteRune(r)
		}
		pos += w
		if pos >= b {
			break
		}
	}
	return out.String()
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth)) }
