package model

import (
	"errors"
	"testing"
	"time"
)

func row(key, payee string, amount int64, date, category string) Imported {
	return Imported{Label: payee, Amount: lei(amount), Date: date, Payee: NormalizePayee(payee),
		Source: SourceING, Key: key, Category: category}
}

// November: the pizza paid for the team and the employer's refund for it,
// beside a salary and groceries that count as usual.
func november() (m *Month, pizza, refund string) {
	m = New(Key{2026, time.November})
	m.AddImported(Income, row("s", "Employer", 10000, "2026-11-10", ""))
	refund, _ = m.AddImported(Income, row("r", "ING Hubs", 282, "2026-11-17", ""))
	pizza, _ = m.AddImported(Expense, row("p", "Jerry's Pizza", 282, "2026-11-14", "PayAndReceiveLater"))
	m.AddImported(Expense, row("g", "Carrefour", 500, "2026-11-12", "Groceries"))
	return m, pizza, refund
}

func TestFullRefundLeavesBothOutOfTheTotals(t *testing.T) {
	m, pizza, refund := november()
	if m.TotalIncome() != lei(10282) || m.TotalExpenses() != lei(782) {
		t.Fatalf("before linking: %v / %v", m.TotalIncome(), m.TotalExpenses())
	}
	got, err := LinkRefund(m, refund, m, pizza)
	if err != nil || got != lei(282) {
		t.Fatalf("link = %v, %v", got, err)
	}
	if m.TotalIncome() != lei(10000) || m.TotalExpenses() != lei(500) || m.Left() != lei(9500) {
		t.Errorf("after linking: %v / %v / %v", m.TotalIncome(), m.TotalExpenses(), m.Left())
	}
	if m.Spent() != lei(500) || m.ActualTotal(Income) != lei(10000) {
		t.Errorf("actuals: spent %v, income %v", m.Spent(), m.ActualTotal(Income))
	}
	for _, c := range m.Categories() {
		if c.Name == "PayAndReceiveLater" && (c.Projected != 0 || c.Count != 1) {
			t.Errorf("paid-back category = %+v", c)
		}
	}
	e, _ := m.Find(Expense, pizza)
	r, _ := m.Find(Income, refund)
	if e.Open() != 0 || r.Open() != 0 || e.LinkedTo(m.Key, refund) != lei(282) || r.LinkedTo(m.Key, pizza) != lei(282) {
		t.Errorf("sides: expense %+v, refund %+v", e, r)
	}
}

func TestPartialRefundLeavesTheDifferenceCounted(t *testing.T) {
	m := New(Key{2026, time.November})
	dinner, _ := m.AddImported(Expense, row("d", "Bistro", 848, "2026-11-14", "PayAndReceiveLater"))
	a, _ := m.AddImported(Income, row("a", "Friend A", 200, "2026-11-15", ""))
	b, _ := m.AddImported(Income, row("b", "Friend B", 300, "2026-11-16", ""))
	if _, err := LinkRefund(m, a, m, dinner); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkRefund(m, b, m, dinner); err != nil {
		t.Fatal(err)
	}
	if m.TotalExpenses() != lei(348) || m.TotalIncome() != 0 {
		t.Errorf("totals: %v / %v", m.TotalIncome(), m.TotalExpenses())
	}
	e, _ := m.Find(Expense, dinner)
	if e.Reimbursed() != lei(500) || e.Open() != lei(348) || len(e.Links()) != 2 {
		t.Errorf("dinner = %+v", e)
	}
}

