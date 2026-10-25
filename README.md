# lazychanges

Watch what your coding agents change, live, across every repository under the current
directory — and keep track of what you have already reviewed.

![lazychanges: a coding agent edits files while the view follows](demo/demo.gif)

Start it at the beginning of a working session and leave it open. The tree of modified, added,
deleted and untracked files sits on the left, the diff of the selected file on the right; both
refresh on their own while agents work.

- **One view for many repositories.** Started in a meta-repository, a folder of clones or a
  repository with submodules, it shows every repository as a branch of the same tree.
- **What is not committed yet.** Changes are measured from each repository's current HEAD: once
  the work is committed, it leaves the view.
- **Review tracking.** `v` marks a file as reviewed; it becomes unreviewed again as soon as its
  content changes, so after an agent's turn only what it touched needs reading again.
- **Read-only.** It never writes the index and never takes `index.lock`, so it cannot get in the
  agents' way.

## Install

```
go install github.com/thomasmhr/lazychanges@latest
```

## Usage

```
lazychanges [dir]          # current directory by default
lazychanges --once [dir]   # plain-text list of the changes, no UI
```

`lazychanges` works out where it was started:

| started in | shows |
|---|---|
| a repository | its changes; from a subdirectory, only that subdirectory |
| a monorepo (`go.work`, `pnpm-workspace.yaml`, `package.json` workspaces, …) | the same, a subdirectory narrows the view to a package |
| a meta-repository | the repository and the repositories it holds, ignored or untracked |
| a repository with submodules | the repository and each submodule |
| a folder that is not a repository | the repositories it holds, three levels deep |

`node_modules`, `vendor`, `tmp` and other dependency or build directories are not searched.
Repositories cloned or removed during the session show up within about thirty seconds.

## Keys

| key | action |
|---|---|
| `j` `k` `↓` `↑`, `g` `G` | next, previous, first, last row |
| `h` `l` `←` `→` | fold or climb to the parent, unfold or enter |
| `enter` | fold or unfold the repository or directory |
| `-` `+` | fold, unfold everything |
| `v` | mark the file, or every file of the branch, as reviewed (again: unmark) |
| `space` `b`, `ctrl-d` `ctrl-u` | scroll the diff |
| `n` `N` | next, previous hunk |
| `w` | wrap long lines (on by default for `.md` and `.txt`) |
| `q` | quit |

The mouse wheel scrolls the list or the diff depending on the pane; a click selects a file or
folds a directory. To select text with the mouse, hold `shift` while dragging.

## Details

- A file that just changed carries a `●` for fifteen seconds; a reviewed file turns grey with a
  `√`, and the header counts how many are left.
- Reviews last as long as the session.
- Files over 2 MB are listed with their size; their diff is not loaded.
- Control characters in a file are shown in caret notation (`^M`, `^[`), never sent to the
  terminal.
- Every git command runs with `GIT_OPTIONAL_LOCKS=0`. The view refreshes every two seconds, or
  every twice the duration of the last refresh when that is longer.

## Development

```
go test ./...
go build -o ~/.local/bin/lazychanges .
vhs demo/demo.tape   # records demo/demo.gif; needs vhs, ttyd and ffmpeg
```

## License

MIT
