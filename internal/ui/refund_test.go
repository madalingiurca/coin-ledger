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

func TestLinkingARefundFromTheIncomeSide(t *testing.T) {
	s := testStore(t)
	pizzaOctober(t, s)
	m := onEntry(t, appOn(t, s, october, 120, testH), model.Income, "ING Hubs")
	m = press(t, m, "b")
	if m.link == nil {
		t.Fatalf("no link dialog: %s", m.status)
	}
	screen := plain(m)
	mustContain(t, screen, "What does ING Hubs pay back?", "Jerry's Pizza · PayAndReceiveLat", "Bistro · PayAndReceiveLater")
	// The expense of the same amount is offered first.
	if c := m.link.cands[0].entry.Label; c != "Jerry's Pizza" {
		t.Errorf("first offered %s", c)
	}
	m = press(t, m, "space", "enter")
	if m.link != nil || m.isError {
		t.Fatalf("dialog still open or error: %s", m.status)
	}
	mustContain(t, plain(m), "ING Hubs pays back Jerry's Pizza · 282,00 lei")

	got, err := s.Load(october)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalIncome() != money.FromLei(10200) || got.TotalExpenses() != money.FromLei(1348) {
		t.Errorf("saved totals: %v / %v", got.TotalIncome(), got.TotalExpenses())
	}

	// Opening it again ticks the pizza; unticking it unlinks.
	m = press(t, m, "b")
	if !m.link.picked[0] {
		t.Fatal("the linked expense is not ticked")
	}
	m = press(t, m, "space", "enter")
	got, _ = s.Load(october)
	if got.TotalIncome() != money.FromLei(10482) {
		t.Errorf("after unlinking: income %v", got.TotalIncome())
	}
	mustContain(t, plain(m), "unlinked Jerry's Pizza")
}

func TestLinkDialogFilters(t *testing.T) {
	s := testStore(t)
	pizzaOctober(t, s)
	m := onEntry(t, appOn(t, s, october, 120, testH), model.Income, "ING Hubs")
	m = press(t, m, "b")
	m = press(t, m, typing("bist")...)
	if vis := m.link.visible(); len(vis) != 1 || m.link.cands[vis[0]].entry.Label != "Bistro" {
		t.Fatalf("filter left %v", vis)
	}
	m = press(t, m, "space", "enter")
	got, _ := s.Load(october)
	for _, e := range got.Expenses {
		if e.Label == "Bistro" && e.Reimbursed() != money.FromLei(282) {
			t.Errorf("bistro = %+v", e)
		}
	}
}

// The refund came in November for an October expense: linked from the
// expense, it is found in the month after and both files change.
func TestLinkingAcrossMonthsAndDeletingOneSide(t *testing.T) {
	s := testStore(t)
	pizzaOctober(t, s)
	nov := model.New(october.Next())
	nov.AddImported(model.Income, bank("late", "ING Hubs late", 282, ""))
	saveMonth(t, s, nov)

	m := onEntry(t, appOn(t, s, october, 120, testH), model.Expense, "Jerry's Pizza")
	m = press(t, m, "b")
	mustContain(t, plain(m), "What paid back Jerry's Pizza?", "ING Hubs late")
	m = press(t, m, typing("late")...)
	m = press(t, m, "space", "enter")
	if m.isError {
		t.Fatal(m.status)
	}
	gotNov, _ := s.Load(nov.Key)
	if gotNov.TotalIncome() != 0 {
		t.Errorf("november income = %v", gotNov.TotalIncome())
	}

	// A linked entry cannot move to another month until it is unlinked.
	m = press(t, m, ">")
	if !m.isError || !strings.Contains(m.status, "unlink") {
		t.Errorf("move status = %q", m.status)
	}

	// Deleting the expense lets go of the refund in November too.
	m = press(t, m, "d")
	mustContain(t, plain(m), "reimbursement links go too")
	m = press(t, m, "y")
	if m.isError {
		t.Fatal(m.status)
	}
	gotNov, _ = s.Load(nov.Key)
	if gotNov.TotalIncome() != money.FromLei(282) || gotNov.Income[0].IsLinked() {
		t.Errorf("november after delete: %v %+v", gotNov.TotalIncome(), gotNov.Income[0])
	}
}

func TestTransfersCannotBeLinked(t *testing.T) {
	s := testStore(t)
	mo := model.New(october)
	mo.AddImported(model.Income, bank("t", "Top-up by *5438", 100, model.Transfers))
	mo.AddImported(model.Expense, bank("e", "Shop", 100, "Shopping"))
	saveMonth(t, s, mo)
	m := onEntry(t, appOn(t, s, october, 120, testH), model.Income, "Top-up by *5438")
	m = press(t, m, "b")
	if m.link != nil || !strings.Contains(m.status, "transfer") {
		t.Errorf("status %q", m.status)
	}
}

func TestLinkDialogFitsANarrowTerminal(t *testing.T) {
	s := testStore(t)
	pizzaOctober(t, s)
	for _, w := range []int{40, 60, testW} {
		m := onEntry(t, appOn(t, s, october, w, testH), model.Income, "ING Hubs")
		m = press(t, m, "b")
		checkWidth(t, m)
	}
}
