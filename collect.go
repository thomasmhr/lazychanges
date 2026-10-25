package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status byte

const (
	Added     Status = 'A'
	Modified  Status = 'M'
	Deleted   Status = 'D'
	TypeChg   Status = 'T'
	Untracked Status = '?'
)

type Change struct {
	Repo     *Repo
	Base     string
	Path     string // relative to the repository
	Status   Status
	Add, Del int
	Binary   bool
	Size     int64 // on disk, 0 when deleted
	Mtime    time.Time
}

// Stat is the short size of a change: lines added and removed, "bin", or the size of an
// untracked file too large to count.
func (c Change) Stat() string {
	switch {
	case c.Status == Untracked && c.Large():
		return humanSize(c.Size)
	case c.Binary:
		return "bin"
	case c.Add > 0 || c.Del > 0:
		return fmt.Sprintf("+%d −%d", c.Add, c.Del)
	}
	return ""
}

// counted reports whether Stat is a count of lines.
func (c Change) counted() bool {
	return !c.Binary && !(c.Status == Untracked && c.Large()) && (c.Add > 0 || c.Del > 0)
}

// Large changes are listed but their diff is not shown.
func (c Change) Large() bool { return c.Size > maxDiffSize }

// Key identifies a file across refreshes.
func (c Change) Key() string { return c.Repo.Dir + "\x00" + c.Path }

// Display is the path relative to the context root.
func (c Change) Display() string {
	if c.Repo.Scope != "" {
		return strings.TrimPrefix(c.Path, c.Repo.Scope+"/")
	}
	if c.Repo.Rel == "." {
		return c.Path
	}
	return path.Join(c.Repo.Rel, c.Path)
}

func (c Change) signature() string {
	return string(c.Status) + strconv.Itoa(c.Add) + "/" + strconv.Itoa(c.Del) + c.Mtime.String()
}

type Snapshot struct {
	Changes []Change
	Errors  map[*Repo]error
}

// Collect gathers the changes of every repository in parallel, measured from the commit
// each repository was at when the session started.
func Collect(repos []*Repo) Snapshot {
	snap := Snapshot{Errors: map[*Repo]error{}}
	results := make([][]Change, len(repos))
	errs := make([]error, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = collectRepo(r, r.Head)
		}()
	}
	wg.Wait()
	for i, r := range repos {
		cs := results[i]
		sort.Slice(cs, func(a, b int) bool { return lessPath(cs[a].Path, cs[b].Path) })
		snap.Changes = append(snap.Changes, cs...)
		if errs[i] != nil {
			snap.Errors[r] = errs[i]
		}
	}
	return snap
}

func pathspec(r *Repo) []string {
	if r.Scope == "" {
		return nil
	}
	return []string{"--", r.Scope}
}

func collectRepo(r *Repo, base string) ([]Change, error) {
	ns, err := git(r.Dir, append([]string{"diff", "--name-status", "-z", "--no-renames", "--no-ext-diff", base}, pathspec(r)...)...)
	if err != nil {
		return nil, err
	}
	num, err := git(r.Dir, append([]string{"diff", "--numstat", "-z", "--no-renames", "--no-ext-diff", base}, pathspec(r)...)...)
	if err != nil {
		return nil, err
	}
	untracked, err := git(r.Dir, append([]string{"ls-files", "-z", "--others", "--exclude-standard"}, pathspec(r)...)...)
	if err != nil {
		return nil, err
	}

	byPath := map[string]*Change{}
	var changes []*Change
	parts := splitZ(ns)
	for i := 0; i+1 < len(parts); i += 2 {
		c := &Change{Repo: r, Base: base, Path: parts[i+1], Status: Status(parts[i][0])}
		byPath[c.Path] = c
		changes = append(changes, c)
	}
	for _, rec := range splitZ(num) {
		f := strings.SplitN(rec, "\t", 3)
		if len(f) != 3 {
			continue
		}
		if c := byPath[f[2]]; c != nil {
			if f[0] == "-" {
				c.Binary = true
			} else {
				c.Add, _ = strconv.Atoi(f[0])
				c.Del, _ = strconv.Atoi(f[1])
			}
		}
	}
	for _, p := range splitZ(untracked) {
		if strings.HasSuffix(p, "/") {
			continue // a nested repository, listed on its own
		}
		changes = append(changes, &Change{Repo: r, Base: base, Path: p, Status: Untracked})
	}

	out := make([]Change, 0, len(changes))
	for _, c := range changes {
		if fi, err := os.Lstat(filepath.Join(r.Dir, c.Path)); err == nil {
			c.Mtime, c.Size = fi.ModTime(), fi.Size()
		}
		if c.Status == Untracked && !c.Large() {
			c.Add, c.Binary = lineCounts.get(filepath.Join(r.Dir, c.Path), c.Mtime, c.Size)
		}
		out = append(out, *c)
	}
	return out, nil
}

// maxDiffSize bounds the files whose diff is read: a log or a dump left by an agent is
// listed, not loaded.
const maxDiffSize = 2 << 20

// lineCounts remembers the line count of untracked files, read again only when they change.
var lineCounts = &lineCache{m: map[string]lineCount{}}

type lineCount struct {
	mtime  time.Time
	size   int64
	lines  int
	binary bool
}

type lineCache struct {
	mu sync.Mutex
	m  map[string]lineCount
}

func (lc *lineCache) get(file string, mtime time.Time, size int64) (int, bool) {
	lc.mu.Lock()
	e, ok := lc.m[file]
	lc.mu.Unlock()
	if ok && e.mtime.Equal(mtime) && e.size == size {
		return e.lines, e.binary
	}
	lines, binary := countLines(file)
	lc.mu.Lock()
	lc.m[file] = lineCount{mtime, size, lines, binary}
	lc.mu.Unlock()
	return lines, binary
}

func countLines(file string) (int, bool) {
	buf, err := os.ReadFile(file)
	if err != nil {
		return 0, false
	}
	if bytes.IndexByte(buf[:min(len(buf), 8000)], 0) >= 0 {
		return 0, true
	}
	lines := bytes.Count(buf, []byte{'\n'})
	if len(buf) > 0 && buf[len(buf)-1] != '\n' {
		lines++
	}
	return lines, false
}

// Diff returns the uncoloured diff of one change.
func Diff(c Change) (string, error) {
	if c.Large() {
		return fmt.Sprintf("Too large to show (%s).\n", humanSize(c.Size)), nil
	}
	var out []byte
	var err error
	if c.Status == Untracked {
		out, err = gitAllowing(c.Repo.Dir, []int{1}, "diff", "--no-color", "--no-ext-diff", "--no-index", "--", "/dev/null", c.Path)
	} else {
		out, err = git(c.Repo.Dir, "diff", "--no-color", "--no-ext-diff", "--no-renames", c.Base, "--", c.Path)
	}
	return string(out), err
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// lessPath sorts directories before files at each level, like a file tree.
func lessPath(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		aDir, bDir := i < len(as)-1, i < len(bs)-1
		if aDir != bDir {
			return aDir
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}
