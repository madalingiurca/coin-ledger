package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
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

// The numbers in the pane titles are keys that jump to those screens.
func TestNumberKeysJumpBetweenScreens(t *testing.T) {
	m := app(t, testStore(t))
	for _, step := range []struct {
		key   string
		want  screen
		title string
	}{{"2", screenHistory, "[2] History"}, {"3", screenPayees, "[3] Uncategorised payees"},
		{"4", screenImport, "[4] Import"}, {"1", screenMonth, "[1] "}} {
		m = press(t, m, step.key)
		if m.screen != step.want {
			t.Errorf("%s went to screen %d, want %d", step.key, m.screen, step.want)
		}
		mustContain(t, plain(m), step.title)
	}
}

// A dialog opens over the screen rather than under it: the month stays where
// it was, the screen keeps its height, and the dialog carries its keys.
func TestDialogsOpenOverTheScreen(t *testing.T) {
	s := testStore(t)
	mo := model.New(model.Today())
	mo.Add(model.Income, "Salary", money.FromLei(12500), true)
	if err := s.Save(mo); err != nil {
		t.Fatal(err)
	}
	m := app(t, s)
	height := strings.Count(plain(m), "\n")

	for _, key := range []string{"a", "d"} {
		open := press(t, m, key)
		screen := plain(open)
		if got := strings.Count(screen, "\n"); got != height {
			t.Errorf("%s: screen is %d lines with the dialog, %d without", key, got, height)
		}
		mustContain(t, screen, "[1] "+model.Today().Label())
		checkWidth(t, open)
	}
	mustContain(t, plain(press(t, m, "a")), "Add income", "esc cancel─╯")
	mustContain(t, plain(press(t, m, "d")), "Delete Salary", "y delete · n keep─╯")
}

// Every screen is named in the main pane's border, so the number keys show.
func TestTabsShowEveryScreen(t *testing.T) {
	m := app(t, testStore(t))
	mustContain(t, plain(m), "[1] "+model.Today().Label()+" · 2 History · 3 Payees · 4 Import")
	mustContain(t, plain(press(t, m, "2")), "1 Month · [2] History · 3 Payees · 4 Import")

	narrow := New(m.store)
	next, _ := narrow.Update(tea.WindowSizeMsg{Width: 44, Height: 20})
	mustContain(t, plain(next.(Model)), "[1] "+model.Today().Label()+" · 2 · 3 · 4")
}

// Income and expenses are panes of their own; the one holding the cursor is
// focused, and tab moves between them.
func TestIncomeAndExpensesAreSeparatePanes(t *testing.T) {
	s := testStore(t)
	mo := model.New(model.Today())
	mo.Add(model.Income, "Salary", money.FromLei(12500), true)
	mo.Add(model.Expense, "Rent", money.FromLei(2800), true)
	mo.Add(model.Expense, "Food", money.FromLei(900), false)
	if err := s.Save(mo); err != nil {
		t.Fatal(err)
	}
	m := app(t, s)
	mustContain(t, plain(m), "╭─Expenses─", "1 of 1─╯")

	m = press(t, m, "tab")
	if r, _ := m.dash.current(); r.kind != model.Expense {
		t.Fatalf("tab left the cursor in %v", r.kind)
	}
	mustContain(t, plain(m), "1 of 2─╯")
	m = press(t, m, "tab")
	if r, _ := m.dash.current(); r.kind != model.Income {
		t.Errorf("tab did not go back to income")
	}
}
