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
	mustContain(t, plain(m), "BY CATEGORY", "1/6 s")

	m = press(t, m, "s")
	screen := plain(m)
	mustContain(t, screen, "BY LABEL", "2/6 s")
	if strings.Contains(screen, "BY CATEGORY") {
		t.Errorf("both views on screen:\n%s", screen)
	}
	mustContain(t, plain(press(t, m, "s")), "LARGEST")
	mustContain(t, plain(press(t, m, "S")), "BY CATEGORY")
	mustContain(t, plain(press(t, m, "S", "S")), "VS SEPTEMBER")

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

func TestEveryViewDraws(t *testing.T) {
	m, _ := statsApp(t)
	for _, want := range []string{"BY CATEGORY", "BY LABEL", "LARGEST", "BY WEEK", "INCOME", "VS SEPTEMBER"} {
		mustContain(t, plain(m), want)
		checkWidth(t, m)
		m = press(t, m, "s")
	}
	mustContain(t, plain(m), "BY CATEGORY")
}

func TestByWeekCountsBankRowsByWeek(t *testing.T) {
	m, mo := statsApp(t)
	// A row booked late in September that counts for October.
	if _, ok := mo.AddImported(model.Expense, model.Imported{Label: "Rent", Amount: money.FromLei(100),
		Date: "2026-09-29", Payee: "rent", Source: model.SourceING, Key: "p9"}); !ok {
		t.Fatal("row not added")
	}
	m.dash.month = mo
	got := map[string]money.Amount{}
	var names []string
	for _, it := range m.byWeek().items {
		got[it.name] = it.amount
		names = append(names, it.name)
	}
	want := []string{"in Sep", "1–7 Oct", "8–14 Oct", "15–21 Oct", "22–28 Oct", "29–31 Oct"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("weeks = %v, want %v", names, want)
	}
	// Rata Credit 2154 + Lidl 900 + Bolt 27; the transfer is left out.
	if got["1–7 Oct"] != money.FromLei(3081) {
		t.Errorf("first week = %s", got["1–7 Oct"])
	}
	if got["15–21 Oct"] != money.FromLei(31) || got["in Sep"] != money.FromLei(100) {
		t.Errorf("weeks = %v", got)
	}
}

func TestByWeekWithoutBankRows(t *testing.T) {
	s := testStore(t)
	mo := model.New(statsMonth)
	mo.Add(model.Expense, "Rent", money.FromLei(2800), true)
	if err := s.Save(mo); err != nil {
		t.Fatal(err)
	}
	m := New(s)
	m.openMonth(statsMonth)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mustContain(t, plain(press(t, next.(Model), "s", "s", "s")), "BY WEEK", "No bank rows yet")
}

func TestBySourceLeavesTransfersOut(t *testing.T) {
	m, mo := statsApp(t)
	mo.AddImported(model.Income, model.Imported{Label: "Own account", Amount: money.FromLei(900),
		Date: "2026-10-07", Payee: "own account", Source: model.SourceRevolut, Key: "p10", Category: model.Transfers})
	m.dash.month = mo
	var names []string
	for _, it := range m.bySource().items {
		names = append(names, it.name)
	}
	if strings.Join(names, ",") != "Salary,Freelance,Employer SRL" {
		t.Errorf("sources = %v", names)
	}
}

func TestVsLastMonth(t *testing.T) {
	s := testStore(t)
	statsFixture(t, s)
	m := New(s)
	m.openMonth(statsMonth)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = press(t, next.(Model), "S")
	mustContain(t, plain(m), "VS SEPTEMBER", "Nothing saved for September 2026")

	sep := model.New(statsMonth.Prev())
	for _, f := range []struct {
		cat string
		lei int64
	}{{"Loan", 2300}, {"Groceries", 1200}, {"Fun", 400}} {
		id := sep.Add(model.Expense, f.cat, money.FromLei(f.lei), false)
		if err := sep.SetCategory(id, f.cat); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(sep); err != nil {
		t.Fatal(err)
	}
	screen := plain(m)
	mustContain(t, screen, "All expenses", "4.258,00 +358,00",
		"Groceries", "1.700,00 +500,00", "Fun", "0,00 −400,00", "2.300,00    same")
	// The biggest change comes first.
	if strings.Index(screen, "Groceries ") > strings.Index(screen, "Fun ") {
		t.Errorf("biggest change is not first:\n%s", screen)
	}
	checkWidth(t, m)
}
