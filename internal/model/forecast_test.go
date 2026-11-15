package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/madalingiurca/budget/internal/money"
)

func lei(n int64) money.Amount { return money.FromLei(n) }

func bankRow(key, payee string, amount money.Amount, category string) Imported {
	return Imported{
		Label: payee, Amount: amount, Date: "2026-10-02", Payee: NormalizePayee(payee),
		Source: SourceING, Key: key, Category: category,
	}
}

// A month nobody has imported into must total exactly as it did before
// forecasts and actuals existed.
func TestForecastOnlyMonthTotalsAsBefore(t *testing.T) {
	m := sample()
	var income, expenses money.Amount
	for _, e := range m.Income {
		income += e.Amount
	}
	for _, e := range m.Expenses {
		expenses += e.Amount
	}
	if m.TotalIncome() != income || m.TotalExpenses() != expenses || m.Left() != income-expenses {
		t.Errorf("totals = %v / %v / %v", m.TotalIncome(), m.TotalExpenses(), m.Left())
	}
	if m.HasActuals() || m.Spent() != 0 {
		t.Errorf("a forecast-only month has actuals: spent %v", m.Spent())
	}
}

func TestCategoryCountsTheLargerOfForecastAndActual(t *testing.T) {
	m := New(Key{2026, time.October})
	id := m.Add(Expense, "Groceries", lei(1500), true)
	if err := m.SetCategory(id, "Groceries"); err != nil {
		t.Fatal(err)
	}
	m.AddImported(Expense, bankRow("k1", "Carrefour", lei(400), "Groceries"))
	m.AddImported(Expense, bankRow("k2", "Kaufland", lei(300), "Groceries"))

	if got := m.TotalExpenses(); got != lei(1500) {
		t.Errorf("under forecast: projected %v, want the forecast", got)
	}
	if got := m.Spent(); got != lei(700) {
		t.Errorf("spent = %v", got)
	}

	m.AddImported(Expense, bankRow("k3", "Mega Image", lei(1000), "Groceries"))
	if got := m.TotalExpenses(); got != lei(1700) {
		t.Errorf("over forecast: projected %v, want the actual", got)
	}

	cats := m.Categories()
	if len(cats) != 1 || cats[0].Name != "Groceries" || cats[0].Forecast != lei(1500) ||
		cats[0].Actual != lei(1700) || cats[0].Count != 4 {
		t.Errorf("categories = %+v", cats)
	}
}

// Uncategorised entries have nothing in common, so a forecast there must not
// swallow unrelated bank rows.
func TestUncategorisedForecastAndActualAddUp(t *testing.T) {
	m := New(Key{2026, time.October})
	m.Add(Expense, "Rent", lei(2800), true)
	m.AddImported(Expense, bankRow("k1", "Some shop", lei(100), ""))
	if got := m.TotalExpenses(); got != lei(2900) {
		t.Errorf("projected = %v, want 2.900", got)
	}
}

func TestTransfersAreNeverCounted(t *testing.T) {
	m := New(Key{2026, time.October})
	m.Add(Income, "Salary", lei(10000), true)
	m.AddImported(Expense, bankRow("k1", "Revolut**1234*", lei(500), Transfers))
	m.AddImported(Expense, bankRow("k2", "Round Up", lei(5), "Savings"))

	if got := m.TotalExpenses(); got != lei(5) {
		t.Errorf("projected expenses = %v, want only the savings", got)
	}
	if got := m.Spent(); got != lei(5) {
		t.Errorf("spent = %v", got)
	}
	if got := m.Left(); got != lei(9995) {
		t.Errorf("left = %v", got)
	}
	cats := m.Categories()
	if len(cats) != 2 || cats[1].Name != Transfers || cats[1].Projected != 0 || cats[1].Actual != lei(500) {
		t.Errorf("categories = %+v", cats)
	}
}

func TestCategoriesOrder(t *testing.T) {
	m := New(Key{2026, time.October})
	for _, c := range []string{"transport", Transfers, "", "Groceries"} {
		id := m.Add(Expense, "x", lei(1), false)
		m.SetCategory(id, c)
	}
	var got []string
	for _, c := range m.Categories() {
		got = append(got, c.Name)
	}
	if want := []string{"", "Groceries", "transport", Transfers}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("order = %q, want %q", got, want)
	}
	if n := len(m.InCategory("Groceries")); n != 1 {
		t.Errorf("InCategory = %d entries", n)
	}
}