// A refund larger than the expense keeps the rest: free for another expense,
// and counted as income until then.
func TestRefundLargerThanTheExpenseKeepsTheRest(t *testing.T) {
	m := New(Key{2026, time.November})
	taxi, _ := m.AddImported(Expense, row("t", "Taxi", 60, "2026-11-03", "Work"))
	lunch, _ := m.AddImported(Expense, row("l", "Lunch", 100, "2026-11-04", "Work"))
	refund, _ := m.AddImported(Income, row("r", "Employer", 200, "2026-11-20", ""))

	if got, _ := LinkRefund(m, refund, m, taxi); got != lei(60) {
		t.Errorf("taxi linked %v", got)
	}
	if m.TotalIncome() != lei(140) {
		t.Errorf("income with 140 unassigned = %v", m.TotalIncome())
	}
	if got, _ := LinkRefund(m, refund, m, lunch); got != lei(100) {
		t.Errorf("lunch linked %v", got)
	}
	if m.TotalIncome() != lei(40) || m.TotalExpenses() != 0 {
		t.Errorf("totals: %v / %v", m.TotalIncome(), m.TotalExpenses())
	}
	if _, err := LinkRefund(m, refund, m, lunch); err == nil {
		t.Error("linking the same pair twice should be refused")
	}
	other, _ := m.AddImported(Expense, row("o", "Hotel", 900, "2026-11-05", "Work"))
	if got, _ := LinkRefund(m, refund, m, other); got != lei(40) {
		t.Errorf("the last 40 went %v", got)
	}
	if _, err := LinkRefund(m, refund, m, other); err == nil {
		t.Error("a refund with nothing left should be refused")
	}
}

// The refund often arrives a month later. Each month counts its own side.
func TestRefundInALaterMonth(t *testing.T) {
	nov := New(Key{2026, time.November})
	dec := New(Key{2026, time.December})
	pizza, _ := nov.AddImported(Expense, row("p", "Jerry's Pizza", 282, "2026-11-14", "PayAndReceiveLater"))
	refund, _ := dec.AddImported(Income, row("r", "ING Hubs", 282, "2026-12-03", ""))
	if _, err := LinkRefund(dec, refund, nov, pizza); err != nil {
		t.Fatal(err)
	}
	if nov.TotalExpenses() != 0 || dec.TotalIncome() != 0 {
		t.Errorf("nov expenses %v, dec income %v", nov.TotalExpenses(), dec.TotalIncome())
	}
	e, _ := nov.Find(Expense, pizza)
	if links := e.Links(); len(links) != 1 || links[0].Month != dec.Key || links[0].ID != refund {
		t.Errorf("expense points at %+v", links)
	}

	UnlinkRefund(dec, refund, nov, pizza)
	if nov.TotalExpenses() != lei(282) || dec.TotalIncome() != lei(282) {
		t.Errorf("after unlinking: %v / %v", nov.TotalExpenses(), dec.TotalIncome())
	}
	if e, _ := nov.Find(Expense, pizza); e.IsLinked() {
		t.Errorf("still linked: %+v", e)
	}
}

func TestLinkedEntriesCannotBeDeletedOrMovedWithoutUnlinking(t *testing.T) {
	m, pizza, refund := november()
	if _, err := LinkRefund(m, refund, m, pizza); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(Expense, pizza); !errors.Is(err, ErrLinked) {
		t.Errorf("remove = %v", err)
	}
	if _, err := m.Take(Income, refund); !errors.Is(err, ErrLinked) {
		t.Errorf("take = %v", err)
	}
	m.DropLink(Expense, pizza, m.Key, refund)
	if err := m.Remove(Expense, pizza); err != nil {
		t.Errorf("after dropping the link: %v", err)
	}
}

func TestTransfersAreNotReimbursements(t *testing.T) {
	m := New(Key{2026, time.November})
	top, _ := m.AddImported(Income, row("t", "Top-up by *5438", 100, "2026-11-04", Transfers))
	e, _ := m.AddImported(Expense, row("e", "Shop", 100, "2026-11-04", "Shopping"))
	if _, err := LinkRefund(m, top, m, e); err == nil {
		t.Error("a transfer was linked as a refund")
	}
}

func TestCloneDoesNotShareLinks(t *testing.T) {
	m, pizza, refund := november()
	if _, err := LinkRefund(m, refund, m, pizza); err != nil {
		t.Fatal(err)
	}
	c := m.Clone()
	UnlinkRefund(c, refund, c, pizza)
	if e, _ := m.Find(Expense, pizza); !e.IsLinked() {
		t.Error("unlinking the clone unlinked the original")
	}
}

func TestOpenRefunds(t *testing.T) {
	m, pizza, refund := november()
	later := func(c string) bool { return c == "PayAndReceiveLater" }
	if sum, n := m.OpenRefunds(later); sum != lei(282) || n != 1 {
		t.Errorf("open before = %v, %d", sum, n)
	}
	LinkRefund(m, refund, m, pizza)
	if sum, n := m.OpenRefunds(later); sum != 0 || n != 0 {
		t.Errorf("open after = %v, %d", sum, n)
	}
}
