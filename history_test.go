package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// commitAt commits every change of dir as if at the given time.
func commitAt(t *testing.T, dir, msg string, at time.Time) {
	t.Helper()
	run(t, dir, "git", "add", "-A")
	cmd := exec.Command("git", "commit", "-qm", msg, "--allow-empty")
	cmd.Dir = dir
	date := at.Format(time.RFC3339)
	cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
}

// withRemote gives dir a bare remote and pushes what it holds.
func withRemote(t *testing.T, dir string) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	run(t, dir, "git", "init", "-q", "--bare", remote)
	run(t, dir, "git", "remote", "add", "origin", remote)
	run(t, dir, "git", "push", "-q", "origin", "main")
}

func (m *model) historyList() []string {
	var out []string
	for _, r := range m.rows {
		switch r.Kind {
		case RowCommit:
			c := m.snap.Commits[r.Commit]
			mark := " "
			if c.Unpushed {
				mark = "↑"
			}
			out = append(out, mark+" "+repoLabel(m.ctx, c.Repo)+" "+r.Label)
		default:
			out = append(out, strings.Repeat("  ", r.Depth)+r.Label)
		}
	}
	return out
}

func TestHistoryMergesTheRepositoriesNewestFirst(t *testing.T) {
	root := repo(t, t.TempDir(), map[string]string{".gitignore": "/apps/*/\n"})
	front := repo(t, filepath.Join(root, "apps", "front"), map[string]string{"a.rb": "a\n"})
	now := time.Now()
	write(t, filepath.Join(root, "doc.md"), "d\n")
	commitAt(t, root, "Document the system.", now.Add(time.Minute))
	write(t, filepath.Join(front, "a.rb"), "b\n")
	commitAt(t, front, "Fix the front.", now.Add(2*time.Minute))
	write(t, filepath.Join(root, "doc.md"), "e\n")
	commitAt(t, root, "Correct the documentation.", now.Add(3*time.Minute))

	m := newTestModel(t, root)
	press(m, "tab")
	m.refresh()
	name := filepath.Base(root)
	assertList(t, m.historyList()[:3],
		"  "+name+" Correct the documentation.",
		"  apps/front Fix the front.",
		"  "+name+" Document the system.")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "history") || !strings.Contains(v, "5 commits") || strings.Contains(v, "not pushed") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestTheLatestCommitsAcrossRepositoriesAndEveryUnpushedOne(t *testing.T) {
	root := repo(t, t.TempDir(), map[string]string{".gitignore": "/apps/*/\n"})
	withRemote(t, root)
	write(t, filepath.Join(root, "old.md"), "o\n")
	commitAt(t, root, "Old, not pushed.", time.Now().Add(-30*24*time.Hour))
	busy := repo(t, filepath.Join(root, "apps", "busy"), nil)
	script := `base=$(( $(date +%s) + 60 ))
for i in $(seq 1 110); do
  GIT_AUTHOR_DATE="$((base + i)) +0000" GIT_COMMITTER_DATE="$((base + i)) +0000" git commit -q --allow-empty -m "Busy $i."
done`
	run(t, busy, "bash", "-c", script)

	m := newTestModel(t, root)
	press(m, "tab")
	m.refresh()
	got := m.historyList()
	if len(got) != 101 || got[0] != "  apps/busy Busy 110." || got[99] != "  apps/busy Busy 11." || got[100] != "↑ "+filepath.Base(root)+" Old, not pushed." {
		t.Fatalf("got %d commits:\n  %s", len(got), strings.Join(got, "\n  "))
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "latest 100") || !strings.Contains(v, "101 commits") || !strings.Contains(v, "↑ 1 not pushed") {
		t.Fatalf("view:\n%s", v)
	}

	press(m, "m")
	m.refresh()
	if got := m.historyList(); len(got) != 113 {
		t.Fatalf("with more, got %d commits", len(got))
	}

	run(t, root, "git", "push", "-q", "origin", "main")
	press(m, "M")
	m.refresh()
	if got := m.historyList(); len(got) != 100 || strings.Contains(strings.Join(got, "\n"), "Old") {
		t.Fatalf("once pushed, an old commit leaves the history:\n  %s", strings.Join(got, "\n  "))
	}
}

