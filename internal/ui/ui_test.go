package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
	"github.com/madalingiurca/budget/internal/store"
)

const testW, testH = 90, 40

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// app returns a model sized as if the terminal had already reported itself.
func app(t *testing.T, s *store.Store) Model {
	t.Helper()
	m := New(s)
	next, _ := m.Update(tea.WindowSizeMsg{Width: testW, Height: testH})
	return next.(Model)
}

// press feeds a sequence of keystrokes, in the form String() reports them.
func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(keyMsg(k))
		m = next.(Model)
	}
	return m
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	}
	r := []rune(k)
	if len(r) != 1 {
		panic("keyMsg: not a single key: " + k)
	}
	return tea.KeyPressMsg{Code: r[0], Text: k}
}

// typing spells out a string one keystroke at a time.
func typing(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		if r == ' ' {
			out = append(out, "space")
			continue
		}
		out = append(out, string(r))
	}
	return out
}

// plain strips styling so tests can assert on what the user actually reads.
func plain(m Model) string {
	var out strings.Builder
	for _, line := range strings.Split(m.View().Content, "\n") {
		out.WriteString(strings.TrimRight(stripANSI(line), " "))
		out.WriteByte('\n')
	}
	return out.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' && s[i] != '\a' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func mustContain(t *testing.T, screen string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(screen, w) {
			t.Errorf("screen is missing %q:\n%s", w, screen)
		}
	}
}

// Every rendered line must fit the terminal, or the frame tears.
func checkWidth(t *testing.T, m Model) {
	t.Helper()
	for i, line := range strings.Split(m.View().Content, "\n") {
		if w := lipgloss.Width(line); w > testW {
			t.Errorf("line %d is %d cells wide, terminal is %d:\n%q", i, w, testW, stripANSI(line))
		}
	}
}

func TestFirstRunIsInviting(t *testing.T) {
	m := app(t, testStore(t))
	screen := plain(m)
	mustContain(t, screen, "[1] "+model.Today().Label(), "INCOME", "EXPENSES",
		"nothing here yet", "new month · nothing saved yet", "0,00 lei")
	checkWidth(t, m)
}

func TestAddIncomeThroughTheForm(t *testing.T) {
	s := testStore(t)
	m := app(t, s)

	m = press(t, m, "a") // cursor starts in INCOME
	mustContain(t, plain(m), "Add income", "Label", "Amount", "Recurring")

	m = press(t, m, typing("Salary")...)
	m = press(t, m, "tab")
	m = press(t, m, typing("12500")...)
	m = press(t, m, "tab", "space") // mark it recurring
	m = press(t, m, "enter")

	if m.form != nil {
		t.Fatalf("form stayed open: %+v", m.form.err)
	}
	screen := plain(m)
	mustContain(t, screen, "Salary", "12.500,00", "Added Salary")
	checkWidth(t, m)

	// It must have reached disk, recurring flag and all.
	saved, err := s.Load(model.Today())
	if err != nil {
		t.Fatalf("the entry was not saved: %v", err)
	}
	if got := saved.TotalIncome(); got != money.FromLei(12500) {
		t.Errorf("saved income = %s", got)
	}
	if !saved.Entries(model.Income)[0].Recurring {
		t.Error("the recurring checkbox did not stick")
	}
}

func TestFormRejectsBadInput(t *testing.T) {
	m := app(t, testStore(t))

	m = press(t, m, "a", "enter") // no label
	if m.form == nil {
		t.Fatal("an empty label should have kept the form open")
	}
	mustContain(t, plain(m), "Give it a name")

	m = press(t, m, typing("Rent")...)
	m = press(t, m, "tab")
	m = press(t, m, typing("abc")...)
	m = press(t, m, "enter")
	if m.form == nil {
		t.Fatal("a nonsense amount should have kept the form open")
	}
	mustContain(t, plain(m), "not a number")

	// Correct it and the form closes.
	m = press(t, m, "backspace")
	m.form.amount.SetValue("2800")
	m = press(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form still open: %q", m.form.err)
	}
	mustContain(t, plain(m), "2.800,00")
}

func TestEscapeAbandonsTheForm(t *testing.T) {
	s := testStore(t)
	m := app(t, s)
	m = press(t, m, "a")
	m = press(t, m, typing("Oops")...)
	m = press(t, m, "esc")

	if m.form != nil {
		t.Fatal("esc should close the form")
	}
	if strings.Contains(plain(m), "Oops") {
		t.Error("an abandoned entry was added anyway")
	}
	if s.Exists(model.Today()) {
		t.Error("cancelling wrote a file")
	}
}

