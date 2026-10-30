package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg struct{ gen int }

type snapMsg struct {
	snap  Snapshot
	hist  *Snapshot // collected only while the history is shown
	took  time.Duration
	repos []*Repo // set when the repositories were discovered again
	kind  Kind
}

type diffMsg struct {
	key, sig string
	text     string
	err      error
}

type model struct {
	ctx      *Context
	interval time.Duration
	started  time.Time

	// view is the list on screen, changes or history; each keeps its own rows and selection.
	*view
	changes, history *view
	depth            int // index in depths
	seen             map[string]bool

	loading  bool
	tickGen  int
	collects int

	// rediscoverEvery is how many collections pass between two discoveries of the repositories.
	rediscoverEvery int

	sigs      map[string]string
	changedAt map[string]time.Time
	reviewed  map[string]review

	diff     viewport.Model
	diffKey  string
	diffSig  string
	lines    []diffLine
	isDiff   bool // lines are those of a diff, not a summary
	wrap     bool
	hunkRows []int
	segs     []segment // the rows of the diff pane
	painted  []string  // the rows rendered without the selection
	sel      selection

	notice      string
	noticeUntil time.Time

	width, height int
}

type view struct {
	snap      Snapshot
	rows      []Row
	cursor    int
	selKey    string
	listTop   int
	loaded    bool
	collapsed map[string]bool
}

func newView() *view { return &view{cursor: -1, collapsed: map[string]bool{}} }

func newModel(ctx *Context, interval time.Duration) *model {
	m := &model{
		ctx:             ctx,
		interval:        interval,
		started:         time.Now(),
		changes:         newView(),
		history:         newView(),
		seen:            map[string]bool{},
		rediscoverEvery: 15,
		sigs:            map[string]string{},
		changedAt:       map[string]time.Time{},
		reviewed:        map[string]review{},
		diff:            viewport.New(0, 0),
	}
	m.view = m.changes
	return m
}

func (m *model) inHistory() bool { return m.view == m.history }

// on runs f with v as the current view, so a list off screen can be updated.
func (m *model) on(v *view, f func()) {
	prev := m.view
	m.view = v
	f()
	m.view = prev
}

func (m *model) Init() tea.Cmd {
	m.loading = true
	return m.collect()
}

// tick schedules the next collection once the previous one is done, never sooner than
// twice its duration, so a large folder of repositories is not scanned back to back.
// Only the latest scheduled tick counts.
func (m *model) tick(took time.Duration) tea.Cmd {
	m.tickGen++
	gen := m.tickGen
	return tea.Tick(max(m.interval, 2*took), func(time.Time) tea.Msg { return tickMsg{gen} })
}

func (m *model) collect() tea.Cmd {
	m.collects++
	root, repos := m.ctx.Root, m.ctx.Repos
	rediscover := m.collects%m.rediscoverEvery == 0
	withHistory, depth := m.inHistory(), depths[m.depth]
	return func() tea.Msg {
		start := time.Now()
		msg := snapMsg{}
		if rediscover {
			if fresh, err := Detect(root); err == nil {
				repos, msg.repos, msg.kind = fresh.Repos, fresh.Repos, fresh.Kind
			}
		}
		msg.snap = Collect(repos)
		if withHistory {
			h := CollectHistory(repos, depth)
			msg.hist = &h
		}
		msg.took = time.Since(start)
		return msg
	}
}

func (m *model) loadDiff(c Change) tea.Cmd {
	key, sig := c.Key(), c.signature()
	return func() tea.Msg {
		text, err := Diff(c)
		return diffMsg{key: key, sig: sig, text: text, err: err}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tickMsg:
		if msg.gen != m.tickGen || m.loading {
			return m, nil
		}
		m.loading = true
		return m, m.collect()

	case snapMsg:
		m.loading = false
		if msg.repos != nil {
			m.ctx.Repos, m.ctx.Kind = msg.repos, msg.kind
		}
		m.applySnapshot(msg.snap)
		if msg.hist != nil {
			m.applyHistory(*msg.hist)
		}
		return m, tea.Batch(m.syncDiff(), m.tick(msg.took))

	case diffMsg:
		if msg.key != m.selKey {
			return m, nil
		}
		sameFile := msg.key == m.diffKey
		m.diffKey, m.diffSig = msg.key, msg.sig
		if msg.err != nil {
			m.setLines([]diffLine{{msg.err.Error(), styleErr}}, false)
		} else {
			m.setLines(parseDiff(msg.text), true)
		}
		if !sameFile {
			m.wrap = wrapsByDefault(msg.key)
		}
		m.refreshDiffContent(sameFile)
		return m, nil

	case tea.MouseMsg:
		return m, m.mouse(msg)

	case dragScrollMsg:
		return m, m.dragScrolled()

	case copiedMsg:
		return m, m.copied(msg)

	case tea.KeyMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *model) key(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "esc":
		m.clearSelection()
	case "j", "down":
		return m.move(1)
	case "k", "up":
		return m.move(-1)
	case "g", "home":
		return m.move(-len(m.rows))
	case "G", "end":
		return m.move(len(m.rows))
	case "enter":
		return m.toggle()
	case "h", "left":
		return m.left()
	case "l", "right":
		return m.right()
	case "-":
		return m.foldAll(true)
	case "+", "=":
		return m.foldAll(false)
	case "ctrl+d", "pgdown", " ":
		m.diff.HalfPageDown()
	case "ctrl+u", "pgup", "b":
		m.diff.HalfPageUp()
	case "n":
		m.jumpHunk(1)
	case "N":
		m.jumpHunk(-1)
	case "tab":
		return m.switchView()
	case "m":
		return m.widen(1)
	case "M":
		return m.widen(-1)
	case "v":
		if !m.inHistory() && m.toggleReviewed() {
			return m.nextUnreviewed()
		}
	case "w":
		m.wrap = !m.wrap
		m.refreshDiffContent(true)
	}
	return nil
}

