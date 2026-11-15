package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
	"github.com/madalingiurca/budget/internal/store"
)

// TestPreview is not an assertion; it prints the screens so they can be looked
// at. Run with: go test ./internal/ui -run TestPreview -v
func TestPreview(t *testing.T) {
	s, _ := store.Open(t.TempDir())
	base := model.Key{Year: 2026, Month: time.September}
	for i, spend := range []int64{6750, 9100, 5200, 11900, 14000} {
		mo := model.New(base.Shift(-i))
		mo.Add(model.Income, "Salary", money.FromLei(12500), true)
		if i == 0 {
			mo.Add(model.Income, "Freelance project", money.FromLei(2000), false)
		}
		mo.Add(model.Expense, "Rent", money.FromLei(2800), true)
		mo.Add(model.Expense, "Utilities", money.FromLei(450), true)
		mo.Add(model.Expense, "Everything else", money.FromLei(spend-3250), false)
		if err := s.Save(mo); err != nil {
			t.Fatal(err)
		}
	}

	show := func(name string, m Model) {
		t.Logf("\n\n===== %s =====\n%s", name, m.View().Content)
	}

	m := New(s)
	m.openMonth(base)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	m = next.(Model)
	show("MONTH", m)

	show("FORM", press(t, m, "a"))
	show("DELETE", press(t, m, "down", "d"))
	show("HISTORY", press(t, m, "h"))
	show("HELP", press(t, m, "?"))

	// A month with imported entries, categories and a forecast laid under them.
	oct := base.Next()
	mo := model.New(oct)
	mo.Add(model.Income, "Salary", money.FromLei(12500), true)
	for _, f := range []struct {
		label, cat string
		lei        int64
	}{{"Mortgage", "Loan", 2300}, {"Groceries budget", "Groceries", 1500}, {"Holiday fund", "Savings", 500}} {
		id := mo.Add(model.Expense, f.label, money.FromLei(f.lei), true)
		mo.SetCategory(id, f.cat)
	}
	row := func(key, payee, date string, lei int64, cat string) model.Imported {
		return model.Imported{Label: payee, Amount: money.FromLei(lei), Date: date,
			Payee: model.NormalizePayee(payee), Source: model.SourceING, Key: key, Category: cat}
	}
	mo.ReplaceForecast(model.Expense, mo.Expenses[0].ID, row("p1", "Rata Credit", "2026-10-02", 2154, ""))
	mo.AddImported(model.Income, row("p2", "Employer SRL", "2026-10-30", 12650, ""))
	mo.AddImported(model.Expense, row("p3", "Lidl", "2026-10-03", 412, "Groceries"))
	mo.AddImported(model.Expense, row("p4", "Mega Image", "2026-10-11", 1240, "Groceries"))
	mo.AddImported(model.Expense, row("p5", "Round Up", "2026-10-04", 3, "Savings"))
	mo.AddImported(model.Expense, row("p6", "Bolt", "2026-10-05", 27, ""))
	mo.AddImported(model.Expense, row("p7", "Bolt", "2026-10-09", 31, ""))
	mo.AddImported(model.Expense, row("p8", "Cinema City", "2026-10-12", 64, ""))
	mo.AddImported(model.Expense, row("p9", "Revolut**1234*", "2026-10-06", 500, model.Transfers))
	if err := s.Save(mo); err != nil {
		t.Fatal(err)
	}
	c := New(s)
	c.openMonth(oct)
	c2, _ := c.Update(tea.WindowSizeMsg{Width: 100, Height: 44})
	c = c2.(Model)
	show("CATEGORIES", c)
	show("CATEGORISE", press(t, c, "down", "down", "down", "c"))
	show("EXPENSE FORM", press(t, c, "x"))
	show("PAYEES", press(t, c, "u"))
	show("PAYEES CATEGORISE", press(t, c, "u", "enter"))

	short, _ := c.Update(tea.WindowSizeMsg{Width: 100, Height: 22})
	show("SCROLLED (22 rows)", press(t, short.(Model), "down", "down", "down", "down", "down", "down", "down", "down"))

	narrow := New(s)
	narrow.openMonth(base)
	n2, _ := narrow.Update(tea.WindowSizeMsg{Width: 46, Height: 30})
	show("NARROW (46 cols)", n2.(Model))

}
