package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
	"github.com/madalingiurca/budget/internal/store"
)

var october = model.Key{Year: 2026, Month: time.October}

func bank(key, payee string, lei int64, category string) model.Imported {
	return model.Imported{
		Label: payee, Amount: money.FromLei(lei), Date: "2026-10-14",
		Payee: model.NormalizePayee(payee), Source: model.SourceING, Key: key, Category: category,
	}
}

// appOn opens the app on a specific month at a given terminal size.
func appOn(t *testing.T, s *store.Store, k model.Key, w, h int) Model {
	t.Helper()
	m := New(s)
	m.openMonth(k)
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func saveMonth(t *testing.T, s *store.Store, m *model.Month) {
	t.Helper()
	if err := s.Save(m); err != nil {
		t.Fatal(err)
	}
}

// A month where nothing has a category must look exactly as it always did:
// no headers, no "spent so far".
func TestPlainMonthHasNoCategoryHeaders(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.Add(model.Income, "Salary", money.FromLei(10000), true)
	mo.Add(model.Expense, "Rent", money.FromLei(2800), true)
	saveMonth(t, s, mo)

	screen := plain(appOn(t, s, october, testW, testH))
	for _, unwanted := range []string{"Uncategorised", "▾", "spent so far"} {
		if strings.Contains(screen, unwanted) {
			t.Errorf("a plain month shows %q:\n%s", unwanted, screen)
		}
	}
}

func TestExpensesAreGroupedByCategory(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.Add(model.Income, "Salary", money.FromLei(10000), true)
	id := mo.Add(model.Expense, "Groceries budget", money.FromLei(1500), true)
	mo.SetCategory(id, "Groceries")
	mo.AddImported(model.Expense, bank("k1", "Lidl", 400, "Groceries"))
	mo.AddImported(model.Expense, bank("k2", "Bolt", 25, ""))
	mo.AddImported(model.Expense, bank("k3", "Revolut**1234*", 500, model.Transfers))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	screen := plain(m)
	mustContain(t, screen,
		"▾ Uncategorised (1)",
		"▾ Groceries (2)", "400,00 of 1.500,00",
		"▾ Transfers (1)", "not counted 500,00",
		"Wed 14 Oct", "Lidl", "spent so far", "425,00 lei",
	)
	// Projected: groceries count their forecast, Bolt adds, the transfer does not.
	mustContain(t, screen, "8.475,00 lei")
	checkWidth(t, m)

	// Uncategorised comes first, Transfers last.
	if u, g, tr := strings.Index(screen, "Uncategorised"), strings.Index(screen, "Groceries ("), strings.Index(screen, "Transfers"); !(u < g && g < tr) {
		t.Errorf("category order is wrong:\n%s", screen)
	}
}

func TestFoldingACategory(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Lidl", 400, "Groceries"))
	mo.AddImported(model.Expense, bank("k2", "Bolt", 25, "Transport"))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down") // income placeholder → Groceries header
	if r, _ := m.dash.current(); !r.header || r.cat != "Groceries" {
		t.Fatalf("cursor is on %+v, want the Groceries header", r)
	}
	m = press(t, m, "enter")
	screen := plain(m)
	if strings.Contains(screen, "Lidl") || !strings.Contains(screen, "▸ Groceries (1)") {
		t.Errorf("Groceries did not fold:\n%s", screen)
	}
	mustContain(t, screen, "Bolt")
	if m.form != nil {
		t.Error("enter on a header opened the edit form")
	}

	// Folding survives a trip to another month and back.
	m = press(t, m, "right", "left")
	if strings.Contains(plain(m), "Lidl") {
		t.Error("the category unfolded while paging months")
	}

	m = press(t, m, "down", "space")
	mustContain(t, plain(m), "Lidl", "▾ Groceries (1)")
}

func TestFormSetsCategoryOnAnExpense(t *testing.T) {
	s := testStore(t)
	m := app(t, s)
	m = press(t, m, "x")
	m = press(t, m, typing("Gym")...)
	m = press(t, m, "tab")
	m = press(t, m, typing("150")...)
	m = press(t, m, "tab")
	mustContain(t, plain(m), "Category")
	m = press(t, m, typing("Health")...)
	m = press(t, m, "enter")

	e := m.dash.month.Expenses[0]
	if e.Label != "Gym" || e.Category != "Health" {
		t.Fatalf("entry = %+v", e)
	}
	if _, got, ok := m.dash.currentEntry(); !ok || got.ID != e.ID {
		t.Error("the cursor is not on the new entry")
	}
	mustContain(t, plain(m), "▾ Health (1)")

	// The new category is remembered and offered next time, whatever its case.
	rules, err := s.LoadRules()
	if err != nil || !strings.Contains(strings.Join(rules.Categories, "|"), "Health") {
		t.Fatalf("categories = %v, %v", rules.Categories, err)
	}
	m = press(t, m, "x")
	m = press(t, m, typing("Pool")...)
	m = press(t, m, "tab")
	m = press(t, m, typing("50")...)
	m = press(t, m, "tab")
	m = press(t, m, typing("health")...)
	m = press(t, m, "enter")
	if got := m.dash.month.Expenses[1].Category; got != "Health" {
		t.Errorf("category = %q, want the existing spelling", got)
	}
}

func TestIncomeFormHasNoCategory(t *testing.T) {
	m := press(t, app(t, testStore(t)), "i")
	if strings.Contains(plain(m), "Category") {
		t.Error("the income form offers a category")
	}
	// Tab still cycles label → amount → recurring → label.
	m = press(t, m, "tab", "tab", "tab")
	if m.form.focus != fieldLabel {
		t.Errorf("focus = %d after a full cycle", m.form.focus)
	}
}

func TestAddFromInsideACategoryPrefillsIt(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Lidl", 400, "Groceries"))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down", "a") // on the Groceries header
	if m.form == nil || m.form.kind != model.Expense || m.form.category.Value() != "Groceries" {
		t.Fatalf("form = %+v", m.form)
	}
}

