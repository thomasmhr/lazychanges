package main

import (
	"path"
	"sort"
	"strings"
)

type RowKind int

const (
	RowRepo RowKind = iota
	RowDir
	RowFile
	RowNote
)

type Row struct {
	Kind      RowKind
	Key       string
	Depth     int
	Label     string
	Parent    int   // index of the enclosing repo or directory row, -1 at the top
	Change    int   // index in the snapshot, for RowFile
	Files     []int // every change below, for RowRepo and RowDir
	Collapsed bool
}

func (r Row) Foldable() bool { return r.Kind == RowRepo || r.Kind == RowDir }

func repoKey(r *Repo) string          { return "R" + r.Dir }
func dirKey(r *Repo, p string) string { return "D" + r.Dir + "\x00" + p }

// BuildTree lays the changes out as a tree: one branch per repository when there are
// several, then directories, with chains of single directories folded into one row.
// Branches whose key is in collapsed keep their row but hide what is below.
func BuildTree(ctx *Context, snap Snapshot, collapsed map[string]bool) []Row {
	multi := len(ctx.Repos) > 1
	var rows []Row
	for _, r := range ctx.Repos {
		var idx []int
		for i, c := range snap.Changes {
			if c.Repo == r {
				idx = append(idx, i)
			}
		}
		err := snap.Errors[r]
		if multi && len(idx) == 0 && err == nil {
			continue
		}
		depth, parent := 0, -1
		if multi {
			label := r.Rel
			if label == "." {
				label = ctx.Name
			}
			key := repoKey(r)
			rows = append(rows, Row{Kind: RowRepo, Key: key, Label: label, Parent: -1, Files: idx, Collapsed: collapsed[key]})
			if collapsed[key] {
				continue
			}
			depth, parent = 1, len(rows)-1
		}
		if err != nil {
			rows = append(rows, Row{Kind: RowNote, Depth: depth, Parent: parent, Label: firstLine(err.Error())})
		}
		rows = dirRows(rows, r, snap, idx, depth, parent, collapsed)
	}
	return rows
}

type node struct {
	dirs  map[string]*node
	files []int
	all   []int
}

func dirRows(rows []Row, r *Repo, snap Snapshot, idx []int, depth, parent int, collapsed map[string]bool) []Row {
	root := &node{dirs: map[string]*node{}}
	for _, i := range idx {
		parts := strings.Split(scoped(snap.Changes[i]), "/")
		n := root
		for _, p := range parts[:len(parts)-1] {
			if n.dirs[p] == nil {
				n.dirs[p] = &node{dirs: map[string]*node{}}
			}
			n = n.dirs[p]
			n.all = append(n.all, i)
		}
		n.files = append(n.files, i)
	}
	var walk func(n *node, prefix string, depth, parent int)
	walk = func(n *node, prefix string, depth, parent int) {
		names := make([]string, 0, len(n.dirs))
		for name := range n.dirs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			d := n.dirs[name]
			label := name
			for len(d.files) == 0 && len(d.dirs) == 1 {
				for sub, child := range d.dirs {
					label += "/" + sub
					d = child
				}
			}
			key := dirKey(r, prefix+label)
			rows = append(rows, Row{Kind: RowDir, Key: key, Depth: depth, Label: label + "/", Parent: parent, Files: d.all, Collapsed: collapsed[key]})
			if !collapsed[key] {
				walk(d, prefix+label+"/", depth+1, len(rows)-1)
			}
		}
		for _, i := range n.files {
			c := snap.Changes[i]
			rows = append(rows, Row{Kind: RowFile, Key: c.Key(), Depth: depth, Label: path.Base(c.Path), Parent: parent, Change: i})
		}
	}
	walk(root, "", depth, parent)
	return rows
}

// scoped is the path of a change inside the directory the view is narrowed to.
func scoped(c Change) string {
	if c.Repo.Scope != "" {
		return strings.TrimPrefix(c.Path, c.Repo.Scope+"/")
	}
	return c.Path
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
