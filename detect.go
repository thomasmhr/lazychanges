package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Kind string

const (
	KindRepo       Kind = "repository"
	KindMonorepo   Kind = "monorepo"
	KindMeta       Kind = "meta-repository"
	KindSubmodules Kind = "repository with submodules"
	KindFolder     Kind = "folder of repositories"
)

type Repo struct {
	Dir   string // absolute
	Rel   string // relative to the context root, "." for the root itself
	Scope string // pathspec relative to Dir, "" for the whole repository
}

type Context struct {
	Root  string // absolute directory the paths are shown relative to, where changes was started
	Name  string
	Kind  Kind
	Repos []*Repo
}

// skipDirs are never searched for nested repositories.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".venv": true, "venv": true, "__pycache__": true,
	".cache": true, "tmp": true, "log": true, "target": true, "dist": true, "build": true,
	"coverage": true, ".bundle": true, "storage": true, ".git": true,
}

func isRepoDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Detect works out what dir is: a repository (whole or a subdirectory), a repository
// holding other repositories (meta-repository, submodules), or a plain folder of repositories.
func Detect(dir string) (*Context, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	ctx := &Context{Root: dir, Name: filepath.Base(dir)}

	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		for _, r := range scanFolder(dir, 3) {
			ctx.Repos = append(ctx.Repos, newRepo(r, dir, ""))
		}
		ctx.Kind = KindFolder
		return ctx, nil
	}

	top := strings.TrimSpace(string(out))
	scope, _ := filepath.Rel(top, dir)
	if scope == "." {
		scope = ""
	}
	ctx.Repos = append(ctx.Repos, newRepo(top, dir, filepath.ToSlash(scope)))

	submodules := false
	for _, n := range nestedRepos(top) {
		if !within(n.dir, dir) {
			continue
		}
		submodules = submodules || n.submodule
		ctx.Repos = append(ctx.Repos, newRepo(n.dir, dir, ""))
	}

	switch {
	case len(ctx.Repos) > 1 && submodules:
		ctx.Kind = KindSubmodules
	case len(ctx.Repos) > 1:
		ctx.Kind = KindMeta
	case isMonorepo(top):
		ctx.Kind = KindMonorepo
	default:
		ctx.Kind = KindRepo
	}
	if scope != "" {
		ctx.Name = filepath.Base(top) + "/" + filepath.ToSlash(scope)
	}
	return ctx, nil
}

func newRepo(dir, root, scope string) *Repo {
	rel, _ := filepath.Rel(root, dir)
	if strings.HasPrefix(rel, "..") {
		rel = "."
	}
	return &Repo{Dir: dir, Rel: filepath.ToSlash(rel), Scope: scope}
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

type nested struct {
	dir       string
	submodule bool
}

// nestedRepos asks git which directories it does not descend into (untracked or ignored
// directories, submodules) and keeps those that hold a repository, recursively.
func nestedRepos(top string) []nested {
	var found []nested
	seen := map[string]bool{}
	add := func(d string, sub bool) {
		if seen[d] {
			return
		}
		seen[d] = true
		found = append(found, nested{d, sub})
		found = append(found, nestedRepos(d)...)
	}

	if out, err := git(top, "ls-files", "-z", "--stage"); err == nil {
		for _, line := range splitZ(out) {
			if strings.HasPrefix(line, "160000 ") {
				if i := strings.IndexByte(line, '\t'); i >= 0 {
					d := filepath.Join(top, line[i+1:])
					if isRepoDir(d) {
						add(d, true)
					}
				}
			}
		}
	}

	for _, extra := range [][]string{{}, {"--ignored"}} {
		args := append([]string{"ls-files", "-z", "--others", "--exclude-standard", "--directory"}, extra...)
		out, err := git(top, args...)
		if err != nil {
			continue
		}
		for _, p := range splitZ(out) {
			if !strings.HasSuffix(p, "/") {
				continue
			}
			d := filepath.Join(top, strings.TrimSuffix(p, "/"))
			if skipDirs[filepath.Base(d)] {
				continue
			}
			if isRepoDir(d) {
				add(d, false)
				continue
			}
			for _, r := range scanFolder(d, 2) {
				add(r, false)
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].dir < found[j].dir })
	return found
}

// scanFolder finds the repositories under dir, without descending into them.
func scanFolder(dir string, depth int) []string {
	if depth == 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() || skipDirs[e.Name()] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		d := filepath.Join(dir, e.Name())
		if isRepoDir(d) {
			repos = append(repos, d)
		} else {
			repos = append(repos, scanFolder(d, depth-1)...)
		}
	}
	return repos
}

var monorepoMarkers = []string{
	"go.work", "pnpm-workspace.yaml", "lerna.json", "nx.json", "turbo.json", "rush.json",
}

func isMonorepo(top string) bool {
	for _, m := range monorepoMarkers {
		if _, err := os.Stat(filepath.Join(top, m)); err == nil {
			return true
		}
	}
	for _, f := range []struct{ file, needle string }{
		{"package.json", `"workspaces"`},
		{"Cargo.toml", "[workspace]"},
		{"pyproject.toml", "[tool.uv.workspace]"},
	} {
		if b, err := os.ReadFile(filepath.Join(top, f.file)); err == nil && strings.Contains(string(b), f.needle) {
			return true
		}
	}
	return false
}