// The heart of the feature: categorise one Bolt ride and every Bolt ride, in
// every month, follows; rides filed by hand stay where they were put.
func TestCategorisingAPayeeFilesItEverywhere(t *testing.T) {
	s := testStore(t)
	sept := model.New(october.Prev())
	sept.AddImported(model.Expense, bank("s1", "Bolt", 30, ""))
	sept.AddImported(model.Expense, bank("s2", "Bolt", 40, "Work travel"))
	saveMonth(t, s, sept)

	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Bolt", 25, ""))
	mo.AddImported(model.Expense, bank("k2", "Bolt", 35, ""))
	mo.AddImported(model.Expense, bank("k3", "Lidl", 400, ""))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down", "c") // first Bolt ride
	if m.cat == nil {
		t.Fatal("c did not open the categorise dialog")
	}
	screen := plain(m)
	mustContain(t, screen, "Categorise Bolt", "new category", "Match", "BOLT", "Every expense whose payee contains this")
	checkWidth(t, m)

	m = press(t, m, typing("Transport")...)
	m = press(t, m, "enter")
	if m.cat != nil {
		t.Fatalf("dialog still open: %q", m.cat.err)
	}

	got := []string{}
	for _, e := range m.dash.month.Expenses {
		got = append(got, e.Category)
	}
	if strings.Join(got, "|") != "Transport|Transport|" {
		t.Errorf("October categories = %q", got)
	}
	if _, e, ok := m.dash.currentEntry(); !ok || e.Key != "k1" {
		t.Errorf("the cursor left the entry: %+v", e)
	}
	mustContain(t, plain(m), "Bolt → Transport", "2 more entries filed")

	other, _ := s.Load(october.Prev())
	if other.Expenses[0].Category != "Transport" || other.Expenses[1].Category != "Work travel" {
		t.Errorf("September = %q / %q", other.Expenses[0].Category, other.Expenses[1].Category)
	}

	rules, _ := s.LoadRules()
	if c, ok := rules.CategoryFor("BOLT.EU"); !ok || c != "Transport" {
		t.Errorf("the rule was not saved: %q, %v", c, ok)
	}
}

func TestCategoriseOnlyThisEntry(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Bolt", 25, ""))
	mo.AddImported(model.Expense, bank("k2", "Bolt", 35, ""))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down", "c")
	m = press(t, m, typing("Transport")...)
	m = press(t, m, "tab", "tab", "space", "enter") // untick "every expense"

	if a, b := m.dash.month.Expenses[0].Category, m.dash.month.Expenses[1].Category; a != "Transport" || b != "" {
		t.Errorf("categories = %q / %q", a, b)
	}
	rules, _ := s.LoadRules()
	if _, ok := rules.CategoryFor("BOLT"); ok {
		t.Error("a rule was saved although it was switched off")
	}
}

