package ui

import (
	"strings"
	"testing"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
	"github.com/madalingiurca/budget/internal/store"
)

// onHeader puts the cursor on a category's header.
func onHeader(t *testing.T, m Model, cat string) Model {
	t.Helper()
	for i, r := range m.dash.rows() {
		if r.header && r.cat == cat {
			m.dash.cursor = i
			return m
		}
	}
	t.Fatalf("no header for %s", cat)
	return m
}

// onEntry puts the cursor on the entry with a given label.
func onEntry(t *testing.T, m Model, kind model.Kind, label string) Model {
	t.Helper()
	for _, e := range m.dash.month.Entries(kind) {
		if e.Label == label {
			m.selectEntry(kind, e.ID)
			return m
		}
	}
	t.Fatalf("no %s %s", kind, label)
	return m
}

// pizzaOctober is a month with a team pizza waiting to be paid back, the
// refund for it, a dinner split with friends and one friend's share.
func pizzaOctober(t *testing.T, s *store.Store) *model.Month {
	t.Helper()
	mo := model.New(october)
	mo.AddImported(model.Income, bank("sal", "Employer", 10000, ""))
	mo.AddImported(model.Income, bank("ref", "ING Hubs", 282, ""))
	mo.AddImported(model.Income, bank("ana", "Ana", 200, ""))
	mo.AddImported(model.Expense, bank("piz", "Jerry's Pizza", 282, "PayAndReceiveLater"))
	mo.AddImported(model.Expense, bank("din", "Bistro", 848, "PayAndReceiveLater"))
	mo.AddImported(model.Expense, bank("gro", "Lidl", 500, "Groceries"))
	saveMonth(t, s, mo)
	return mo
}

func TestPaidBackCategoryShowsWhatIsStillOpen(t *testing.T) {
	s := testStore(t)
	pizzaOctober(t, s)
	m := appOn(t, s, october, 120, testH)
	m = press(t, onHeader(t, m, "PayAndReceiveLater"), "b")

	r, err := s.LoadRules()
	if err != nil || !r.IsPaidBack("PayAndReceiveLater") {
		t.Fatalf("rule not saved: %v %v", r, err)
	}
	m = unfoldAll(m)
	screen := plain(m)
	mustContain(t, screen,
		"PayAndReceiveLater (2) · paid back later, 2 open",
		"Jerry's Pizza · awaiting refund",
		"still to come back", "1.130,00 lei",
	)

	m = press(t, onHeader(t, m, "PayAndReceiveLater"), "b")
	if strings.Contains(plain(m), "awaiting refund") {
		t.Errorf("toggling off left expenses awaiting:\n%s", plain(m))
	}
}

func TestLinkedRefundIsShownAsNotCounted(t *testing.T) {
	s := testStore(t)
	mo := pizzaOctober(t, s)
	link := func(refund, expense string) {
		var rid, eid string
		for _, e := range mo.Income {
			if e.Label == refund {
				rid = e.ID
			}
		}
		for _, e := range mo.Expenses {
			if e.Label == expense {
				eid = e.ID
			}
		}
		if _, err := model.LinkRefund(mo, rid, mo, eid); err != nil {
			t.Fatal(err)
		}
	}
	link("ING Hubs", "Jerry's Pizza")
	link("Ana", "Bistro")
	saveMonth(t, s, mo)

	m := unfoldAll(appOn(t, s, october, 120, testH))
	screen := plain(m)
	mustContain(t, screen,
		"ING Hubs · refund, not counted",
		"Ana · refund, not counted",
		"Jerry's Pizza · paid back",
		"Bistro · 200,00 paid back",
		"10.000,00 lei", // income: the salary only
		"1.148,00 lei",  // expenses: groceries and the 648 of the dinner still counted
	)
	if got := m.dash.month.Left(); got != money.FromLei(10000-1148) {
		t.Errorf("left = %v", got)
	}
}
