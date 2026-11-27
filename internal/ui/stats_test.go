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

var statsMonth = model.Key{Year: 2026, Month: time.October}

// statsFixture is a month with plans, bank rows that replace or spend them,
// rows of one payee on several days, a transfer and two kinds of income.
func statsFixture(t *testing.T, s *store.Store) *model.Month {
	t.Helper()
	mo := model.New(statsMonth)
	mo.Add(model.Income, "Salary", money.FromLei(12500), true)
	mo.Add(model.Income, "Freelance", money.FromLei(1500), false)
	for _, f := range []struct {
		label, cat string
		lei        int64
	}{{"Mortgage", "Loan", 2300}, {"Groceries budget", "Groceries", 1500}, {"Gym", "Sport", 200}} {
		id := mo.Add(model.Expense, f.label, money.FromLei(f.lei), true)
		if err := mo.SetCategory(id, f.cat); err != nil {
			t.Fatal(err)
		}
	}
	row := func(key, payee, date string, lei int64, cat string) model.Imported {
		return model.Imported{Label: payee, Amount: money.FromLei(lei), Date: date,
			Payee: model.NormalizePayee(payee), Source: model.SourceING, Key: key, Category: cat}
	}
	// The bank's mortgage rate is less than planned: Loan counts the plan.
	if err := mo.ReplaceForecast(model.Expense, mo.Expenses[0].ID, row("p1", "Rata Credit", "2026-10-02", 2154, "")); err != nil {
		t.Fatal(err)
	}
	// Groceries spent past the budget: the shop rows count, not the budget.
	mo.AddImported(model.Expense, row("p2", "Lidl", "2026-10-03", 900, "Groceries"))
	mo.AddImported(model.Expense, row("p3", "Mega Image", "2026-10-11", 800, "Groceries"))
	mo.AddImported(model.Expense, row("p4", "Bolt", "2026-10-05", 27, ""))
	mo.AddImported(model.Expense, row("p5", "BOLT", "2026-10-20", 31, ""))
	mo.AddImported(model.Expense, row("p6", "Revolut**1234*", "2026-10-06", 500, model.Transfers))
	mo.AddImported(model.Income, row("p7", "Employer SRL", "2026-10-30", 300, ""))
	if err := s.Save(mo); err != nil {
		t.Fatal(err)
	}
	return mo
}

func statsApp(t *testing.T) (Model, *model.Month) {
	t.Helper()
	s := testStore(t)
	mo := statsFixture(t, s)
	m := New(s)
	m.openMonth(statsMonth)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(Model), mo
}

// The breakdown by label adds up to the month's expenses, the same figure as
// by category, so the two views never disagree about what was spent.
func TestCountedExpensesAddUpToTheTotal(t *testing.T) {
	mo := statsFixture(t, testStore(t))
	var sum money.Amount
	for _, c := range countedExpenses(mo) {
		sum += c.amount
	}
	if want := mo.Total(model.Expense); sum != want {
		t.Errorf("counted expenses add up to %s, total is %s", sum, want)
	}
}

func TestStatsCycleWithS(t *testing.T) {
	m, _ := statsApp(t)
	mustContain(t, plain(m), "BY CATEGORY", "1/2 s")

	m = press(t, m, "s")
	screen := plain(m)
	mustContain(t, screen, "BY LABEL", "2/2 s")
	if strings.Contains(screen, "BY CATEGORY") {
		t.Errorf("both views on screen:\n%s", screen)
	}
	mustContain(t, plain(press(t, m, "s")), "BY CATEGORY")
	mustContain(t, plain(press(t, m, "S")), "BY CATEGORY")
	mustContain(t, plain(press(t, m, "S", "S")), "BY LABEL")

	// The choice stays while paging through months.
	mustContain(t, plain(press(t, m, "]")), "BY LABEL")
	checkWidth(t, m)
}

func TestByLabelAddsUpOnePayee(t *testing.T) {
	m, _ := statsApp(t)
	b := press(t, m, "s").byLabel()
	got := map[string]money.Amount{}
	for _, it := range b.items {
		got[it.name] = it.amount
	}
	if got["Bolt"] != money.FromLei(58) {
		t.Errorf("Bolt = %s, want both rows, 58,00: %v", got["Bolt"], got)
	}
	if _, ok := got["Groceries budget"]; ok {
		t.Errorf("an overspent budget is counted beside the rows that spent it: %v", got)
	}
	if got["Mortgage"] != money.FromLei(2300) {
		t.Errorf("a bank row under its plan counts for the plan, got %s", got["Mortgage"])
	}
	if _, ok := got["Revolut**1234*"]; ok {
		t.Errorf("a transfer is counted: %v", got)
	}
	if b.items[0].name != "Mortgage" {
		t.Errorf("largest is not first: %v", b.items)
	}
}
