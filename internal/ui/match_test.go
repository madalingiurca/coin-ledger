package ui

import (
	"strings"
	"testing"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// october with a planned mortgage and a planned rent transfer for the same
// amounts the invented ING statement carries.
func plannedOctober(t *testing.T) (*model.Month, string, string) {
	t.Helper()
	mo := model.New(october)
	mortgage := mo.Add(model.Expense, "Mortgage", money.Amount(215470), true)
	mo.SetCategory(mortgage, "Loan")
	rent := mo.Add(model.Expense, "Rent to Maria", money.FromLei(250), true)
	mo.Add(model.Expense, "Gym", money.FromLei(150), true) // nothing in the file matches
	return mo, mortgage, rent
}

func TestImportOffersToReplaceForecastEntries(t *testing.T) {
	importHome(t)
	s := testStore(t)
	mo, mortgage, rent := plannedOctober(t)
	saveMonth(t, s, mo)

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "2 look like a forecast", "Press enter to choose")

	m = press(t, m, "enter")
	if m.imp.stage != stageMatches {
		t.Fatalf("stage = %v", m.imp.stage)
	}
	screen := plain(m)
	mustContain(t, screen, "already planned", "2 matches", "Mortgage", "2.154,70", "Rata Credit · same amount",
		"Rent to Maria", "Maria Ionescu · same amount", "replace")
	if strings.Contains(screen, "Gym") {
		t.Errorf("an unmatched forecast is listed:\n%s", screen)
	}
	checkWidth(t, m)

	// The rent (booked first) is listed first: keep both; replace the mortgage.
	m = press(t, m, "space")
	mustContain(t, plain(m), "keep both")
	if s.Exists(october) {
		got, _ := s.Load(october)
		if got.HasActuals() {
			t.Fatal("choosing wrote the month")
		}
	}
	m = press(t, m, "enter")
	mustContain(t, plain(m), "Imported 10 entries into 1 month", "1 forecast replaced")

	got, _ := s.Load(october)
	e, _ := got.Find(model.Expense, mortgage)
	if e.IsForecast() || e.Planned != money.Amount(215470) || e.Label != "Mortgage" || e.Category != "Loan" || !e.Recurring {
		t.Errorf("mortgage = %+v", e)
	}
	r, _ := got.Find(model.Expense, rent)
	if !r.IsForecast() {
		t.Errorf("rent was replaced although kept: %+v", r)
	}
	// 3 forecasts + 8 bank expenses − 1 replaced in place.
	if len(got.Expenses) != 10 {
		t.Errorf("%d expenses, want 10", len(got.Expenses))
	}

	// The pairing is remembered.
	rules, _ := s.LoadRules()
	if label, ok := rules.LinkFor("RATA CREDIT"); !ok || label != "Mortgage" {
		t.Errorf("link = %q, %v", label, ok)
	}
	if _, ok := rules.LinkFor("MARIA IONESCU"); ok {
		t.Error("a kept pair was remembered")
	}
}

// Next month the remembered pairing finds the forecast even though the bank
// amount differs from the plan.
func TestRememberedPairingMatchesADifferentAmount(t *testing.T) {
	importHome(t)
	s := testStore(t)
	mo := model.New(october)
	mortgage := mo.Add(model.Expense, "Mortgage", money.FromLei(2300), true)
	saveMonth(t, s, mo)
	rules, _ := s.LoadRules()
	rules.SetLink("Rata Credit", "Mortgage")
	s.SaveRules(rules)

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter", "enter")
	mustContain(t, plain(m), "Mortgage", "replaced it before")
	m = press(t, m, "enter")

	got, _ := s.Load(october)
	e, _ := got.Find(model.Expense, mortgage)
	if e.Amount != money.Amount(215470) || e.Planned != money.FromLei(2300) {
		t.Errorf("mortgage = %+v", e)
	}
}

func TestMatchesBackGoesToPreview(t *testing.T) {
	importHome(t)
	s := testStore(t)
	mo, _, _ := plannedOctober(t)
	saveMonth(t, s, mo)
	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter", "enter", "esc")
	if m.imp.stage != stagePreview {
		t.Errorf("stage = %v", m.imp.stage)
	}
}

