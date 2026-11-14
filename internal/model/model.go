// Package model holds the budget domain: months, the entries inside them, and
// the rules for carrying recurring entries forward. It knows nothing about
// terminals or files.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/madalingiurca/budget/internal/money"
)

// Version is the month file format this build reads and writes.
const Version = 1

var ErrNotFound = errors.New("entry not found")

// Kind separates the two lists a month holds.
type Kind int

const (
	Income Kind = iota
	Expense
)

func (k Kind) String() string {
	if k == Income {
		return "income"
	}
	return "expense"
}

// Entry is one line of income or one planned expense.
type Entry struct {
	ID        string       `json:"id"`
	Label     string       `json:"label"`
	Amount    money.Amount `json:"amount"`
	Recurring bool         `json:"recurring"`
}

// Key identifies a month. It is the file name and the unit of navigation.
type Key struct {
	Year  int
	Month time.Month
}

// KeyOf returns the month containing t.
func KeyOf(t time.Time) Key { return Key{Year: t.Year(), Month: t.Month()} }

// Today returns the current month in local time.
func Today() Key { return KeyOf(time.Now()) }

// ParseKey reads a "2026-09" month key.
func ParseKey(s string) (Key, error) {
	year, month, ok := strings.Cut(s, "-")
	if !ok || len(year) != 4 || len(month) != 2 {
		return Key{}, fmt.Errorf("month %q is not in YYYY-MM form", s)
	}
	y, err := strconv.Atoi(year)
	if err != nil {
		return Key{}, fmt.Errorf("month %q has a bad year", s)
	}
	m, err := strconv.Atoi(month)
	if err != nil || m < 1 || m > 12 {
		return Key{}, fmt.Errorf("month %q has a bad month number", s)
	}
	return Key{Year: y, Month: time.Month(m)}, nil
}

// String renders the key as it appears in file names: "2026-09".
func (k Key) String() string { return fmt.Sprintf("%04d-%02d", k.Year, int(k.Month)) }

// Label renders the key for people: "September 2026".
func (k Key) Label() string { return fmt.Sprintf("%s %d", k.Month.String(), k.Year) }

// Shift moves n months forward, or backward when n is negative.
func (k Key) Shift(n int) Key {
	return KeyOf(time.Date(k.Year, k.Month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0))
}

func (k Key) Next() Key { return k.Shift(1) }
func (k Key) Prev() Key { return k.Shift(-1) }

// Compare orders keys chronologically, like cmp.Compare.
func (k Key) Compare(o Key) int {
	if k.Year != o.Year {
		if k.Year < o.Year {
			return -1
		}
		return 1
	}
	if k.Month != o.Month {
		if k.Month < o.Month {
			return -1
		}
		return 1
	}
	return 0
}

func (k Key) Before(o Key) bool { return k.Compare(o) < 0 }
func (k Key) After(o Key) bool  { return k.Compare(o) > 0 }

func (k Key) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(k.String())), nil }

func (k *Key) UnmarshalJSON(b []byte) error {
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return fmt.Errorf("month key %s: %w", b, err)
	}
	parsed, err := ParseKey(s)
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}

// Month is one budget: what comes in, what is planned to go out.
type Month struct {
	Version   int       `json:"version"`
	Key       Key       `json:"month"`
	Income    []Entry   `json:"income"`
	Expenses  []Entry   `json:"expenses"`
	UpdatedAt time.Time `json:"updated_at"`
}

// New returns an empty month.
func New(k Key) *Month {
	return &Month{Version: Version, Key: k, Income: []Entry{}, Expenses: []Entry{}}
}

// SeedFrom returns a new month for k carrying prev's recurring entries, with
// fresh ids so editing the copy never touches the original. A nil prev yields
// an empty month. This is the whole recurrence mechanism: the previous month is
// the template, so there is no second place to keep in sync.
func SeedFrom(prev *Month, k Key) *Month {
	m := New(k)
	if prev == nil {
		return m
	}
	for _, kind := range []Kind{Income, Expense} {
		for _, e := range prev.Entries(kind) {
			if !e.Recurring {
				continue
			}
			copied := e
			copied.ID = m.newID()
			*m.list(kind) = append(*m.list(kind), copied)
		}
	}
	return m
}

func (m *Month) list(kind Kind) *[]Entry {
	if kind == Income {
		return &m.Income
	}
	return &m.Expenses
}

// Entries returns the entries of one kind, in the order the user added them.
func (m *Month) Entries(kind Kind) []Entry { return *m.list(kind) }

// Total sums one kind.
func (m *Month) Total(kind Kind) money.Amount {
	var sum money.Amount
	for _, e := range m.Entries(kind) {
		sum += e.Amount
	}
	return sum
}

func (m *Month) TotalIncome() money.Amount   { return m.Total(Income) }
func (m *Month) TotalExpenses() money.Amount { return m.Total(Expense) }

// Left is what remains after every planned expense.
func (m *Month) Left() money.Amount { return m.TotalIncome() - m.TotalExpenses() }

// LeftShare is the share of income still unspent, as a percentage. It reports
// false when there is no income to take a share of.
func (m *Month) LeftShare() (int, bool) {
	income := m.TotalIncome()
	if income <= 0 {
		return 0, false
	}
	return int(m.Left() * 100 / income), true
}

// IsEmpty reports whether the month has nothing in it at all.
func (m *Month) IsEmpty() bool { return len(m.Income) == 0 && len(m.Expenses) == 0 }

// Count returns the total number of entries across both kinds.
func (m *Month) Count() int { return len(m.Income) + len(m.Expenses) }

// Add appends an entry and returns its new id.
func (m *Month) Add(kind Kind, label string, amount money.Amount, recurring bool) string {
	e := Entry{ID: m.newID(), Label: label, Amount: amount, Recurring: recurring}
	*m.list(kind) = append(*m.list(kind), e)
	return e.ID
}

// Find returns the entry with the given id.
func (m *Month) Find(kind Kind, id string) (Entry, bool) {
	for _, e := range m.Entries(kind) {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Update replaces the label, amount and recurring flag of an existing entry.
func (m *Month) Update(kind Kind, id, label string, amount money.Amount, recurring bool) error {
	entries := *m.list(kind)
	for i := range entries {
		if entries[i].ID == id {
			entries[i].Label = label
			entries[i].Amount = amount
			entries[i].Recurring = recurring
			return nil
		}
	}
	return fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
}

// SetRecurring flips whether an entry carries into future months.
func (m *Month) SetRecurring(kind Kind, id string, recurring bool) error {
	entries := *m.list(kind)
	for i := range entries {
		if entries[i].ID == id {
			entries[i].Recurring = recurring
			return nil
		}
	}
	return fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
}

// Remove deletes an entry.
func (m *Month) Remove(kind Kind, id string) error {
	entries := *m.list(kind)
	for i, e := range entries {
		if e.ID == id {
			*m.list(kind) = append(entries[:i:i], entries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
}

// newID returns a short id that no entry in this month is using.
func (m *Month) newID() string {
	for {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			// crypto/rand does not fail in practice; fall back to a
			// time-derived id rather than panicking mid-edit.
			return strconv.FormatInt(time.Now().UnixNano()%0xffffff, 16)
		}
		id := hex.EncodeToString(b[:])
		if _, taken := m.Find(Income, id); taken {
			continue
		}
		if _, taken := m.Find(Expense, id); taken {
			continue
		}
		return id
	}
}