func TestCategoriseWithAShorterMatch(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Carrefour Lujerului", 100, ""))
	mo.AddImported(model.Expense, bank("k2", "Carrefour Orhideea", 200, ""))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down", "c")
	m = press(t, m, typing("Groceries")...)
	m = press(t, m, "tab")
	for range len(" LUJERULUI") {
		m = press(t, m, "backspace")
	}
	m = press(t, m, "enter")
	if m.cat != nil {
		t.Fatalf("dialog still open: %q", m.cat.err)
	}
	if a, b := m.dash.month.Expenses[0].Category, m.dash.month.Expenses[1].Category; a != "Groceries" || b != "Groceries" {
		t.Errorf("categories = %q / %q", a, b)
	}
}

func TestCategoriseRejectsBadInput(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Bolt", 25, ""))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down", "c", "enter") // on "new" with nothing typed
	if m.cat == nil || !strings.Contains(plain(m), "Type a name") {
		t.Fatalf("an empty new category was accepted:\n%s", plain(m))
	}

	m = press(t, m, typing("Transport")...)
	m = press(t, m, "tab")
	m = press(t, m, typing("x")...) // BOLTX is not in BOLT
	m = press(t, m, "enter")
	if m.cat == nil || !strings.Contains(plain(m), "does not contain") {
		t.Fatalf("a match the payee does not contain was accepted:\n%s", plain(m))
	}

	m = press(t, m, "esc")
	if m.cat != nil || m.dash.month.Expenses[0].Category != "" {
		t.Error("esc did not abandon the dialog cleanly")
	}
}

// A hand-typed expense has no payee, so there is nothing to remember.
func TestCategoriseAForecastEntry(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.Add(model.Expense, "Rent", money.FromLei(2800), true)
	saveMonth(t, s, mo)
	rules, _ := s.LoadRules()
	rules.AddCategory("Housing")
	s.SaveRules(rules)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down", "c")
	screen := plain(m)
	if strings.Contains(screen, "Match") {
		t.Errorf("a forecast entry was offered a payee rule:\n%s", screen)
	}
	m = press(t, m, "enter") // cursor starts on Housing, the first real category
	if got := m.dash.month.Expenses[0].Category; got != "Housing" {
		t.Errorf("category = %q", got)
	}
}

func TestCategoriseIncomeIsRefused(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.Add(model.Income, "Salary", money.FromLei(100), true)
	saveMonth(t, s, mo)
	m := press(t, appOn(t, s, october, testW, testH), "c")
	if m.cat != nil {
		t.Error("income was offered a category")
	}
	mustContain(t, plain(m), "Only expenses have categories")
}

func TestUncategorisedPayeesScreen(t *testing.T) {
	s := testStore(t)
	sept := model.New(october.Prev())
	sept.AddImported(model.Expense, bank("s1", "Bolt", 30, ""))
	saveMonth(t, s, sept)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Bolt", 25, ""))
	mo.AddImported(model.Expense, bank("k2", "Lidl", 400, ""))
	mo.AddImported(model.Expense, bank("k3", "Kaufland", 90, "Groceries"))
	mo.Add(model.Expense, "Rent", money.FromLei(2800), true) // no payee: not listed
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "u")
	screen := plain(m)
	mustContain(t, screen, "uncategorised", "2 payees", "Lidl", "400,00", "Bolt", "55,00")
	for _, unwanted := range []string{"Kaufland", "Rent"} {
		if strings.Contains(screen, unwanted) {
			t.Errorf("%q should not be listed:\n%s", unwanted, screen)
		}
	}
	if strings.Index(screen, "Lidl") > strings.Index(screen, "Bolt") {
		t.Errorf("payees are not ordered by total:\n%s", screen)
	}
	checkWidth(t, m)

	// Categorise Bolt (second row) from here.
	m = press(t, m, "down", "enter")
	if m.cat == nil {
		t.Fatal("enter did not open the categorise dialog")
	}
	mustContain(t, plain(m), "Categorise Bolt", "Match")
	m = press(t, m, typing("Transport")...)
	m = press(t, m, "enter")

	screen = plain(m)
	mustContain(t, screen, "1 payee", "Lidl", "2 entries filed")
	for _, r := range m.payees.rows {
		if r.Payee == "BOLT" {
			t.Errorf("Bolt is still listed:\n%s", screen)
		}
	}
	for _, k := range []model.Key{october.Prev(), october} {
		got, _ := s.Load(k)
		if got.Expenses[0].Category != "Transport" {
			t.Errorf("%s: Bolt is in %q", k, got.Expenses[0].Category)
		}
	}

	// Back on the month, the change is visible without reopening it.
	m = press(t, m, "esc")
	mustContain(t, plain(m), "▾ Transport (1)")
}

