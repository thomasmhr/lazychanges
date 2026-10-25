package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMetaRepositoryFindsIgnoredNestedRepositories(t *testing.T) {
	root := repo(t, t.TempDir(), map[string]string{".gitignore": "/apps/*/\n", "docs/map.md": "m\n"})
	front := repo(t, filepath.Join(root, "apps", "front"), map[string]string{"app.rb": "a\n"})
	repo(t, filepath.Join(root, "apps", "quiet"), map[string]string{"x": "x\n"})
	repo(t, filepath.Join(root, "apps", "front", "node_modules", "dep"), nil)
	write(t, filepath.Join(root, "docs/map.md"), "m2\n")
	write(t, filepath.Join(front, "app.rb"), "b\n")

	ctx := detect(t, root)
	if ctx.Kind != KindMeta || len(ctx.Repos) != 3 {
		t.Fatalf("kind %s, %d repos", ctx.Kind, len(ctx.Repos))
	}
	snap := Collect(ctx.Repos)
	assertList(t, listing(snap), "M docs/map.md", "M apps/front/app.rb")

	var labels []string
	for _, r := range BuildTree(ctx, snap, nil) {
		labels = append(labels, strings.Repeat("  ", r.Depth)+r.Label)
	}
	assertList(t, labels, filepath.Base(root), "  docs/", "    map.md", "apps/front", "  app.rb")
}

func TestUntrackedNestedRepositoryIsItsOwnRepository(t *testing.T) {
	root := repo(t, t.TempDir(), map[string]string{"r.txt": "r\n"})
	inner := repo(t, filepath.Join(root, "inner"), map[string]string{"i.txt": "i\n"})
	write(t, filepath.Join(inner, "i.txt"), "j\n")
	ctx := detect(t, root)
	assertList(t, listing(Collect(ctx.Repos)), "M inner/i.txt")
}

func TestSubmodules(t *testing.T) {
	lib := repo(t, filepath.Join(t.TempDir(), "lib"), map[string]string{"l.go": "l\n"})
	root := repo(t, t.TempDir(), nil)
	run(t, root, "git", "submodule", "add", "-q", lib, "vendored")
	run(t, root, "git", "commit", "-qm", "sub")
	write(t, filepath.Join(root, "vendored", "l.go"), "m\n")
	ctx := detect(t, root)
	if ctx.Kind != KindSubmodules || len(ctx.Repos) != 2 {
		t.Fatalf("kind %s, %d repos", ctx.Kind, len(ctx.Repos))
	}
	got := listing(Collect(ctx.Repos))
	if got[len(got)-1] != "M vendored/l.go" {
		t.Fatalf("got %v", got)
	}
}

func TestMonorepo(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"package.json": `{"workspaces": ["packages/*"]}`})
	if k := detect(t, dir).Kind; k != KindMonorepo {
		t.Fatalf("kind %s", k)
	}
}

func TestSubdirectoryNarrowsTheView(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"pkg/a/x.go": "x\n", "pkg/b/y.go": "y\n", "top.txt": "t\n"})
	for _, f := range []string{"pkg/a/x.go", "pkg/b/y.go", "top.txt"} {
		write(t, filepath.Join(dir, f), "changed\n")
	}
	write(t, filepath.Join(dir, "pkg/a/new.go"), "n\n")
	ctx := detect(t, filepath.Join(dir, "pkg", "a"))
	if ctx.Name != filepath.Base(dir)+"/pkg/a" {
		t.Errorf("name %s", ctx.Name)
	}
	snap := Collect(ctx.Repos)
	assertList(t, listing(snap), "? new.go", "M x.go")
	if d, _ := Diff(snap.Changes[1]); !strings.Contains(d, "+changed") {
		t.Errorf("diff %s", d)
	}
}

func TestFolderOfRepositories(t *testing.T) {
	root := t.TempDir()
	a := repo(t, filepath.Join(root, "a"), map[string]string{"f": "1\n"})
	repo(t, filepath.Join(root, "group", "b"), map[string]string{"g": "1\n"})
	repo(t, filepath.Join(root, "node_modules", "skip"), nil)
	write(t, filepath.Join(a, "f"), "2\n")
	ctx := detect(t, root)
	if ctx.Kind != KindFolder || len(ctx.Repos) != 2 {
		t.Fatalf("kind %s, %d repos", ctx.Kind, len(ctx.Repos))
	}
	assertList(t, listing(Collect(ctx.Repos)), "M a/f")
}

// --- the live view ---