func TestIncomeCountsTheLargerOfForecastAndActual(t *testing.T) {
	m := New(Key{2026, time.October})
	m.Add(Income, "Salary", lei(12000), true)
	m.AddImported(Income, bankRow("k1", "Employer", lei(12500), ""))
	if got := m.TotalIncome(); got != lei(12500) {
		t.Errorf("income = %v", got)
	}
	if got := m.ActualTotal(Income); got != lei(12500) {
		t.Errorf("actual income = %v", got)
	}
	if got := m.ForecastTotal(Income); got != lei(12000) {
		t.Errorf("forecast income = %v", got)
	}
}

func TestAddImportedSkipsKnownRows(t *testing.T) {
	m := New(Key{2026, time.October})
	id, added := m.AddImported(Expense, bankRow("k1", "Bolt", lei(20), "Transport"))
	if !added || id == "" {
		t.Fatal("first import was refused")
	}
	if _, added := m.AddImported(Expense, bankRow("k1", "Bolt", lei(20), "Transport")); added {
		t.Error("the same bank row was imported twice")
	}
	if len(m.Expenses) != 1 {
		t.Errorf("%d expenses", len(m.Expenses))
	}
	e := m.Expenses[0]
	if e.IsForecast() || e.Actual() != lei(20) || e.Forecast() != 0 || e.Payee != "BOLT" || e.Category != "Transport" {
		t.Errorf("entry = %+v", e)
	}
}

func TestImportedIncomeHasNoCategory(t *testing.T) {
	m := New(Key{2026, time.October})
	m.AddImported(Income, bankRow("k1", "Employer", lei(100), "Whatever"))
	if m.Income[0].Category != "" {
		t.Errorf("income got category %q", m.Income[0].Category)
	}
}

// Deleting an imported row must stick: the next import of the same statement
// is not allowed to resurrect it.
func TestDeletedBankRowStaysDeleted(t *testing.T) {
	m := New(Key{2026, time.October})
	id, _ := m.AddImported(Expense, bankRow("k1", "Bolt", lei(20), ""))
	if err := m.Remove(Expense, id); err != nil {
		t.Fatal(err)
	}
	if !m.HasKey("k1") {
		t.Fatal("deleted key was forgotten")
	}
	if _, added := m.AddImported(Expense, bankRow("k1", "Bolt", lei(20), "")); added {
		t.Error("deleted bank row came back")
	}

	// Deleting a forecast entry records nothing.
	fid := m.Add(Expense, "Rent", lei(1), false)
	m.Remove(Expense, fid)
	if len(m.SkippedKeys) != 1 {
		t.Errorf("skipped keys = %v", m.SkippedKeys)
	}
	if m.HasKey("") {
		t.Error("the empty key must never match")
	}
}

func TestReplaceForecast(t *testing.T) {
	m := New(Key{2026, time.October})
	id := m.Add(Expense, "Mortgage", lei(2300), true)
	m.SetCategory(id, "Loan")

	imp := bankRow("k1", "Rata Credit", money.Amount(215470), "Other")
	if err := m.ReplaceForecast(Expense, id, imp); err != nil {
		t.Fatal(err)
	}
	if len(m.Expenses) != 1 {
		t.Fatalf("%d expenses, want the one entry replaced in place", len(m.Expenses))
	}
	e := m.Expenses[0]
	if e.ID != id || e.Label != "Mortgage" || !e.Recurring || e.Category != "Loan" {
		t.Errorf("identity was not kept: %+v", e)
	}
	if e.Amount != money.Amount(215470) || e.Planned != lei(2300) || e.Key != "k1" || e.Source != SourceING ||
		e.Payee != "RATA CREDIT" || e.Date != "2026-10-02" {
		t.Errorf("bank data was not taken: %+v", e)
	}
	if e.IsForecast() || e.Forecast() != lei(2300) || e.Actual() != money.Amount(215470) {
		t.Errorf("forecast/actual = %v / %v", e.Forecast(), e.Actual())
	}
	// Planned 2.300 against actual 2.154,70: the category projects the larger.
	if got := m.TotalExpenses(); got != lei(2300) {
		t.Errorf("projected = %v", got)
	}

	if err := m.ReplaceForecast(Expense, id, bankRow("k2", "Rata Credit", lei(1), "")); err == nil {
		t.Error("an actual entry was replaced a second time")
	}
	other := m.Add(Expense, "Other", lei(1), false)
	if err := m.ReplaceForecast(Expense, other, imp); err == nil {
		t.Error("a bank row already in the month replaced a second forecast")
	}
	if err := m.ReplaceForecast(Expense, "nope", bankRow("k3", "x", lei(1), "")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing entry: %v", err)
	}
}

