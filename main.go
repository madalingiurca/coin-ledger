// Command budget is a monthly budget you keep in the terminal: set what comes
// in and what you have planned to spend, then check back to see where you
// stand.
//
// It takes no arguments. Months live as one JSON file each in ~/.budget, or in
// $BUDGET_HOME when that is set.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/store"
	"github.com/madalingiurca/budget/internal/ui"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3"; the
// justfile does this from the latest git tag.
var version = "dev"

func main() {
	ui.Version = version
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "budget:", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := store.Home()
	if err != nil {
		return err
	}
	s, err := store.Open(dir)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(ui.New(s)).Run()
	return err
}