func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	left := m.leftWidth()
	if m.sel.dragging || msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.X > left {
		return m.diffMouse(msg)
	}
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		if msg.X < left {
			return m.move(1)
		}
		m.diff.ScrollDown(3)
	case tea.MouseButtonWheelUp:
		if msg.X < left {
			return m.move(-1)
		}
		m.diff.ScrollUp(3)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress || msg.X >= left {
			return nil
		}
		i := m.listTop + msg.Y - 1
		if i < 0 || i >= len(m.rows) || m.rows[i].Kind == RowNote {
			return nil
		}
		m.cursor = i
		if m.rows[i].Foldable() {
			return m.toggle()
		}
		return m.selectCursor()
	}
	return nil
}

func (m *model) toggle() tea.Cmd {
	if m.cursor < 0 || !m.rows[m.cursor].Foldable() {
		return nil
	}
	return m.setFolded(m.rows[m.cursor], !m.rows[m.cursor].Collapsed)
}

func (m *model) setFolded(r Row, folded bool) tea.Cmd {
	if folded {
		m.collapsed[r.Key] = true
	} else {
		delete(m.collapsed, r.Key)
	}
	m.selKey = r.Key
	m.rebuild()
	return m.syncDiff()
}

// left folds the branch under the cursor, or climbs to its parent.
func (m *model) left() tea.Cmd {
	if m.cursor < 0 {
		return nil
	}
	r := m.rows[m.cursor]
	if r.Foldable() && !r.Collapsed {
		return m.setFolded(r, true)
	}
	if r.Parent >= 0 {
		m.cursor = r.Parent
		return m.selectCursor()
	}
	return nil
}

// right unfolds the branch under the cursor, or enters it.
func (m *model) right() tea.Cmd {
	if m.cursor < 0 || !m.rows[m.cursor].Foldable() {
		return nil
	}
	if r := m.rows[m.cursor]; r.Collapsed {
		return m.setFolded(r, false)
	}
	return m.move(1)
}

// foldAll folds or unfolds every repository, or every top-level directory in a single repository.
func (m *model) foldAll(folded bool) tea.Cmd {
	if !folded {
		m.collapsed = map[string]bool{}
	} else {
		for _, r := range m.rows {
			if r.Foldable() && r.Parent == -1 {
				m.collapsed[r.Key] = true
			}
		}
		if m.cursor >= 0 {
			top := m.cursor
			for m.rows[top].Parent >= 0 {
				top = m.rows[top].Parent
			}
			m.selKey = m.rows[top].Key
		}
	}
	m.rebuild()
	return m.syncDiff()
}

func (m *model) applySnapshot(snap Snapshot) {
	now := time.Now()
	sigs := make(map[string]string, len(snap.Changes))
	for _, c := range snap.Changes {
		k, sig := c.Key(), c.signature()
		sigs[k] = sig
		if old, ok := m.sigs[k]; !ok || old != sig {
			if m.loaded {
				m.changedAt[k] = now
			} else {
				m.changedAt[k] = c.Mtime
			}
		}
	}
	m.sigs = sigs
	m.on(m.changes, func() {
		m.snap = snap
		m.loaded = true
		m.recheckReviews()
		m.rebuild()
	})
}

// applyHistory shows commits folded the first time they appear.
func (m *model) applyHistory(snap Snapshot) {
	m.on(m.history, func() {
		for _, c := range snap.Commits {
			if k := c.Key(); !m.seen[k] {
				m.seen[k] = true
				m.collapsed[k] = true
			}
		}
		m.snap = snap
		m.loaded = true
		m.rebuild()
	})
}

// switchView goes from the changes to the history and back; the history is read at once.
func (m *model) switchView() tea.Cmd {
	if m.inHistory() {
		m.view = m.changes
	} else {
		m.view = m.history
	}
	m.scrollList()
	cmd := m.syncDiff()
	if m.inHistory() && !m.loading {
		m.loading = true
		return tea.Batch(cmd, m.collect())
	}
	return cmd
}

// widen shows more of the history, or less.
func (m *model) widen(step int) tea.Cmd {
	if !m.inHistory() {
		return nil
	}
	d := max(0, min(len(depths)-1, m.depth+step))
	if d == m.depth {
		return nil
	}
	m.depth = d
	if m.loading {
		return nil
	}
	m.loading = true
	return m.collect()
}

