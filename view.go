package main

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

const freshFor = 15 * time.Second

// A restrained palette: muted true colours, readable on dark and light backgrounds. Additions
// and deletions use the terminal's own green and red, those of git diff and lazygit.
func colour(dark, light string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Dark: dark, Light: light}
}

var (
	colAdd   = colour("2", "2")
	colDel   = colour("1", "1")
	colMod   = colour("#D9BC8C", "#8A6420")
	colDim   = colour("#6E7681", "#8C959F")
	colDir   = colour("#8C9BB0", "#4E5D73")
	colRepo  = colour("#C8D3F5", "#2E3F6E")
	colHunk  = colour("#7FB4CA", "#2F6F86")
	colFresh = colour("#F0C674", "#B07D12")
	colErr   = colour("1", "1")

	styleHeader   = lipgloss.NewStyle().Bold(true)
	styleDim      = lipgloss.NewStyle().Foreground(colDim)
	styleRepo     = lipgloss.NewStyle().Bold(true).Foreground(colRepo)
	styleDir      = lipgloss.NewStyle().Foreground(colDir)
	styleAdd      = lipgloss.NewStyle().Foreground(colAdd)
	styleDel      = lipgloss.NewStyle().Foreground(colDel)
	styleHunk     = lipgloss.NewStyle().Foreground(colHunk)
	styleFresh    = lipgloss.NewStyle().Foreground(colFresh).Bold(true)
	styleErr      = lipgloss.NewStyle().Foreground(colErr)
	styleSelected = lipgloss.NewStyle().Reverse(true)
	styleUnpushed = lipgloss.NewStyle().Foreground(colMod)
	statusStyles  = map[Status]lipgloss.Style{
		Added:     styleAdd,
		Untracked: styleAdd,
		Deleted:   styleDel,
		Modified:  lipgloss.NewStyle().Foreground(colMod),
		TypeChg:   lipgloss.NewStyle().Foreground(colMod),
	}
)

func (m *model) leftWidth() int {
	return max(24, min(60, m.width*35/100))
}

func (m *model) bodyHeight() int { return max(1, m.height-2) }

func (m *model) layout() {
	m.diff.Width = max(1, m.width-m.leftWidth()-1)
	m.diff.Height = max(1, m.bodyHeight()-1)
	m.refreshDiffContent(true)
	m.scrollList()
}

func (m *model) refreshDiffContent(keepOffset bool) {
	w := max(1, m.diff.Width)
	m.segs, m.painted, m.hunkRows = m.segs[:0], m.painted[:0], m.hunkRows[:0]
	for i, l := range m.lines {
		text := expandTabs(l.text)
		if strings.HasPrefix(text, "@@") {
			m.hunkRows = append(m.hunkRows, len(m.segs))
		}
		if !m.wrap || runewidth.StringWidth(text) <= w {
			m.segs = append(m.segs, segment{line: i, text: runewidth.Truncate(text, w, ""), style: l.style, last: true})
			continue
		}
		parts := strings.Split(ansi.Wrap(text, w-1, ""), "\n")
		at := 0
		for j, part := range parts {
			// Wrapping may drop the spaces it breaks at: find where each part starts.
			if k := strings.Index(text[at:], part); k >= 0 {
				at += k
			}
			s := segment{line: i, col: runewidth.StringWidth(text[:at]), text: part, style: l.style, last: j == len(parts)-1}
			if j > 0 {
				s.indent = 1
			}
			m.segs = append(m.segs, s)
			at += len(part)
		}
	}
	for _, s := range m.segs {
		m.painted = append(m.painted, s.style.Render(strings.Repeat(" ", s.indent)+s.text))
	}
	off := m.diff.YOffset
	m.paintSelection()
	if keepOffset {
		m.diff.SetYOffset(off)
	} else {
		m.diff.GotoTop()
	}
}