// Building a month, then checking every number the dashboard reports.
func TestDashboardArithmetic(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Income, "Salary", money.FromLei(12500), true)
	seed.Add(model.Income, "Freelance", money.FromLei(2000), false)
	seed.Add(model.Expense, "Rent", money.FromLei(2800), true)
	seed.Add(model.Expense, "Car insurance", money.FromLei(1450), false)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	m := app(t, s)
	screen := plain(m)
	mustContain(t, screen,
		"14.500,00 lei", // income total
		"4.250,00 lei",  // expense total
		"LEFT",
		"10.250,00 lei", // left
		"70% of income",
	)
	checkWidth(t, m)
}

func TestOverspendingIsCalledOut(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Income, "Salary", money.FromLei(10000), true)
	seed.Add(model.Expense, "Holiday", money.FromLei(14000), false)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	m := app(t, s)
	screen := plain(m)
	mustContain(t, screen, "OVER BUDGET", "-4.000,00 lei", "overspent by 40% of income")
	if strings.Contains(screen, "LEFT") {
		t.Error("a month in the red should not say LEFT")
	}
	checkWidth(t, m)
}

func TestDeleteAsksFirst(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Expense, "Rent", money.FromLei(2800), true)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	m := app(t, s)
	m = press(t, m, "down") // onto the expense row
	m = press(t, m, "d")
	mustContain(t, plain(m), "Delete Rent (2.800,00 lei)?")

	m = press(t, m, "n")
	if m.confirm != nil {
		t.Fatal("n should dismiss the prompt")
	}
	mustContain(t, plain(m), "Rent")

	m = press(t, m, "d", "y")
	screen := plain(m)
	mustContain(t, screen, "Deleted Rent")
	if strings.Contains(screen, "2.800,00") {
		t.Error("the deleted row is still on screen")
	}
	saved, err := s.Load(model.Today())
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Entries(model.Expense)) != 0 {
		t.Error("the delete was not saved")
	}
}

func TestToggleRecurring(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Income, "Salary", money.FromLei(12500), false)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	m := app(t, s)
	m = press(t, m, "r")
	mustContain(t, plain(m), "will carry to next month", "↻")

	saved, _ := s.Load(model.Today())
	if !saved.Entries(model.Income)[0].Recurring {
		t.Fatal("toggle was not saved")
	}

	m = press(t, m, "r")
	mustContain(t, plain(m), "no longer carries")
	saved, _ = s.Load(model.Today())
	if saved.Entries(model.Income)[0].Recurring {
		t.Error("toggling back was not saved")
	}
}

// The heart of the design: stepping forward carries the recurring entries, and
// browsing alone writes nothing.
func TestNextMonthCarriesRecurringEntries(t *testing.T) {
	s := testStore(t)
	this := model.Today()
	seed := model.New(this)
	seed.Add(model.Income, "Salary", money.FromLei(12500), true)
	seed.Add(model.Income, "Freelance", money.FromLei(2000), false)
	seed.Add(model.Expense, "Rent", money.FromLei(2800), true)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	m := app(t, s)
	m = press(t, m, "right")

	screen := plain(m)
	mustContain(t, screen, this.Next().Label(), "Salary", "Rent",
		"recurring entries carried from "+this.Label())
	if strings.Contains(screen, "Freelance") {
		t.Error("a one-off entry was carried forward")
	}
	if s.Exists(this.Next()) {
		t.Error("merely looking at next month created a file")
	}
	checkWidth(t, m)

	// The first edit is what commits it.
	m = press(t, m, "r")
	if !s.Exists(this.Next()) {
		t.Error("editing the new month did not save it")
	}

	// And going back finds the original month untouched.
	m = press(t, m, "left")
	mustContain(t, plain(m), this.Label(), "Freelance")
}

func TestTodayJumpsBack(t *testing.T) {
	m := app(t, testStore(t))
	m = press(t, m, "right", "right", "right")
	if m.dash.key != model.Today().Shift(3) {
		t.Fatalf("landed on %v", m.dash.key)
	}
	m = press(t, m, "t")
	if m.dash.key != model.Today() {
		t.Errorf("t landed on %v, want today", m.dash.key)
	}
}

