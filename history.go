package main

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Commit struct {
	Repo       *Repo
	Hash       string
	Parent     string // first parent, the empty tree for a root commit
	Time       time.Time
	Author     string
	Message    string
	Unpushed   bool
	Start, End int // its files in the snapshot's Changes
}

func (c Commit) Subject() string { return firstLine(c.Message) }

func (c Commit) Key() string { return "C" + c.Repo.Dir + "\x00" + c.Hash }

func (c Commit) Short() string { return c.Hash[:min(len(c.Hash), 7)] }

// maxUnpushed bounds the unpushed commits one repository adds to the history.
const maxUnpushed = 300

const logFormat = "--format=%x1e%H%x1f%P%x1f%ct%x1f%an%x1f%B"

// CollectHistory gathers the latest commits of the current branch of every repository, newest
// first, as many as limit across them all, plus every commit not pushed yet. Commits that are on
// no remote are unpushed; a repository without remotes has none.
func CollectHistory(repos []*Repo, limit int) Snapshot {
	snap := Snapshot{Errors: map[*Repo]error{}}
	logs := make([][]Commit, len(repos))
	errs := make([]error, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logs[i], errs[i] = logRepo(r, limit)
		}()
	}
	wg.Wait()

	var all []Commit
	for i, r := range repos {
		all = append(all, logs[i]...)
		if errs[i] != nil {
			snap.Errors[r] = errs[i]
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].Time.After(all[b].Time) })
	var kept []Commit
	for i, c := range all {
		if i < limit || c.Unpushed {
			kept = append(kept, c)
		}
	}

	files := make([][]Change, len(kept))
	ferrs := make([]error, len(kept))
	for i, c := range kept {
		wg.Add(1)
		go func() {
			defer wg.Done()
			files[i], ferrs[i] = commitFiles.get(c.Repo, c)
		}()
	}
	wg.Wait()
	for i, c := range kept {
		if ferrs[i] != nil {
			snap.Errors[c.Repo] = ferrs[i]
			continue
		}
		c.Start = len(snap.Changes)
		snap.Changes = append(snap.Changes, files[i]...)
		c.End = len(snap.Changes)
		snap.Commits = append(snap.Commits, c)
	}
	return snap
}

// logRepo reads the latest commits of a repository and the unpushed ones.
func logRepo(r *Repo, limit int) ([]Commit, error) {
	if headCommit(r.Dir) == emptyTree {
		return nil, nil
	}
	recent, err := git(r.Dir, append([]string{"log", "HEAD", "--max-count=" + strconv.Itoa(limit), logFormat}, pathspec(r)...)...)
	if err != nil {
		return nil, err
	}
	commits := parseLog(r, recent)

	remotes, err := git(r.Dir, "remote")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(remotes))) == 0 {
		return commits, nil
	}
	out, err := git(r.Dir, append([]string{"log", "HEAD", "--not", "--remotes", "--max-count=" + strconv.Itoa(maxUnpushed), logFormat}, pathspec(r)...)...)
	if err != nil {
		return nil, err
	}
	unpushed := map[string]bool{}
	for _, c := range parseLog(r, out) {
		unpushed[c.Hash] = true
		if !containsCommit(commits, c.Hash) {
			commits = append(commits, c)
		}
	}
	for i := range commits {
		commits[i].Unpushed = unpushed[commits[i].Hash]
	}
	return commits, nil
}

func parseLog(r *Repo, out []byte) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.SplitN(rec, "\x1f", 5)
		if len(f) != 5 {
			continue
		}
		parent := emptyTree
		if ps := strings.Fields(f[1]); len(ps) > 0 {
			parent = ps[0]
		}
		ct, _ := strconv.ParseInt(f[2], 10, 64)
		commits = append(commits, Commit{
			Repo:    r,
			Hash:    f[0],
			Parent:  parent,
			Time:    time.Unix(ct, 0),
			Author:  f[3],
			Message: strings.TrimSpace(f[4]),
		})
	}
	return commits
}

func containsCommit(cs []Commit, hash string) bool {
	for _, c := range cs {
		if c.Hash == hash {
			return true
		}
	}
	return false
}

// commitFiles remembers the files of each commit: a commit never changes.
var commitFiles = &commitCache{m: map[string][]Change{}}

type commitCache struct {
	mu sync.Mutex
	m  map[string][]Change
}

func (cc *commitCache) get(r *Repo, c Commit) ([]Change, error) {
	key := r.Dir + "\x00" + r.Scope + "\x00" + c.Hash
	cc.mu.Lock()
	cs, ok := cc.m[key]
	cc.mu.Unlock()
	if !ok {
		var err error
		if cs, err = readCommitFiles(r, c); err != nil {
			return nil, err
		}
		cc.mu.Lock()
		cc.m[key] = cs
		cc.mu.Unlock()
	}
	out := make([]Change, len(cs))
	for i, ch := range cs {
		ch.Repo = r
		out[i] = ch
	}
	return out, nil
}

func readCommitFiles(r *Repo, c Commit) ([]Change, error) {
	ns, err := git(r.Dir, append([]string{"diff-tree", "-r", "-z", "--no-renames", "--name-status", c.Parent, c.Hash}, pathspec(r)...)...)
	if err != nil {
		return nil, err
	}
	num, err := git(r.Dir, append([]string{"diff-tree", "-r", "-z", "--no-renames", "--numstat", c.Parent, c.Hash}, pathspec(r)...)...)
	if err != nil {
		return nil, err
	}
	changes := parseStatus(r, ns, num)
	out := make([]Change, 0, len(changes))
	for _, ch := range changes {
		ch.Base, ch.Commit = c.Parent, c.Hash
		out = append(out, *ch)
	}
	sort.Slice(out, func(a, b int) bool { return lessPath(out[a].Path, out[b].Path) })
	return out, nil
}

// when is the short time of a commit: the hour today, the day before.
func when(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return t.Format("15:04")
	}
	return t.Format("Jan 02")
}

// depths are the numbers of commits the history steps through.
var depths = []int{100, 200, 500}
