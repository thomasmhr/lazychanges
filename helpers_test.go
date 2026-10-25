package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "lazychanges-home")
	os.Setenv("HOME", home)
	os.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.WriteFile(filepath.Join(home, "gitconfig"), []byte("[user]\n\tname = t\n\temail = t@t\n[init]\n\tdefaultBranch = main\n[protocol \"file\"]\n\tallow = always\n"), 0o644)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo creates a repository with the given files committed.
func repo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	run(t, dir, "git", "init", "-q")
	for p, c := range files {
		write(t, filepath.Join(dir, p), c)
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "init", "--allow-empty")
	return dir
}

func detect(t *testing.T, dir string) *Context {
	t.Helper()
	ctx, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func listing(snap Snapshot) []string {
	var out []string
	for _, c := range snap.Changes {
		out = append(out, string(c.Status)+" "+c.Display())
	}
	return out
}

func assertList(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func newTestModel(t *testing.T, dir string) *model {
	t.Helper()
	m := newModel(detect(t, dir), time.Second)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.refresh()
	return m
}

// refresh runs one collection and the diff load it triggers, synchronously.
func (m *model) refresh() {
	m.loading = true
	msg := m.collect()()
	m.Update(msg)
	if c, ok := m.selected(); ok && (c.Key() != m.diffKey || c.signature() != m.diffSig) {
		m.Update(m.loadDiff(c)())
	}
}

func (m *model) selectedPath() string {
	if c, ok := m.selected(); ok {
		return c.Display()
	}
	return ""
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		if _, cmd := m.Update(msg); cmd != nil {
			if d, ok := cmd().(diffMsg); ok {
				m.Update(d)
			}
		}
	}
}

func (m *model) visible() []string {
	var out []string
	for _, r := range m.rows {
		out = append(out, strings.Repeat("  ", r.Depth)+r.Label)
	}
	return out
}

func foldFixture(t *testing.T) (*model, string) {
	root := repo(t, t.TempDir(), map[string]string{".gitignore": "/apps/*/\n", "top.md": "t\n"})
	front := repo(t, filepath.Join(root, "apps", "front"), map[string]string{"app/a.rb": "a\n", "app/b.rb": "b\n"})
	write(t, filepath.Join(root, "top.md"), "u\n")
	write(t, filepath.Join(front, "app/a.rb"), "x\n")
	write(t, filepath.Join(front, "app/b.rb"), "y\n")
	return newTestModel(t, root), filepath.Base(root)
}