func TestHistoryScreen(t *testing.T) {
	s := testStore(t)
	base := model.Key{Year: 2026, Month: time.September}
	for i, amount := range []int64{7750, 3400, -1500} {
		mo := model.New(base.Shift(-i))
		mo.Add(model.Income, "Salary", money.FromLei(10000), true)
		mo.Add(model.Expense, "Spending", money.FromLei(10000-amount), false)
		if err := s.Save(mo); err != nil {
			t.Fatal(err)
		}
	}

	m := app(t, s)
	m = press(t, m, "h")
	if m.screen != screenHistory {
		t.Fatal("h did not open history")
	}
	screen := plain(m)
	mustContain(t, screen, "[2] History", "MONTH", "INCOME", "EXPENSES", "LEFT",
		"2026-09", "2026-08", "2026-07", "7.750,00", "-1.500,00", "Average left over 3 months")
	checkWidth(t, m)

	// Selecting a month and opening it lands on that month's dashboard.
	m = press(t, m, "down", "enter")
	if m.screen != screenMonth {
		t.Fatal("enter did not return to the month view")
	}
	if m.dash.key != (model.Key{Year: 2026, Month: time.August}) {
		t.Errorf("opened %v, want 2026-08", m.dash.key)
	}
	mustContain(t, plain(m), "August 2026")
}

func TestHistoryWithNothingSaved(t *testing.T) {
	m := app(t, testStore(t))
	m = press(t, m, "h")
	mustContain(t, plain(m), "No months saved yet")
	checkWidth(t, m)

	m = press(t, m, "esc")
	if m.screen != screenMonth {
		t.Error("esc should return to the month view")
	}
}

func TestKeybindingsMenu(t *testing.T) {
	m := app(t, testStore(t))
	m = press(t, m, "?")
	screen := plain(m)
	mustContain(t, screen, "Keybindings: month", "add to the section the cursor is in",
		"previous month", "Global", "Months are saved in", "enter run · esc close")
	checkWidth(t, m)

	// Typing narrows the list; enter runs what is left.
	m = press(t, m, typing("hist")...)
	screen = plain(m)
	mustContain(t, screen, "history of the last 24 months")
	if strings.Contains(screen, "add to the section") {
		t.Errorf("the filter kept rows that do not match:\n%s", screen)
	}
	m = press(t, m, "enter")
	if m.menu != nil || m.screen != screenHistory {
		t.Errorf("enter did not run the binding: menu open %v, screen %d", m.menu != nil, m.screen)
	}

	m = press(t, m, "?", "esc")
	if m.menu != nil {
		t.Error("esc should close the menu")
	}
}

func TestCursorMovesThroughBothSections(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Income, "Salary", money.FromLei(12500), true)
	seed.Add(model.Expense, "Rent", money.FromLei(2800), true)
	seed.Add(model.Expense, "Utilities", money.FromLei(450), true)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	m := app(t, s)
	if r, _ := m.dash.current(); r.kind != model.Income {
		t.Error("the cursor should start on income")
	}
	m = press(t, m, "down")
	if _, e, _ := m.dash.currentEntry(); e.Label != "Rent" {
		t.Errorf("second row is %q, want Rent", e.Label)
	}
	m = press(t, m, "down", "down", "down") // past the end
	if _, e, _ := m.dash.currentEntry(); e.Label != "Utilities" {
		t.Error("the cursor ran off the bottom")
	}
	m = press(t, m, "up", "up", "up", "up")
	if _, e, _ := m.dash.currentEntry(); e.Label != "Salary" {
		t.Error("the cursor ran off the top")
	}
}

// An empty section still takes the cursor, so "add" always knows where to put
// things and there is no section picker to build.
func TestAddTargetsTheSectionTheCursorIsIn(t *testing.T) {
	s := testStore(t)
	m := app(t, s)

	m = press(t, m, "down") // onto the empty EXPENSES placeholder
	m = press(t, m, "a")
	mustContain(t, plain(m), "Add expense")

	m = press(t, m, typing("Rent")...)
	m = press(t, m, "tab")
	m = press(t, m, typing("2800")...)
	m = press(t, m, "enter")

	saved, err := s.Load(model.Today())
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Entries(model.Expense)) != 1 || len(saved.Entries(model.Income)) != 0 {
		t.Errorf("entry landed in the wrong section: %+v", saved)
	}
}

