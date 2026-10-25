package main

import (
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

type review struct {
	sig     string // change signature when it was reviewed, a cheap first check
	content string // content hash, the real one: a rewrite with the same content stays reviewed
}

// contentHash fingerprints the file as it is on disk, "-" when it is gone.
func contentHash(c Change) string {
	f, err := os.Open(filepath.Join(c.Repo.Dir, c.Path))
	if err != nil {
		return "-"
	}
	defer f.Close()
	h := fnv.New64a()
	if _, err := io.Copy(h, f); err != nil {
		return "?"
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

func (m *model) isReviewed(c Change) bool {
	_, ok := m.reviewed[c.Key()]
	return ok
}

// recheckReviews drops the review of every file whose content changed since.
func (m *model) recheckReviews() {
	for _, c := range m.snap.Changes {
		rv, ok := m.reviewed[c.Key()]
		if !ok {
			continue
		}
		sig := c.signature()
		if sig == rv.sig {
			continue
		}
		if h := contentHash(c); h == rv.content {
			m.reviewed[c.Key()] = review{sig, h}
		} else {
			delete(m.reviewed, c.Key())
		}
	}
}

// nextUnreviewed moves to the next file left to review, else to the next file.
func (m *model) nextUnreviewed() tea.Cmd {
	next := -1
	for i := m.cursor + 1; i < len(m.rows); i++ {
		if m.rows[i].Kind != RowFile {
			continue
		}
		if !m.isReviewed(m.snap.Changes[m.rows[i].Change]) {
			next = i
			break
		}
		if next < 0 {
			next = i
		}
	}
	if next < 0 {
		return nil
	}
	m.cursor = next
	return m.selectCursor()
}

// toggleReviewed marks the file under the cursor, or every file of the branch under it, as
// reviewed; when they all already are, it unmarks them. It reports whether it was a file.
func (m *model) toggleReviewed() bool {
	if m.cursor < 0 {
		return false
	}
	r := m.rows[m.cursor]
	files := r.Files
	if r.Kind == RowFile {
		files = []int{r.Change}
	}
	if len(files) == 0 {
		return false
	}
	all := true
	for _, i := range files {
		all = all && m.isReviewed(m.snap.Changes[i])
	}
	for _, i := range files {
		c := m.snap.Changes[i]
		if all {
			delete(m.reviewed, c.Key())
		} else {
			m.reviewed[c.Key()] = review{c.signature(), contentHash(c)}
		}
	}
	return r.Kind == RowFile
}

func (m *model) reviewedCount() int {
	n := 0
	for _, c := range m.snap.Changes {
		if m.isReviewed(c) {
			n++
		}
	}
	return n
}

func (m *model) allReviewed(files []int) bool {
	for _, i := range files {
		if !m.isReviewed(m.snap.Changes[i]) {
			return false
		}
	}
	return len(files) > 0
}
