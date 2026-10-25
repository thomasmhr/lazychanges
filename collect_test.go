package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSingleRepositoryShowsEveryKindOfChange(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n2\n", "gone.txt": "x\n", "lib/b.go": "package b\n"})
	write(t, filepath.Join(dir, "a.txt"), "1\n3\n4\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
	write(t, filepath.Join(dir, "lib/new.go"), "package b\n\nfunc F() {}\n")
	write(t, filepath.Join(dir, "img.bin"), "\x00\x01\x02")
	write(t, filepath.Join(dir, ".gitignore"), "ignored.log\n")
	write(t, filepath.Join(dir, "ignored.log"), "noise\n")
	run(t, dir, "git", "add", ".gitignore")

	ctx := detect(t, dir)
	if ctx.Kind != KindRepo || len(ctx.Repos) != 1 {
		t.Fatalf("kind %s, %d repos", ctx.Kind, len(ctx.Repos))
	}
	snap := Collect(ctx.Repos)
	assertList(t, listing(snap), "? lib/new.go", "A .gitignore", "M a.txt", "D gone.txt", "? img.bin")

	byPath := map[string]Change{}
	for _, c := range snap.Changes {
		byPath[c.Path] = c
	}
	if c := byPath["a.txt"]; c.Add != 2 || c.Del != 1 {
		t.Errorf("a.txt +%d -%d", c.Add, c.Del)
	}
	if c := byPath["lib/new.go"]; c.Add != 3 {
		t.Errorf("new.go +%d", c.Add)
	}
	if !byPath["img.bin"].Binary {
		t.Error("img.bin should be binary")
	}

	d, err := Diff(byPath["lib/new.go"])
	if err != nil || !strings.Contains(d, "+func F() {}") {
		t.Errorf("untracked diff: %v\n%s", err, d)
	}
	d, err = Diff(byPath["gone.txt"])
	if err != nil || !strings.Contains(d, "-x") {
		t.Errorf("deleted diff: %v\n%s", err, d)
	}
}

func TestCollectingNeverTouchesTheIndex(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n"})
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	write(t, filepath.Join(dir, "new.txt"), "n\n")
	index := filepath.Join(dir, ".git", "index")
	before, _ := os.ReadFile(index)
	ctx := detect(t, dir)
	for _, c := range Collect(ctx.Repos).Changes {
		Diff(c)
	}
	after, _ := os.ReadFile(index)
	if string(before) != string(after) {
		t.Fatal("the index changed")
	}
}

func TestCommittedWorkLeavesTheList(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n"})
	ctx := detect(t, dir)
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	write(t, filepath.Join(dir, "b.txt"), "b\n")
	assertList(t, listing(Collect(ctx.Repos)), "M a.txt", "? b.txt")

	run(t, dir, "git", "commit", "-qam", "agent")
	assertList(t, listing(Collect(ctx.Repos)), "? b.txt")
}

func TestRepositoryWithoutCommits(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q")
	write(t, filepath.Join(dir, "a.txt"), "1\n")
	run(t, dir, "git", "add", "a.txt")
	write(t, filepath.Join(dir, "b.txt"), "1\n")
	ctx := detect(t, dir)
	assertList(t, listing(Collect(ctx.Repos)), "A a.txt", "? b.txt")
}

func TestLargeFilesAreListedNotLoaded(t *testing.T) {
	dir := repo(t, t.TempDir(), nil)
	write(t, filepath.Join(dir, "dump.log"), strings.Repeat("line of a log\n", 300_000))
	ctx := detect(t, dir)
	c := Collect(ctx.Repos).Changes[0]
	if !c.Large() || c.Stat() != "4.0 MB" {
		t.Fatalf("large %v, stat %q", c.Large(), c.Stat())
	}
	d, err := Diff(c)
	if err != nil || d != "Too large to show (4.0 MB).\n" {
		t.Fatalf("diff %q, %v", d, err)
	}
}

func TestUntrackedLineCountsFollowTheFile(t *testing.T) {
	dir := repo(t, t.TempDir(), nil)
	f := filepath.Join(dir, "new.txt")
	write(t, f, "1\n2\n")
	ctx := detect(t, dir)
	if c := Collect(ctx.Repos).Changes[0]; c.Add != 2 {
		t.Fatalf("+%d", c.Add)
	}
	write(t, f, "1\n2\n3\n")
	if c := Collect(ctx.Repos).Changes[0]; c.Add != 3 {
		t.Fatalf("a cached count must not outlive a change, +%d", c.Add)
	}
}
