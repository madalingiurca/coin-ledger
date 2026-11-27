package model

import (
	"errors"
	"fmt"
	"slices"

	"github.com/madalingiurca/budget/internal/money"
)

// Ref points at an entry in some month, and says how much of the money
// between the two entries is a reimbursement. A refund (income) lists the
// expenses it pays back; each of those expenses lists the refund back, with
// the same amount. The two sides are kept in step by LinkRefund and UnlinkRefund, which is
// what lets each month count its own totals from its own file.
type Ref struct {
	Month  Key          `json:"month"`
	ID     string       `json:"id"`
	Amount money.Amount `json:"amount"`
}

// ErrLinked refuses a change that would leave a reimbursement pointing at an
// entry that is no longer there.
var ErrLinked = errors.New("linked to a reimbursement")

// links returns the reimbursement refs on whichever side the entry is.
func (e Entry) links() []Ref {
	if len(e.Reimburses) > 0 {
		return e.Reimburses
	}
	return e.ReimbursedBy
}

// Links lists what the entry is linked to: for a refund, the expenses it
// pays back; for an expense, the refunds that paid it back.
func (e Entry) Links() []Ref { return slices.Clone(e.links()) }

// IsLinked reports whether the entry takes part in any reimbursement.
func (e Entry) IsLinked() bool { return len(e.Reimburses)+len(e.ReimbursedBy) > 0 }

// Reimbursed is how much of the entry is a reimbursement: for an expense,
// what has been paid back; for a refund, how much of it went to expenses.
// Neither side counts that part in any total.
func (e Entry) Reimbursed() money.Amount {
	var sum money.Amount
	for _, r := range e.links() {
		sum += r.Amount
	}
	return min(sum, max(e.Amount, 0))
}

// Counted is the part of the entry's amount that counts toward totals.
func (e Entry) Counted() money.Amount { return e.Amount - e.Reimbursed() }

// Open is what is still free on the entry for a new link: for an expense,
// what has not been paid back yet; for a refund, what is not yet assigned to
// an expense.
func (e Entry) Open() money.Amount { return max(0, e.Counted()) }

// LinkedTo reports how much of the entry is linked to the entry id in month k.
func (e Entry) LinkedTo(k Key, id string) money.Amount {
	for _, r := range e.links() {
		if r.Month == k && r.ID == id {
			return r.Amount
		}
	}
	return 0
}

// entryPtr finds an entry for changing it in place.
func (m *Month) entryPtr(kind Kind, id string) (*Entry, error) {
	entries := *m.list(kind)
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
}

// LinkRefund records that the refund (income) in refundMonth pays back the expense
// in expenseMonth, for as much of each as is still open: a refund smaller
// than the expense leaves the difference counted, and a refund larger than
// the expense leaves the rest of it free for another expense or counted as
// income. The two months may be the same one. It returns the amount linked.
func LinkRefund(refundMonth *Month, refundID string, expenseMonth *Month, expenseID string) (money.Amount, error) {
	refund, err := refundMonth.entryPtr(Income, refundID)
	if err != nil {
		return 0, err
	}
	expense, err := expenseMonth.entryPtr(Expense, expenseID)
	if err != nil {
		return 0, err
	}
	switch {
	case refund.IsTransfer() || expense.IsTransfer():
		return 0, fmt.Errorf("a transfer is not a reimbursement")
	case refund.LinkedTo(expenseMonth.Key, expenseID) > 0:
		return 0, fmt.Errorf("%s already pays back %s", refund.Label, expense.Label)
	}
	amount := min(refund.Open(), expense.Open())
	switch {
	case refund.Open() <= 0:
		return 0, fmt.Errorf("all of %s already pays back other expenses", refund.Label)
	case expense.Open() <= 0:
		return 0, fmt.Errorf("%s is already paid back", expense.Label)
	}
	refund.Reimburses = append(slices.Clip(refund.Reimburses), Ref{Month: expenseMonth.Key, ID: expenseID, Amount: amount})
	expense.ReimbursedBy = append(slices.Clip(expense.ReimbursedBy), Ref{Month: refundMonth.Key, ID: refundID, Amount: amount})
	return amount, nil
}

// UnlinkRefund undoes LinkRefund, on both sides. An entry that is no longer there is
// skipped, so a half left dangling can still be cleared.
func UnlinkRefund(refundMonth *Month, refundID string, expenseMonth *Month, expenseID string) {
	if e, err := refundMonth.entryPtr(Income, refundID); err == nil {
		e.Reimburses = dropRef(e.Reimburses, expenseMonth.Key, expenseID)
	}
	if e, err := expenseMonth.entryPtr(Expense, expenseID); err == nil {
		e.ReimbursedBy = dropRef(e.ReimbursedBy, refundMonth.Key, refundID)
	}
}

// DropLink removes the entry's link to the entry id in month k, on this side
// only: for when the other side's month cannot be read.
func (m *Month) DropLink(kind Kind, id string, k Key, other string) {
	e, err := m.entryPtr(kind, id)
	if err != nil {
		return
	}
	e.Reimburses = dropRef(e.Reimburses, k, other)
	e.ReimbursedBy = dropRef(e.ReimbursedBy, k, other)
}

func dropRef(refs []Ref, k Key, id string) []Ref {
	out := slices.DeleteFunc(slices.Clone(refs), func(r Ref) bool { return r.Month == k && r.ID == id })
	if len(out) == 0 {
		return nil
	}
	return out
}

// Other is the kind on the other side of a link: refunds pay back expenses.
func (k Kind) Other() Kind {
	if k == Income {
		return Expense
	}
	return Income
}

// OpenRefunds is how much of the month's expenses in the given categories has
// not been paid back yet: money that is still expected to come back.
func (m *Month) OpenRefunds(reimbursable func(category string) bool) (money.Amount, int) {
	var sum money.Amount
	n := 0
	for _, e := range m.Expenses {
		if e.Category != "" && reimbursable(e.Category) && e.Open() > 0 {
			sum += e.Open()
			n++
		}
	}
	return sum, n
}