// One forecast entry is offered to one bank row only: two 5,40 rows and one
// 5,40 forecast make one pair.
func TestAForecastIsMatchedOnce(t *testing.T) {
	importHome(t)
	s := testStore(t)
	mo := model.New(october)
	mo.Add(model.Expense, "Vending", money.Amount(540), false)
	saveMonth(t, s, mo)
	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter")
	if n := len(m.imp.pairs()); n != 1 {
		t.Errorf("%d pairs, want 1", n)
	}
}

// A shop that sells several things: its transactions are filed one by one
// from the payees screen, each to its own category, with no rule saved.
func TestFilingAPayeeOneByOne(t *testing.T) {
	s := testStore(t)
	sept := model.New(october.Prev())
	sept.AddImported(model.Expense, bank("s1", "OMV", 300, ""))
	saveMonth(t, s, sept)
	mo := model.New(october)
	petrol, _ := mo.AddImported(model.Expense, model.Imported{Label: "OMV", Amount: money.FromLei(250), Date: "2026-10-20",
		Payee: "OMV", Source: model.SourceING, Key: "k1"})
	coffee, _ := mo.AddImported(model.Expense, model.Imported{Label: "OMV", Amount: money.FromLei(12), Date: "2026-10-05",
		Payee: "OMV", Source: model.SourceING, Key: "k2"})
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "u", "right")
	screen := plain(m)
	mustContain(t, screen, "OMV", "3 uncategorised", "20 Oct 2026", "05 Oct 2026", "14 Sep 2026")
	checkWidth(t, m)
	// Newest first: October 20, October 5, September.
	if d := m.payees.detail; d.rows[0].Entry.ID != petrol || d.rows[1].Entry.ID != coffee {
		t.Fatalf("order = %+v", d.rows)
	}

	m = press(t, m, "enter")
	if m.cat == nil || m.cat.applyAll {
		t.Fatal("the dialog should open with the payee rule off")
	}
	m = press(t, m, typing("Fuel")...)
	m = press(t, m, "enter")
	mustContain(t, plain(m), "OMV → Fuel", "2 uncategorised")

	// The cursor is now on the coffee. The dialog starts on Fuel, the one
	// category so far; Transfers and then "new" follow it.
	m = press(t, m, "enter", "down", "down")
	m = press(t, m, typing("Coffee")...)
	m = press(t, m, "enter")

	got, _ := s.Load(october)
	p, _ := got.Find(model.Expense, petrol)
	c, _ := got.Find(model.Expense, coffee)
	if p.Category != "Fuel" || c.Category != "Coffee" {
		t.Errorf("petrol %q, coffee %q", p.Category, c.Category)
	}
	rules, _ := s.LoadRules()
	if _, ok := rules.CategoryFor("OMV"); ok {
		t.Error("filing one by one saved a rule")
	}

	// The month on screen shows the change.
	if e, _ := m.dash.month.Find(model.Expense, coffee); e.Category != "Coffee" {
		t.Error("the month on screen is stale")
	}

	// Last one: September's. Then the payee has nothing left and the list
	// comes back.
	m = press(t, m, "enter", "enter") // files it under Coffee, first in the list
	if m.payees.detail != nil {
		t.Error("the payee is still open with nothing left to file")
	}
	mustContain(t, plain(m), "Every imported expense has a category")
	sp, _ := s.Load(october.Prev())
	if sp.Expenses[0].Category == "" {
		t.Error("September's OMV was not filed")
	}
}

func TestPayeeOneByOneEscGoesBack(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "OMV", 250, ""))
	saveMonth(t, s, mo)
	m := press(t, appOn(t, s, october, testW, testH), "u", "right", "esc")
	if m.screen != screenPayees || m.payees.detail != nil {
		t.Error("esc did not return to the payee list")
	}
}