func TestACommitShowsItsFilesAndTheirDiffAtThatCommit(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"app/a.txt": "1\n", "b.txt": "b\n"})
	write(t, filepath.Join(dir, "app/a.txt"), "2\n")
	commitAt(t, dir, "Change a.", time.Now())
	write(t, filepath.Join(dir, "app/a.txt"), "3\n")

	m := newTestModel(t, dir)
	press(m, "tab")
	m.refresh()
	assertList(t, m.historyList(), "  "+filepath.Base(dir)+" Change a.", "  "+filepath.Base(dir)+" init")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Change a.") || !strings.Contains(v, "1 changed file  +1 −1") || !strings.Contains(v, "M app/a.txt  +1 −1") {
		t.Fatalf("a commit shows its message and files:\n%s", v)
	}

	press(m, "l", "j", "j")
	assertList(t, m.historyList()[:3], "  "+filepath.Base(dir)+" Change a.", "  app/", "    a.txt")
	if m.selectedPath() != "app/a.txt" {
		t.Fatalf("selected %q", m.selectedPath())
	}
	v = ansi.Strip(m.View())
	if !strings.Contains(v, "-1") || !strings.Contains(v, "+2") || strings.Contains(v, "+3") {
		t.Fatalf("the diff is the commit's, not the work tree's:\n%s", v)
	}
}

func TestWorkCommittedDuringTheSessionMovesToTheHistory(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n"})
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	m := newTestModel(t, dir)
	if m.selectedPath() != "a.txt" {
		t.Fatalf("selected %q", m.selectedPath())
	}
	run(t, dir, "git", "commit", "-qam", "The agent's work.")
	press(m, "tab")
	m.refresh()
	assertList(t, m.historyList(), "  "+filepath.Base(dir)+" The agent's work.", "  "+filepath.Base(dir)+" init")

	press(m, "tab")
	m.refresh()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no changes") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestEachViewKeepsItsSelection(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n", "b.txt": "1\n"})
	write(t, filepath.Join(dir, "b.txt"), "2\n")
	commitAt(t, dir, "Second.", time.Now())
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	write(t, filepath.Join(dir, "b.txt"), "3\n")
	m := newTestModel(t, dir)
	press(m, "j")
	if m.selectedPath() != "b.txt" {
		t.Fatalf("selected %q", m.selectedPath())
	}
	press(m, "tab")
	m.refresh()
	press(m, "j")
	if m.rows[m.cursor].Label != "init" {
		t.Fatalf("history cursor on %q", m.rows[m.cursor].Label)
	}
	press(m, "tab")
	if m.selectedPath() != "b.txt" {
		t.Fatalf("back in the changes, selected %q", m.selectedPath())
	}
	press(m, "tab")
	if m.rows[m.cursor].Label != "init" {
		t.Fatalf("back in the history, cursor on %q", m.rows[m.cursor].Label)
	}
}

func TestReviewingIsForTheChangesOnly(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n"})
	m := newTestModel(t, dir)
	press(m, "tab")
	m.refresh()
	press(m, "v")
	if len(m.reviewed) != 0 {
		t.Fatalf("reviewed %v", m.reviewed)
	}
}

func TestTheDateOfACommitTellsItsAge(t *testing.T) {
	now := time.Date(2026, 10, 26, 15, 0, 0, 0, time.Local)
	for _, c := range []struct {
		at   time.Time
		want lipgloss.Style
	}{
		{now.Add(-10 * time.Minute), styleFresh},
		{now.Add(-3 * time.Hour), lipgloss.NewStyle()},
		{now.Add(-20 * time.Hour), styleDir},
		{time.Date(2026, 10, 24, 23, 0, 0, 0, time.Local), styleDim},
	} {
		if got := ageStyle(c.at, now); got.GetForeground() != c.want.GetForeground() || got.GetBold() != c.want.GetBold() {
			t.Errorf("%v: got %v", c.at, got.GetForeground())
		}
	}
}

func TestTheHistoryFollowsNewCommits(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n"})
	m := newTestModel(t, dir)
	press(m, "tab")
	m.refresh()
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	run(t, dir, "git", "commit", "-qam", "Committed while the history is shown.")
	m.refresh()
	assertList(t, m.historyList(), "  "+filepath.Base(dir)+" Committed while the history is shown.", "  "+filepath.Base(dir)+" init")
}
