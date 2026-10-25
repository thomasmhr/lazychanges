package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestReviewedFilesStayReviewedUntilTheirContentChanges(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n", "b.txt": "1\n", "c.txt": "1\n"})
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		write(t, filepath.Join(dir, f), "2\n")
	}
	m := newTestModel(t, dir)
	press(m, "v")
	if m.selectedPath() != "b.txt" {
		t.Fatalf("v should move on, selected %q", m.selectedPath())
	}
	press(m, "v")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "2/3 reviewed") || !strings.Contains(v, "√M a.txt") {
		t.Fatalf("view:\n%s", v)
	}

	later := time.Now().Add(2 * time.Second)
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	os.Chtimes(filepath.Join(dir, "a.txt"), later, later)
	write(t, filepath.Join(dir, "b.txt"), "3\n")
	os.Chtimes(filepath.Join(dir, "b.txt"), later, later)
	m.refresh()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "1/3 reviewed") {
		t.Fatalf("a rewrite with the same content stays reviewed, a new content does not:\n%s", v)
	}
	if !m.isReviewed(m.snap.Changes[0]) || m.isReviewed(m.snap.Changes[1]) {
		t.Fatal("a.txt should stay reviewed, b.txt should not")
	}
}

func TestReviewingABranchMarksEveryFileBelow(t *testing.T) {
	m, _ := foldFixture(t)
	press(m, "j", "j", "v")
	if m.reviewedCount() != 2 {
		t.Fatalf("reviewed %d", m.reviewedCount())
	}
	press(m, "v")
	if m.reviewedCount() != 0 {
		t.Fatalf("a second v unmarks, reviewed %d", m.reviewedCount())
	}
}

func TestReviewingAFileJumpsToTheNextFileLeftToReview(t *testing.T) {
	m, _ := foldFixture(t)
	press(m, "v")
	if m.selectedPath() != "apps/front/app/a.rb" {
		t.Fatalf("v skips the repository row, selected %q", m.selectedPath())
	}
	press(m, "v")
	if m.selectedPath() != "apps/front/app/b.rb" {
		t.Fatalf("selected %q", m.selectedPath())
	}
	if m.reviewedCount() != 2 {
		t.Fatalf("reviewed %d", m.reviewedCount())
	}
}

func TestAFileJustChangedThenReviewedShowsAsReviewed(t *testing.T) {
	dir := repo(t, t.TempDir(), map[string]string{"a.txt": "1\n", "b.txt": "1\n"})
	write(t, filepath.Join(dir, "b.txt"), "2\n")
	m := newTestModel(t, dir)
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	m.refresh()
	press(m, "g", "v")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "√M a.txt") {
		t.Fatalf("view:\n%s", v)
	}
}