func (m *model) jumpHunk(dir int) {
	off := m.diff.YOffset
	if dir > 0 {
		for _, r := range m.hunkRows {
			if r > off {
				m.diff.SetYOffset(r)
				return
			}
		}
		return
	}
	for i := len(m.hunkRows) - 1; i >= 0; i-- {
		if m.hunkRows[i] < off {
			m.diff.SetYOffset(m.hunkRows[i])
			return
		}
	}
}

// Prose is read wrapped, code is read with its indentation intact.
func wrapsByDefault(key string) bool {
	switch strings.ToLower(filepath.Ext(key)) {
	case ".md", ".markdown", ".txt", ".rst", ".adoc", "":
		return true
	}
	return false
}

func (m *model) scrollList() {
	h := m.bodyHeight()
	if m.cursor < 0 {
		m.listTop = 0
		return
	}
	if m.cursor < m.listTop {
		m.listTop = m.cursor
	}
	if m.cursor >= m.listTop+h {
		m.listTop = m.cursor - h + 1
	}
	if m.cursor == m.firstFile() && m.cursor < h {
		m.listTop = 0
	}
	m.listTop = max(0, min(m.listTop, len(m.rows)-h))
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	h := m.bodyHeight()
	left := m.renderList(m.leftWidth(), h)
	sep := styleDim.Render(strings.TrimSuffix(strings.Repeat("│\n", h), "\n"))
	right := m.renderDiffPane()
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
	return m.renderHeader() + "\n" + body + "\n" + m.renderFooter()
}

func (m *model) renderHeader() string {
	if m.inHistory() {
		return m.renderHistoryHeader()
	}
	files, add, del := len(m.snap.Changes), 0, 0
	for _, c := range m.snap.Changes {
		add += c.Add
		del += c.Del
	}
	parts := []string{styleHeader.Render(m.ctx.Name), string(m.ctx.Kind)}
	if len(m.ctx.Repos) > 1 {
		parts = append(parts, fmt.Sprintf("%d repositories", len(m.ctx.Repos)))
	}
	if m.loaded {
		parts = append(parts, fmt.Sprintf("%d files %s %s", files, styleAdd.Render(fmt.Sprintf("+%d", add)), styleDel.Render(fmt.Sprintf("−%d", del))))
		if n := m.reviewedCount(); n > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d reviewed", n, files))
		}
	}
	return padRight(" "+strings.Join(parts, styleDim.Render(" · ")), m.width)
}

func (m *model) renderHistoryHeader() string {
	parts := []string{styleHeader.Render(m.ctx.Name), "history", fmt.Sprintf("latest %d", depths[m.depth])}
	if m.loaded {
		unpushed := 0
		for _, c := range m.snap.Commits {
			if c.Unpushed {
				unpushed++
			}
		}
		count := fmt.Sprintf("%d commits", len(m.snap.Commits))
		if len(m.snap.Commits) == 1 {
			count = "1 commit"
		}
		parts = append(parts, count)
		if unpushed > 0 {
			parts = append(parts, styleUnpushed.Render(fmt.Sprintf("↑ %d not pushed", unpushed)))
		}
	}
	return padRight(" "+strings.Join(parts, styleDim.Render(" · ")), m.width)
}

func (m *model) renderFooter() string {
	help := " j/k move  h/l fold  v reviewed  space scroll  n/N hunk  w wrap  tab history  q quit"
	if m.inHistory() {
		help = " j/k move  h/l fold  m/M more/fewer  space scroll  n/N hunk  w wrap  tab changes  q quit"
	}
	if time.Now().Before(m.noticeUntil) {
		help = " " + m.notice
	}
	return styleDim.Render(padRight(runewidth.Truncate(help, m.width, ""), m.width))
}

