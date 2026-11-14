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

	narrow := New(s)
	narrow.openMonth(base)
	n2, _ := narrow.Update(tea.WindowSizeMsg{Width: 46, Height: 30})
	show("NARROW (46 cols)", n2.(Model))

}