func TestDamagedMonthIsShownNotOverwritten(t *testing.T) {
	s := testStore(t)
	path := s.Path(model.Today())
	if err := writeFile(path, "{broken"); err != nil {
		t.Fatal(err)
	}

	m := app(t, s)
	mustContain(t, plain(m), "is damaged", "move it aside")
	checkWidth(t, m)

	// Editing keys must be inert here.
	m = press(t, m, "a", "e", "d", "r")
	if m.form != nil || m.confirm != nil {
		t.Error("a damaged month should not be editable")
	}
	if got, _ := readFile(path); got != "{broken" {
		t.Errorf("the damaged file was rewritten as %q", got)
	}

	// Navigating away still works.
	m = press(t, m, "right")
	if m.dash.loadErr != nil {
		t.Error("could not navigate off a damaged month")
	}
}

// Narrow terminals must still produce a frame that fits.
func TestNarrowTerminal(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Income, "A rather long income label that will not fit", money.FromLei(12500), true)
	seed.Add(model.Expense, "Rent", money.FromLei(2800), true)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	for _, width := range []int{40, 50, 62, 80, 120} {
		m := New(s)
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = next.(Model)
		for i, line := range strings.Split(m.View().Content, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("at width %d, line %d is %d cells:\n%q",
					width, i, w, stripANSI(line))
			}
		}
		if !strings.Contains(stripANSI(m.View().Content), "2.800,00") {
			t.Errorf("at width %d the amounts disappeared", width)
		}
	}
}

func TestAltScreen(t *testing.T) {
	m := app(t, testStore(t))
	if !m.View().AltScreen {
		t.Error("the app should take over the screen")
	}
}

// column returns the display column a substring starts at, counting cells
// rather than bytes so "↻" and "›" do not throw the count off.
func column(line, sub string) (int, bool) {
	i := strings.Index(line, sub)
	if i < 0 {
		return 0, false
	}
	return lipgloss.Width(line[:i]), true
}

// Amounts and recurring marks must sit in the same columns whether or not the
// row is selected, or the list visibly jitters as the cursor moves.
func TestColumnsLineUp(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Income, "Salary", money.FromLei(12500), true)
	seed.Add(model.Income, "Freelance", money.FromLei(2000), false)
	seed.Add(model.Expense, "Rent", money.FromLei(2800), true)
	seed.Add(model.Expense, "Utilities", money.FromLei(450), false)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	for _, width := range []int{44, 60, 90, 140} {
		m := New(s)
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = next.(Model)

		// Walk the cursor over every row so each one is rendered selected.
		for step := range 4 {
			lines := strings.Split(stripANSI(m.View().Content), "\n")

			amountEnds := map[int][]string{}
			markers := map[int][]string{}
			for _, amt := range []string{"12.500,00", "2.000,00", "2.800,00", "450,00"} {
				for _, line := range lines {
					if col, ok := column(line, amt); ok {
						end := col + lipgloss.Width(amt)
						amountEnds[end] = append(amountEnds[end], amt)
						if mcol, ok := column(line, "↻"); ok {
							markers[mcol] = append(markers[mcol], amt)
						}
						break
					}
				}
			}
			if len(amountEnds) != 1 {
				t.Errorf("width %d, cursor at %d: amounts end in %d different columns: %v",
					width, step, len(amountEnds), amountEnds)
			}
			if len(markers) > 1 {
				t.Errorf("width %d, cursor at %d: ↻ appears in %d different columns: %v",
					width, step, len(markers), markers)
			}
			m = press(t, m, "down")
		}
	}
}

// A terminal too small for the design must still render something coherent
// rather than a torn frame.
func TestVeryNarrowTerminal(t *testing.T) {
	s := testStore(t)
	seed := model.New(model.Today())
	seed.Add(model.Income, "Salary", money.FromLei(12500), true)
	seed.Add(model.Expense, "Rent", money.FromLei(2800), true)
	if err := s.Save(seed); err != nil {
		t.Fatal(err)
	}

	for _, size := range []struct{ w, h int }{{20, 10}, {24, 8}, {30, 20}, {12, 6}} {
		m := New(s)
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m = next.(Model)

		for _, screen := range []Model{m, press(t, m, "h"), press(t, m, "?"), press(t, m, "a")} {
			for i, line := range strings.Split(screen.View().Content, "\n") {
				if w := lipgloss.Width(line); w > size.w {
					t.Errorf("at %dx%d, line %d is %d cells:\n%q",
						size.w, size.h, i, w, stripANSI(line))
				}
			}
		}
	}
}