func TestUncategorisedPayeesWhenThereAreNone(t *testing.T) {
	m := press(t, app(t, testStore(t)), "u")
	mustContain(t, plain(m), "Every imported expense has a category")
	m = press(t, m, "enter", "down", "up") // nothing to act on, nothing to crash
	if m.cat != nil {
		t.Error("a dialog opened with no payee selected")
	}
	m = press(t, m, "u")
	if m.screen != screenMonth {
		t.Error("u did not return to the month")
	}
}

// A month with more rows than the terminal has lines must scroll: the title,
// the totals and the selected row all stay on screen.
func TestLongMonthScrollsWithTheCursor(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.Add(model.Income, "Salary", money.FromLei(10000), true)
	for i := range 80 {
		mo.AddImported(model.Expense, bank("k"+itoa(i), "Shop "+itoa(i), 10, ""))
	}
	saveMonth(t, s, mo)

	const h = 30
	m := appOn(t, s, october, testW, h)
	for _, moves := range []int{0, 1, 40, 200} {
		for range moves {
			m = press(t, m, "down")
		}
		screen := plain(m)
		if got := strings.Count(strings.TrimRight(screen, "\n"), "\n") + 1; got > h {
			t.Fatalf("after %d moves the screen is %d lines tall, terminal has %d", moves, got, h)
		}
		mustContain(t, screen, "October 2026", "LEFT", "quit")
		if !strings.Contains(screen, "› ") {
			t.Fatalf("after %d moves the selected row is off screen:\n%s", moves, screen)
		}
		checkWidth(t, m)
	}
	mustContain(t, plain(m), "Shop 79", "more")

	// The form still fits under a scrolled list.
	m = press(t, m, "e")
	if got := strings.Count(strings.TrimRight(plain(m), "\n"), "\n") + 1; got > h {
		t.Errorf("with the form open the screen is %d lines tall", got)
	}
}

func TestDamagedCategoriesFileIsNotOverwritten(t *testing.T) {
	s := testStore(t)
	if err := writeFile(s.RulesPath(), "{broken"); err != nil {
		t.Fatal(err)
	}
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Bolt", 25, ""))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	mustContain(t, plain(m), "damaged")
	m = press(t, m, "down", "c")
	m = press(t, m, typing("Transport")...)
	m = press(t, m, "enter")

	if got, _ := readFile(s.RulesPath()); got != "{broken" {
		t.Errorf("the damaged categories file was overwritten: %q", got)
	}
	// The entry itself is still filed; only the remembering is refused.
	if got := m.dash.month.Expenses[0].Category; got != "Transport" {
		t.Errorf("category = %q", got)
	}
}

// Within a category, bank rows sit under a heading per day with that day's
// total, and the plan under "Planned".
func TestDaysAreBucketed(t *testing.T) {
	mo := model.New(october)
	plan := mo.Add(model.Expense, "Groceries budget", money.FromLei(500), true)
	mo.SetCategory(plan, "Food")
	for _, r := range []struct{ label, date string }{{"Lidl", "2026-10-11"}, {"Kifla", "2026-10-02"}, {"Penny", "2026-10-11"}} {
		mo.AddImported(model.Expense, model.Imported{Label: r.label, Amount: money.FromLei(10), Date: r.date,
			Payee: strings.ToUpper(r.label), Source: model.SourceING, Key: r.label, Category: "Food"})
	}
	s := testStore(t)
	saveMonth(t, s, mo)
	screen := plain(appOn(t, s, october, testW, testH))
	at := func(sub string) int { return strings.Index(screen, sub) }
	order := []string{"Planned", "Groceries budget", "Fri 02 Oct", "Kifla", "Sun 11 Oct", "Lidl", "Penny"}
	last := -1
	for _, sub := range order {
		i := at(sub)
		if i <= last {
			t.Fatalf("%q is out of place in:\n%s", sub, screen)
		}
		last = i
	}
	if strings.Count(screen, "Sun 11 Oct") != 1 || !strings.Contains(screen, "20,00") {
		t.Errorf("the day appears once with its total 20,00:\n%s", screen)
	}
}
