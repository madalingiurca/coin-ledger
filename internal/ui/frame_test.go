package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestPaneDrawsTitleAndCounterInTheBorder(t *testing.T) {
	th := newTheme(true)
	out := pane{title: "[1] October 2026", counter: "3 of 20", body: "INCOME\nSalary", w: 30, h: 6, focused: true}.render(th)
	lines := strings.Split(stripANSI(out), "\n")

	if len(lines) != 6 {
		t.Fatalf("pane is %d lines, want 6:\n%s", len(lines), stripANSI(out))
	}
	if !strings.HasPrefix(lines[0], "╭─[1] October 2026─") || !strings.HasSuffix(lines[0], "╮") {
		t.Errorf("title not in the top border: %q", lines[0])
	}
	if !strings.HasSuffix(lines[5], "─3 of 20─╯") {
		t.Errorf("counter not in the bottom border: %q", lines[5])
	}
	if lines[1] != "│ INCOME                     │" {
		t.Errorf("body line = %q", lines[1])
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 30 {
			t.Errorf("line %d is %d cells, want 30: %q", i, w, l)
		}
	}
}

func TestPaneFitsAnyWidth(t *testing.T) {
	th := newTheme(true)
	for _, w := range []int{4, 5, 8, 12, 20} {
		out := pane{title: "[3] Uncategorised payees", counter: "12 of 140", body: "a line far wider than the pane is", w: w}.render(th)
		for i, l := range strings.Split(out, "\n") {
			if got := lipgloss.Width(l); got != w {
				t.Errorf("width %d: line %d is %d cells: %q", w, i, got, stripANSI(l))
			}
		}
	}
}

// The focused pane is told apart by colour alone, so the two must differ.
func TestFocusedPaneUsesRossoCorsa(t *testing.T) {
	th := newTheme(true)
	on := pane{title: "a", w: 10, h: 3, focused: true}.render(th)
	off := pane{title: "a", w: 10, h: 3}.render(th)
	if on == off {
		t.Fatal("focused and unfocused panes render the same")
	}
	if !strings.Contains(on, "212;0;0") { // #D40000
		t.Errorf("focused border is not Rosso Corsa: %q", on)
	}
}

func TestKeybarKeepsTheMenuHint(t *testing.T) {
	th := newTheme(true)
	hints := []hint{keyHint("a", "add"), keyHint("e", "edit"), keyHint("d", "delete"), keyHint("?", "keys")}

	if got := stripANSI(keybar(th, 80, hints...)); got != "Add: a | Edit: e | Delete: d | Keys: ?" {
		t.Errorf("wide bar = %q", got)
	}
	got := stripANSI(keybar(th, 20, hints...))
	if got != "Add: a | Keys: ?" {
		t.Errorf("narrow bar = %q", got)
	}
	if lipgloss.Width(keybar(th, 5, hints...)) > 5 {
		t.Error("bar wider than the terminal")
	}
}