func (m *model) renderList(w, h int) string {
	lines := make([]string, 0, h)
	switch {
	case !m.loaded:
		lines = append(lines, styleDim.Render(" reading…"))
	case len(m.rows) == 0 && m.inHistory():
		lines = append(lines, styleDim.Render(" no commits"))
	case len(m.rows) == 0:
		lines = append(lines, styleDim.Render(" no changes"))
	}
	now := time.Now()
	for i := m.listTop; i < len(m.rows) && len(lines) < h; i++ {
		lines = append(lines, m.renderRow(m.rows[i], i == m.cursor, w, now))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = padRight(l, w)
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderRow(r Row, selected bool, w int, now time.Time) string {
	indent := strings.Repeat("  ", r.Depth)
	if r.Kind == RowCommit {
		return m.renderCommit(r, selected, w, now)
	}
	if r.Foldable() {
		arrow := "▾"
		if r.Collapsed {
			arrow = "▸"
		}
		text := fmt.Sprintf("%s%s %s", indent, arrow, printable(r.Label))
		if r.Kind == RowRepo || r.Collapsed {
			text += fmt.Sprintf(" (%d)", len(r.Files))
		}
		text = padRight(runewidth.Truncate(text, w, "…"), w)
		switch {
		case selected:
			return styleSelected.Render(text)
		case m.allReviewed(r.Files):
			return styleDim.Render(text)
		case r.Kind == RowRepo:
			return styleRepo.Render(text)
		default:
			return styleDir.Render(text)
		}
	}
	switch r.Kind {
	case RowNote:
		return styleErr.Render(runewidth.Truncate(" "+indent+"! "+r.Label, w, "…"))
	}

	c := m.snap.Changes[r.Change]
	fresh := false
	if t, ok := m.changedAt[c.Key()]; ok && now.Sub(t) < freshFor && t.After(m.started) {
		fresh = true
	}
	stat := c.Stat()
	status := string(c.Status)
	nameW := max(4, w-runewidth.StringWidth(indent)-4-runewidth.StringWidth(stat)-1)
	name := runewidth.Truncate(printable(r.Label), nameW, "…")
	gap := max(1, w-runewidth.StringWidth(indent)-4-runewidth.StringWidth(name)-runewidth.StringWidth(stat))

	reviewed := m.isReviewed(c)
	mark := " "
	switch {
	case reviewed:
		mark = "√"
	case fresh:
		mark = "●"
	}
	if selected {
		plain := mark + indent + status + " " + name + strings.Repeat(" ", gap) + stat
		return styleSelected.Render(padRight(runewidth.Truncate(plain, w, ""), w))
	}
	if reviewed {
		return styleDim.Render(mark + indent + status + " " + name + strings.Repeat(" ", gap) + stat)
	}
	if fresh {
		mark = styleFresh.Render(mark)
	}
	statStyled := styleDim.Render(stat)
	if c.counted() {
		statStyled = styleAdd.Render(fmt.Sprintf("+%d", c.Add)) + " " + styleDel.Render(fmt.Sprintf("−%d", c.Del))
	}
	return mark + indent + statusStyles[c.Status].Render(status) + " " + name + strings.Repeat(" ", gap) + statStyled
}

// renderCommit lays a commit out as: unpushed mark, fold arrow, time, repository, subject.
// Its size is in the summary on the right.
func (m *model) renderCommit(r Row, selected bool, w int, now time.Time) string {
	c := m.snap.Commits[r.Commit]
	mark := " "
	if c.Unpushed {
		mark = "↑"
	}
	arrow := "▾"
	if r.Collapsed {
		arrow = "▸"
	}
	date := fmt.Sprintf("%-6s", when(c.Time, now))
	prefix := arrow + " " + date + " "
	repo := ""
	if len(m.ctx.Repos) > 1 {
		repo = runewidth.FillRight(runewidth.Truncate(shortRepoLabel(m.ctx, c.Repo), m.repoWidth(), "…"), m.repoWidth()) + " "
	}
	fixed := 1 + runewidth.StringWidth(prefix) + runewidth.StringWidth(repo)
	subject := runewidth.Truncate(printable(r.Label), max(1, w-fixed), "…")
	if selected {
		return styleSelected.Render(padRight(runewidth.Truncate(mark+prefix+repo+subject, w, ""), w))
	}
	return styleUnpushed.Render(mark) + styleDim.Render(arrow+" ") + ageStyle(c.Time, now).Render(date) + " " + styleRepo.Render(repo) + subject
}

// ageStyle fades a commit's date with its age: the last hour, today, yesterday, before.
func ageStyle(t, now time.Time) lipgloss.Style {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case now.Sub(t) < time.Hour:
		return styleFresh
	case !t.Before(today):
		return lipgloss.NewStyle()
	case !t.Before(today.AddDate(0, 0, -1)):
		return styleDir
	}
	return styleDim
}

// repoWidth is the width of the repository column of the history: its longest name, within reason.
func (m *model) repoWidth() int {
	n := 0
	for _, c := range m.snap.Commits {
		n = max(n, runewidth.StringWidth(shortRepoLabel(m.ctx, c.Repo)))
	}
	return min(n, 12)
}

func shortRepoLabel(ctx *Context, r *Repo) string { return path.Base(repoLabel(ctx, r)) }

func (m *model) renderDiffPane() string {
	w := m.diff.Width
	title := ""
	if c, ok := m.selected(); ok {
		name := printable(c.Display())
		if c.Commit != "" {
			name = c.Commit[:min(len(c.Commit), 7)] + " " + name
		}
		title = styleHeader.Render(runewidth.Truncate(" "+name, w, "…"))
	} else if m.cursor >= 0 && m.cursor < len(m.rows) {
		title = styleHeader.Render(runewidth.Truncate(" "+printable(m.rows[m.cursor].Label), w, "…"))
	}
	return padRight(title, w) + "\n" + m.diff.View()
}

type diffLine struct {
	text  string
	style lipgloss.Style
}

// parseDiff styles a diff line by line and drops the git headers the title already says.
func parseDiff(text string) []diffLine {
	raw := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range raw {
		// Tabs are kept, to be copied as they are, and expanded on screen.
		parts := strings.Split(strings.TrimSuffix(l, "\r"), "\t")
		for j, p := range parts {
			parts[j] = printable(p)
		}
		raw[i] = strings.Join(parts, "\t")
	}
	start := 0
	for i, l := range raw {
		if strings.HasPrefix(l, "@@") || strings.HasPrefix(l, "Binary files") {
			start = i
			break
		}
	}
	const maxLines = 20000
	raw = raw[start:]
	truncated := len(raw) > maxLines
	if truncated {
		raw = raw[:maxLines]
	}
	plain := lipgloss.NewStyle()
	out := make([]diffLine, 0, len(raw)+1)
	for _, l := range raw {
		st := plain
		switch {
		case strings.HasPrefix(l, "@@"):
			st = styleHunk
		case strings.HasPrefix(l, "+"):
			st = styleAdd
		case strings.HasPrefix(l, "-"):
			st = styleDel
		case strings.HasPrefix(l, `\`), strings.HasPrefix(l, "Binary files"):
			st = styleDim
		}
		out = append(out, diffLine{l, st})
	}
	if truncated {
		out = append(out, diffLine{fmt.Sprintf("… diff truncated at %d lines", maxLines), styleDim})
	}
	return out
}

// printable shows control characters in caret notation, like cat -v, so that what a file
// holds is displayed and never interpreted by the terminal.
func printable(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20:
			b.WriteByte('^')
			b.WriteRune(r + '@')
		case r == 0x7f:
			b.WriteString("^?")
		case r >= 0x80 && r < 0xa0:
			b.WriteRune('\uFFFD')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isControl(r rune) bool { return r < 0x20 || r >= 0x7f && r < 0xa0 }

func padRight(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}
