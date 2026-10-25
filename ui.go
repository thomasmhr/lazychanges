package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg struct{ gen int }

type snapMsg struct {
	snap  Snapshot
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

	snap     Snapshot
	rows     []Row
	cursor   int
	selKey   string
	listTop  int
	loading  bool
	loaded   bool
	tickGen  int
	collects int

	// rediscoverEvery is how many collections pass between two discoveries of the repositories.
	rediscoverEvery int

	sigs      map[string]string
	changedAt map[string]time.Time
	collapsed map[string]bool
	reviewed  map[string]review

	diff     viewport.Model
	diffKey  string
	diffSig  string
	lines    []diffLine
	wrap     bool
	hunkRows []int

	width, height int
}

func newModel(ctx *Context, interval time.Duration) *model {
	return &model{
		ctx:             ctx,
		interval:        interval,
		started:         time.Now(),
		cursor:          -1,
		rediscoverEvery: 15,
		sigs:            map[string]string{},
		changedAt:       map[string]time.Time{},
		collapsed:       map[string]bool{},
		reviewed:        map[string]review{},
		diff:            viewport.New(0, 0),
	}
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
	return func() tea.Msg {
		start := time.Now()
		msg := snapMsg{}
		if rediscover {
			if found, kind, err := Rediscover(root, repos); err == nil {
				repos, msg.repos, msg.kind = found, found, kind
			}
		}
		msg.snap = Collect(repos)
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
		return m, tea.Batch(m.syncDiff(), m.tick(msg.took))

	case diffMsg:
		if msg.key != m.selKey {
			return m, nil
		}
		sameFile := msg.key == m.diffKey
		m.diffKey, m.diffSig = msg.key, msg.sig
		if msg.err != nil {
			m.lines = []diffLine{{msg.err.Error(), styleErr}}
		} else {
			m.lines = parseDiff(msg.text)
		}
		if !sameFile {
			m.wrap = wrapsByDefault(msg.key)
		}
		m.refreshDiffContent(sameFile)
		return m, nil

	case tea.MouseMsg:
		return m, m.mouse(msg)

	case tea.KeyMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *model) key(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit
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
	case "v":
		if m.toggleReviewed() {
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
	m.snap = snap
	m.loaded = true
	m.recheckReviews()
	m.rebuild()
}

// rebuild lays the rows out again and puts the cursor back on the same row; if that row
// is gone, on the row now at its place, and on the first file at the start.
func (m *model) rebuild() {
	m.rows = BuildTree(m.ctx, m.snap, m.collapsed)
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
		m.selKey, m.diffKey, m.lines = "", "", nil
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
		m.lines = m.summary(r)
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
	lines := []diffLine{{count, styleDim}, {"", plain}}
	for _, i := range r.Files {
		c := m.snap.Changes[i]
		lines = append(lines, diffLine{fmt.Sprintf("%c %s  %s", c.Status, printable(c.Display()), c.Stat()), statusStyles[c.Status]})
	}
	return lines
}