func TestReplaceForecastTakesCategoryWhenItHadNone(t *testing.T) {
	m := New(Key{2026, time.October})
	id := m.Add(Expense, "Gym", lei(150), true)
	if err := m.ReplaceForecast(Expense, id, bankRow("k1", "World Class", lei(150), "Health")); err != nil {
		t.Fatal(err)
	}
	if m.Expenses[0].Category != "Health" {
		t.Errorf("category = %q", m.Expenses[0].Category)
	}
}

func TestSeedCarriesThePlanNotTheBankRow(t *testing.T) {
	prev := New(Key{2026, time.October})
	id := prev.Add(Expense, "Mortgage", lei(2300), true)
	prev.SetCategory(id, "Loan")
	prev.ReplaceForecast(Expense, id, bankRow("k1", "Rata Credit", money.Amount(215470), ""))
	prev.AddImported(Expense, bankRow("k2", "Bolt", lei(20), "Transport")) // not recurring

	next := SeedFrom(prev, Key{2026, time.November})
	if len(next.Expenses) != 1 {
		t.Fatalf("seeded %d expenses, want 1", len(next.Expenses))
	}
	e := next.Expenses[0]
	if e.Label != "Mortgage" || e.Amount != lei(2300) || e.Category != "Loan" || !e.Recurring {
		t.Errorf("seeded entry = %+v", e)
	}
	if !e.IsForecast() || e.Planned != 0 || e.Key != "" || e.Date != "" || e.Payee != "" {
		t.Errorf("seeded entry still carries bank data: %+v", e)
	}
	if next.HasActuals() || len(next.SkippedKeys) != 0 {
		t.Error("a seeded month must start as pure forecast")
	}
}

func TestSetCategory(t *testing.T) {
	m := New(Key{2026, time.October})
	id := m.Add(Expense, "Bolt", lei(20), false)
	if err := m.SetCategory(id, "  Transport "); err != nil || m.Expenses[0].Category != "Transport" {
		t.Errorf("category = %q, %v", m.Expenses[0].Category, err)
	}
	if err := m.SetCategory("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing entry: %v", err)
	}
}

// A forecast entry writes none of the import fields, so hand-typed months stay
// as short and readable as they always were.
func TestForecastEntryJSONHasNoImportFields(t *testing.T) {
	b, err := json.Marshal(Entry{ID: "a", Label: "Rent", Amount: lei(2800), Recurring: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"category", "planned", "date", "payee", "source", "key"} {
		if strings.Contains(string(b), `"`+field+`"`) {
			t.Errorf("forecast entry wrote %q: %s", field, b)
		}
	}
}

func TestForecastMatches(t *testing.T) {
	m := New(Key{2026, time.October})
	mortgage := m.Add(Expense, "Mortgage", lei(2300), true)
	gym := m.Add(Expense, "Gym", lei(150), true)
	m.AddImported(Expense, bankRow("k0", "Old row", lei(150), "")) // an actual is never a candidate

	rules := NewRules()

	// Same amount, nothing remembered yet.
	got := m.ForecastMatches(Expense, bankRow("k1", "World Class", lei(150), ""), rules)
	if len(got) != 1 || got[0].Entry.ID != gym || got[0].Reason != MatchAmount {
		t.Errorf("amount match = %+v", got)
	}

	// Different amount and no link: no candidate.
	imp := bankRow("k2", "Rata Credit", money.Amount(215470), "")
	if got := m.ForecastMatches(Expense, imp, rules); len(got) != 0 {
		t.Errorf("unexpected match = %+v", got)
	}

	// Once linked, the payee finds its forecast whatever the amount.
	rules.SetLink("Rata Credit", "Mortgage")
	got = m.ForecastMatches(Expense, imp, rules)
	if len(got) != 1 || got[0].Entry.ID != mortgage || got[0].Reason != MatchLinked {
		t.Errorf("linked match = %+v", got)
	}

	// A linked candidate comes before an amount candidate.
	m.Add(Expense, "Something else", money.Amount(215470), false)
	got = m.ForecastMatches(Expense, imp, rules)
	if len(got) != 2 || got[0].Reason != MatchLinked || got[1].Reason != MatchAmount {
		t.Errorf("order = %+v", got)
	}

	if got := m.ForecastMatches(Income, imp, nil); len(got) != 0 {
		t.Errorf("income matched an expense: %+v", got)
	}
}
