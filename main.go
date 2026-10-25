// lazychanges shows, live, what has been modified in the repository or repositories under
// the current directory. It only reads: no command touches the index or the work tree.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	once := flag.Bool("once", false, "print the list of changes and exit")
	interval := flag.Duration("interval", 2*time.Second, "refresh interval")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: lazychanges [--once] [--interval 2s] [dir]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	ctx, err := Detect(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazychanges:", err)
		os.Exit(1)
	}

	if *once {
		printOnce(ctx)
		return
	}

	p := tea.NewProgram(newModel(ctx, *interval), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazychanges:", err)
		os.Exit(1)
	}
}

func printOnce(ctx *Context) {
	fmt.Printf("%s · %s · %d repositories\n", ctx.Name, ctx.Kind, len(ctx.Repos))
	snap := Collect(ctx.Repos)
	for r, err := range snap.Errors {
		fmt.Printf("! %s: %v\n", r.Rel, err)
	}
	for _, c := range snap.Changes {
		fmt.Printf("%c %s  %s\n", c.Status, printable(c.Display()), c.Stat())
	}
}
