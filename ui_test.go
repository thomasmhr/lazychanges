package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSelectionFollowsTheFileWhileOthersAppear(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"m.txt": "1\n", "z.txt": "1\n"})
	write(t, filepath.Join(dir, "z.txt"), "2\n")
	m := newTestModel(t, dir)
	if m.selectedPath() != "z.txt" {
		t.Fatalf("selected %q", m.selectedPath())
	}
	write(t, filepath.Join(dir, "a.txt"), "new\n")
	write(t, filepath.Join(dir, "m.txt"), "2\n")
	m.refresh()
	if m.selectedPath() != "z.txt" {
		t.Fatalf("selection moved to %q", m.selectedPath())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if m.selectedPath() != "a.txt" {
		t.Fatalf("g selected %q", m.selectedPath())
	}
}

func TestSelectionMovesOnWhenTheFileIsReverted(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n", "b.txt": "1\n"})
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	write(t, filepath.Join(dir, "b.txt"), "2\n")
	m := newTestModel(t, dir)
	write(t, filepath.Join(dir, "a.txt"), "1\n")
	m.refresh()
	if m.selectedPath() != "b.txt" {
		t.Fatalf("selected %q", m.selectedPath())
	}
}

func TestDiffFollowsTheSelectedFileAsItChanges(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n"})
	write(t, filepath.Join(dir, "a.txt"), "first edit\n")
	m := newTestModel(t, dir)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "+first edit") {
		t.Fatalf("view:\n%s", v)
	}
	write(t, filepath.Join(dir, "a.txt"), "second edit\n")
	os.Chtimes(filepath.Join(dir, "a.txt"), time.Now().Add(time.Second), time.Now().Add(time.Second))
	m.refresh()
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "+second edit") || strings.Contains(v, "first edit") {
		t.Fatalf("view:\n%s", v)
	}
	if !strings.Contains(v, "●") {
		t.Errorf("a file changed during the session should be marked:\n%s", v)
	}
}

func TestEmptyView(t *testing.T) {
	m := newTestModel(t, repo(t, t.TempDir(), map[string]string{"a": "a\n"}))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no changes") {
		t.Fatalf("view:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
}

func TestRepositoriesAndDirectoriesFold(t *testing.T) {
	m, name := foldFixture(t)
	assertList(t, m.visible(), name, "  top.md", "apps/front", "  app/", "    a.rb", "    b.rb")
	if m.selectedPath() != "top.md" {
		t.Fatalf("selected %q", m.selectedPath())
	}

	press(m, "j", "enter")
	assertList(t, m.visible(), name, "  top.md", "apps/front")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▸ apps/front (2)") || !strings.Contains(v, "M apps/front/app/a.rb") {
		t.Fatalf("a folded repository shows its count and its files on the right:\n%s", v)
	}

	press(m, "right", "right", "left")
	assertList(t, m.visible(), name, "  top.md", "apps/front", "  app/")
	press(m, "left")
	if m.rows[m.cursor].Label != "apps/front" {
		t.Fatalf("left on a folded directory climbs to %q", m.rows[m.cursor].Label)
	}
}

func TestFoldingSurvivesRefreshes(t *testing.T) {
	m, name := foldFixture(t)
	press(m, "-")
	assertList(t, m.visible(), name, "apps/front")
	write(t, filepath.Join(m.ctx.Repos[1].Dir, "app/c.rb"), "c\n")
	m.refresh()
	assertList(t, m.visible(), name, "apps/front")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "apps/front (3)") {
		t.Fatalf("view:\n%s", v)
	}
	press(m, "+")
	if len(m.visible()) != 7 {
		t.Fatalf("unfolded: %v", m.visible())
	}
}

func TestViewBeforeTheFirstCollection(t *testing.T) {
	m := newModel(detect(t, repo(t, t.TempDir(), nil)), time.Second)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "reading…") {
		t.Fatalf("view:\n%s", v)
	}
	press(m, "j", "enter", "left", "right", "-", "+", "n", "w")
}

func TestFileContentNeverReachesTheTerminalRaw(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"win.txt": "a\r\nb\r\n", "esc.txt": "x\n"})
	write(t, filepath.Join(dir, "win.txt"), "a\r\nc\r\n")
	write(t, filepath.Join(dir, "esc.txt"), "x\x1b]0;title\x07\x1b[2J\x9b\n")
	m := newTestModel(t, dir)
	for _, path := range []string{"esc.txt", "win.txt"} {
		if m.selectedPath() != path {
			t.Fatalf("selected %q, want %q", m.selectedPath(), path)
		}
		// The raw frame: the styling uses CSI colour sequences, never an OSC, a bell or a CR.
		v := m.View()
		if strings.Contains(v, "\x1b]") || strings.Contains(v, "\x1b[2J") || strings.ContainsAny(v, "\r\x07\u009b") {
			t.Fatalf("control characters reached the terminal for %s: %q", path, v)
		}
		press(m, "j")
	}
	press(m, "k")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "+x^[]0;title^G^[[2J") {
		t.Fatalf("control characters should show as pictures:\n%s", v)
	}
}

func TestRepositoriesClonedDuringTheSessionAppear(t *testing.T) {
	root := repo(t, t.TempDir(), map[string]string{".gitignore": "/apps/*/\n"})
	m := newModel(detect(t, root), time.Second)
	m.rediscoverEvery = 1
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m.refresh()
	if m.ctx.Kind != KindRepo {
		t.Fatalf("kind %s", m.ctx.Kind)
	}
	app := repo(t, filepath.Join(root, "apps", "new"), map[string]string{"a.rb": "a\n"})
	write(t, filepath.Join(app, "a.rb"), "b\n")
	m.refresh()
	if m.ctx.Kind != KindMeta || m.selectedPath() != "apps/new/a.rb" {
		t.Fatalf("kind %s, selected %q", m.ctx.Kind, m.selectedPath())
	}
}

func TestAnyTerminalSizeRenders(t *testing.T) {
	m, _ := foldFixture(t)
	for w := 1; w <= 60; w += 7 {
		for h := 1; h <= 12; h += 3 {
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			press(m, "j", "w", "n", " ", "h", "l", "v")
			m.View()
		}
	}
}

func TestTheViewEmptiesOnceEverythingIsCommitted(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n"})
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	write(t, filepath.Join(dir, "new.txt"), "n\n")
	m := newTestModel(t, dir)
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "agent")
	m.refresh()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no changes") || strings.Contains(v, "a.txt") {
		t.Fatalf("view:\n%s", v)
	}
}