// rebuild lays the rows out again and puts the cursor back on the same row; if that row
// is gone, on the row now at its place, and on the first file at the start.
func (m *model) rebuild() {
	if m.inHistory() {
		m.rows = BuildHistory(m.ctx, m.snap, m.collapsed)
	} else {
		m.rows = BuildTree(m.ctx, m.snap, m.collapsed)
	}
	old := m.cursor
	m.cursor = -1
	for i, r := range m.rows {
		if r.Kind != RowNote && r.Key == m.selKey {
			m.cursor = i
			break
		}
	}
	if m.cursor < 0 {
		if m.selKey == "" {
			m.cursor = m.firstFile()
		} else {
			m.cursor = m.nearestSelectable(old)
		}
	}
	m.scrollList()
}

func (m *model) firstFile() int {
	for i, r := range m.rows {
		if r.Kind == RowFile {
			return i
		}
	}
	return m.nearestSelectable(0)
}

// nearestSelectable returns the first selectable row at or after i, else the last one before it.
func (m *model) nearestSelectable(i int) int {
	for j := max(i, 0); j < len(m.rows); j++ {
		if m.rows[j].Kind != RowNote {
			return j
		}
	}
	for j := min(i, len(m.rows)-1); j >= 0; j-- {
		if m.rows[j].Kind != RowNote {
			return j
		}
	}
	return -1
}

func (m *model) move(delta int) tea.Cmd {
	if m.cursor < 0 {
		return nil
	}
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	i := m.cursor
	for n := 0; n < delta; n++ {
		j := i + step
		for j >= 0 && j < len(m.rows) && m.rows[j].Kind == RowNote {
			j += step
		}
		if j < 0 || j >= len(m.rows) {
			break
		}
		i = j
	}
	if i == m.cursor {
		return nil
	}
	m.cursor = i
	return m.selectCursor()
}

func (m *model) selectCursor() tea.Cmd {
	m.scrollList()
	return m.syncDiff()
}

func (m *model) selected() (Change, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].Kind != RowFile {
		return Change{}, false
	}
	return m.snap.Changes[m.rows[m.cursor].Change], true
}

// syncDiff reloads the right pane when the selection moved or the selected file changed.
// A repository or directory shows the files below it.
func (m *model) syncDiff() tea.Cmd {
	if m.cursor < 0 {
		m.selKey, m.diffKey = "", ""
		m.setLines(nil, false)
		m.refreshDiffContent(false)
		return nil
	}
	r := m.rows[m.cursor]
	m.selKey = r.Key
	if r.Foldable() {
		same := m.diffKey == r.Key
		if !same {
			m.wrap = false
		}
		m.diffKey, m.diffSig = r.Key, ""
		if r.Kind == RowCommit {
			m.setLines(m.commitSummary(r), false)
		} else {
			m.setLines(m.summary(r), false)
		}
		m.refreshDiffContent(same)
		return nil
	}
	c, _ := m.selected()
	if c.Key() == m.diffKey && c.signature() == m.diffSig {
		return nil
	}
	return m.loadDiff(c)
}

func (m *model) summary(r Row) []diffLine {
	plain := lipgloss.NewStyle()
	count := fmt.Sprintf("%d changed files", len(r.Files))
	if len(r.Files) == 1 {
		count = "1 changed file"
	}
	return append([]diffLine{{count, styleDim}, {"", plain}}, m.fileLines(r.Files)...)
}

func (m *model) fileLines(files []int) []diffLine {
	var lines []diffLine
	for _, i := range files {
		c := m.snap.Changes[i]
		lines = append(lines, diffLine{fmt.Sprintf("%c %s  %s", c.Status, printable(c.Display()), c.Stat()), statusStyles[c.Status]})
	}
	return lines
}

func (m *model) commitSummary(r Row) []diffLine {
	plain := lipgloss.NewStyle()
	c := m.snap.Commits[r.Commit]
	head := []string{c.Short(), repoLabel(m.ctx, c.Repo), c.Author, c.Time.Format("Mon Jan 2 15:04")}
	lines := []diffLine{{strings.Join(head, " · "), styleHunk}}
	if c.Unpushed {
		lines = append(lines, diffLine{"↑ not pushed", styleUnpushed})
	}
	add, del := 0, 0
	for _, i := range r.Files {
		add += m.snap.Changes[i].Add
		del += m.snap.Changes[i].Del
	}
	count := fmt.Sprintf("%d changed files", len(r.Files))
	if len(r.Files) == 1 {
		count = "1 changed file"
	}
	lines = append(lines, diffLine{fmt.Sprintf("%s  +%d −%d", count, add, del), styleDim})
	lines = append(lines, diffLine{"", plain})
	if _, body, ok := strings.Cut(c.Message, "\n"); ok {
		for _, l := range strings.Split(strings.TrimSpace(body), "\n") {
			lines = append(lines, diffLine{printable(l), plain})
		}
		lines = append(lines, diffLine{"", plain})
	}
	return append(lines, m.fileLines(r.Files)...)
}