// Typing a name while the list cursor is on an existing category starts a
// new one instead of being swallowed.
func TestCategoriseTypingStartsANewName(t *testing.T) {
	s := testStore(t)
	rules, _ := s.LoadRules()
	rules.AddCategory("Groceries")
	s.SaveRules(rules)
	mo := model.New(october)
	mo.AddImported(model.Expense, bank("k1", "Round Up", 2, ""))
	saveMonth(t, s, mo)

	m := appOn(t, s, october, testW, testH)
	m = press(t, m, "down", "c")
	if m.cat.onNew() {
		t.Fatal("the dialog should start on Groceries")
	}
	m = press(t, m, typing("Savings")...)
	m = press(t, m, "enter")
	if got := m.dash.month.Expenses[0].Category; got != "Savings" {
		t.Errorf("category = %q", got)
	}
}

// Re-filing a payee moves the entries its old rule filed, in every month.
func TestChangingAPayeeRuleMovesItsEntries(t *testing.T) {
	importHome(t)
	s := testStore(t)
	rules, _ := s.LoadRules()
	rules.SetRule("Round Up", model.Transfers)
	s.SaveRules(rules)

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter", "enter")
	got, _ := s.Load(october)
	var ru model.Entry
	for _, e := range got.Expenses {
		if e.Label == "Round Up" {
			ru = e
		}
	}
	if ru.Category != model.Transfers || !ru.Auto {
		t.Fatalf("imported round up = %+v", ru)
	}

	m.openMonth(october)
	m.selectEntry(model.Expense, ru.ID)
	m = press(t, m, "c")
	m = press(t, m, typing("Savings")...)
	m = press(t, m, "enter")
	got, _ = s.Load(october)
	e, _ := got.Find(model.Expense, ru.ID)
	if e.Category != "Savings" {
		t.Errorf("round up = %q", e.Category)
	}
	rules, _ = s.LoadRules()
	if c, _ := rules.CategoryFor("ROUND UP"); c != "Savings" {
		t.Errorf("rule = %q", c)
	}
}

// Revolut's top-up is the other side of an ING transfer: it is not income.
func TestRevolutTopUpIsNotIncome(t *testing.T) {
	importHome(t)
	s := testStore(t)
	m := press(t, app(t, s), "I")
	m = pick(t, m, "revolut.csv")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "income · 1", "15,00 lei", "1 transfer, not counted")
	m = press(t, m, "enter")
	got, _ := s.Load(october)
	if got.TotalIncome() != money.FromLei(15) {
		t.Errorf("income = %v", got.TotalIncome())
	}
	mustContain(t, plain(m), "Top-up by *1111 · transfer, not counted")
}

// A near amount starts as "keep both", and keeping both files the bank row in
// the plan's category, so the mortgage is not counted twice. "Unrelated"
// leaves the row on its own.
func TestKeepBothFilesTheRowWithThePlan(t *testing.T) {
	for _, tc := range []struct {
		presses  []string
		category string
	}{
		{nil, "Loan"},
		{[]string{"space"}, ""},
	} {
		importHome(t)
		s := testStore(t)
		mo := model.New(october)
		mortgage := mo.Add(model.Expense, "Mortgage", money.FromLei(2300), true)
		mo.SetCategory(mortgage, "Loan")
		saveMonth(t, s, mo)

		m := press(t, app(t, s), "I")
		m = pick(t, m, "ing.csv")
		m = press(t, m, "enter", "enter")
		mustContain(t, plain(m), "close amount", "keep both")
		m = press(t, m, tc.presses...)
		if tc.category == "" {
			mustContain(t, plain(m), "unrelated")
		}
		m = press(t, m, "enter")

		got, _ := s.Load(october)
		var bank model.Entry
		for _, e := range got.Expenses {
			if e.Payee == "RATA CREDIT" {
				bank = e
			}
		}
		if bank.Category != tc.category {
			t.Errorf("after %v the bank row is in %q, want %q", tc.presses, bank.Category, tc.category)
		}
		if e, _ := got.Find(model.Expense, mortgage); !e.IsForecast() {
			t.Errorf("the plan was replaced: %+v", e)
		}
	}
}
