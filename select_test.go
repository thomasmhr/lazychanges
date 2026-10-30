package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// selectFixture shows the diff of a.go: "@@ -1,3 +1,3 @@" on the first row of the pane,
// then " func f() {", "-\treturn 1", "+\treturn 2", " }".
func selectFixture(t *testing.T) (*model, *[]string) {
	dir := repo(t, t.TempDir(), map[string]string{"a.go": "func f() {\n\treturn 1\n}\n"})
	write(t, filepath.Join(dir, "a.go"), "func f() {\n\treturn 2\n}\n")
	m := newTestModel(t, dir)
	var copied []string
	prev := copyToClipboard
	copyToClipboard = func(s string) error { copied = append(copied, s); return nil }
	t.Cleanup(func() { copyToClipboard = prev })
	return m, &copied
}

// mouse sends an event at a cell of the diff pane, row 0 being its first row of text.
func mouse(m *model, action tea.MouseAction, row, col int) {
	msg := tea.MouseMsg{X: m.leftWidth() + 1 + col, Y: 2 + row, Action: action}
	if action == tea.MouseActionPress {
		msg.Button = tea.MouseButtonLeft
	}
	if _, cmd := m.Update(msg); cmd != nil {
		if c, ok := cmd().(copiedMsg); ok {
			m.Update(c)
		}
	}
}

func TestDragCopiesLinesWithoutTheirDiffPrefix(t *testing.T) {
	m, copied := selectFixture(t)
	mouse(m, tea.MouseActionPress, 1, 0)
	mouse(m, tea.MouseActionMotion, 3, 40)
	mouse(m, tea.MouseActionRelease, 3, 40)
	want := "func f() {\n\treturn 1\n\treturn 2"
	if len(*copied) != 1 || (*copied)[0] != want {
		t.Fatalf("copied %q, want %q", *copied, want)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "copied 3 lines") {
		t.Errorf("footer should say what was copied:\n%s", v)
	}
}

func TestDragWithinALineCopiesTheCellsUnderIt(t *testing.T) {
	m, copied := selectFixture(t)
	// "+    return 2": the r of return is at cell 5.
	mouse(m, tea.MouseActionPress, 3, 5)
	mouse(m, tea.MouseActionMotion, 3, 10)
	mouse(m, tea.MouseActionRelease, 3, 10)
	if len(*copied) != 1 || (*copied)[0] != "return" {
		t.Fatalf("copied %q", *copied)
	}
}

func TestClickWithoutDragCopiesNothing(t *testing.T) {
	m, copied := selectFixture(t)
	mouse(m, tea.MouseActionPress, 1, 2)
	mouse(m, tea.MouseActionRelease, 1, 2)
	if len(*copied) != 0 {
		t.Fatalf("copied %q", *copied)
	}
}

func TestDoubleClickCopiesAWordAndTripleClickALine(t *testing.T) {
	m, copied := selectFixture(t)
	mouse(m, tea.MouseActionPress, 3, 7)
	mouse(m, tea.MouseActionRelease, 3, 7)
	mouse(m, tea.MouseActionPress, 3, 7)
	mouse(m, tea.MouseActionRelease, 3, 7)
	mouse(m, tea.MouseActionPress, 3, 7)
	mouse(m, tea.MouseActionRelease, 3, 7)
	want := []string{"return", "\treturn 2"}
	if strings.Join(*copied, "|") != strings.Join(want, "|") {
		t.Fatalf("copied %q, want %q", *copied, want)
	}
}

func TestSelectionIsClearedWhenTheDiffChanges(t *testing.T) {
	m, _ := selectFixture(t)
	mouse(m, tea.MouseActionPress, 1, 0)
	mouse(m, tea.MouseActionMotion, 1, 4)
	if m.selectedText() != "func" {
		t.Fatalf("selected %q", m.selectedText())
	}
	m.refresh()
	if m.sel.empty() {
		t.Fatal("a refresh that changes nothing should keep the selection")
	}
	write(t, filepath.Join(m.ctx.Root, "a.go"), "func f() {\n\treturn 3\n}\n")
	m.refresh()
	if !m.sel.empty() {
		t.Fatal("a new diff should clear the selection")
	}
}

func TestCutCellsCountsTabsAsShown(t *testing.T) {
	for _, c := range []struct {
		s    string
		a, b int
		want string
	}{
		{"+\treturn 2", 1, endOfLine, "\treturn 2"},
		{"+\treturn 2", 5, 11, "return"},
		{"+\treturn 2", 3, 6, "\tr"},
		{"héllo", 1, 3, "él"},
	} {
		if got := cutCells(c.s, c.a, c.b); got != c.want {
			t.Errorf("cutCells(%q, %d, %d) = %q, want %q", c.s, c.a, c.b, got, c.want)
		}
	}
}

func TestSelectionFollowsWrappedLines(t *testing.T) {
	words := strings.Repeat("lorem ipsum ", 12)
	dir := repo(t, t.TempDir(), map[string]string{"a.md": "x\n"})
	write(t, filepath.Join(dir, "a.md"), words+"\n")
	m := newTestModel(t, dir)
	copied := ""
	prev := copyToClipboard
	copyToClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { copyToClipboard = prev })
	// Row 2 is the "+" line, row 3 its continuation; dragging to past the end of the
	// continuation reaches the end of the line.
	mouse(m, tea.MouseActionPress, 2, 1)
	mouse(m, tea.MouseActionMotion, 3, 200)
	mouse(m, tea.MouseActionRelease, 3, 200)
	if copied != words {
		t.Fatalf("copied %q, want %q", copied, words)
	}
}
