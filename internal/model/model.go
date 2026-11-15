// Package model holds the budget domain: months, the entries inside them, and
// the rules for carrying recurring entries forward. It knows nothing about
// terminals or files.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
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

// Transfers is the built-in category for money moving between the user's own
// spending accounts. Entries in it are shown but never counted.
const Transfers = "Transfers"

// Sources an imported entry can come from.
const (
	SourceING     = "ing"
	SourceRevolut = "revolut"
)

// Entry is one line of income or one expense. An entry typed by hand is a
// forecast; an entry that came from a bank file is an actual, recognisable by
// its Source. Every field after Recurring is optional and left out of the
// file when empty.
type Entry struct {
	ID        string       `json:"id"`
	Label     string       `json:"label"`
	Amount    money.Amount `json:"amount"`
	Recurring bool         `json:"recurring"`
	// Category groups expenses. Empty means uncategorised.
	Category string `json:"category,omitempty"`
	// Planned is the forecast amount this actual entry replaced, kept so the
	// difference stays visible. Zero when nothing was replaced.
	Planned money.Amount `json:"planned,omitempty"`
	// Date is the booking date, "2026-10-02". Empty on forecast entries.
	Date string `json:"date,omitempty"`
	// Payee is the normalised counterparty that category rules match on.
	Payee string `json:"payee,omitempty"`
	// Source names the bank the entry was imported from.
	Source string `json:"source,omitempty"`
	// Key identifies the bank row, so importing it twice adds it once.
	Key string `json:"key,omitempty"`
}

// IsForecast reports whether the entry was typed by hand rather than imported.
func (e Entry) IsForecast() bool { return e.Source == "" }

// Forecast is the planned part of the entry: its amount while it is still a
// forecast, or the amount it replaced once an actual took its place.
func (e Entry) Forecast() money.Amount {
	if e.IsForecast() {
		return e.Amount
	}
	return e.Planned
}

// Actual is what the bank says happened; zero for a forecast.
func (e Entry) Actual() money.Amount {
	if e.IsForecast() {
		return 0
	}
	return e.Amount
}

// IsTransfer reports whether the entry is excluded from every total.
func (e Entry) IsTransfer() bool { return e.Category == Transfers }

// Imported is one bank transaction on its way into a month.
type Imported struct {
	Label    string
	Amount   money.Amount
	Date     string
	Payee    string
	Source   string
	Key      string
	Category string
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

// Month is one budget: what comes in and what goes out, forecast and actual.
type Month struct {
	Version  int     `json:"version"`
	Key      Key     `json:"month"`
	Income   []Entry `json:"income"`
	Expenses []Entry `json:"expenses"`
	// SkippedKeys are bank rows the user deleted after importing them, so a
	// later import of the same file does not bring them back.
	SkippedKeys []string  `json:"skipped_keys,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
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
			// What carries forward is the plan, never last month's bank row.
			amount := e.Amount
			if e.Planned > 0 {
				amount = e.Planned
			}
			copied := Entry{ID: m.newID(), Label: e.Label, Amount: amount, Recurring: true, Category: e.Category}
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

// Total is the projected amount of one kind: what the month is expected to
// come to once forecasts and actuals are laid over each other. A month with no
// imported entries totals exactly as its forecast.
//
// Income is treated as one group and expenses per category; each group counts
// the larger of its forecast and its actual, so a forecast and the bank rows
// that fulfil it are not counted twice. Uncategorised expenses have nothing
// tying them together, so there forecast and actual simply add up. Transfers
// are left out.
func (m *Month) Total(kind Kind) money.Amount {
	if kind == Income {
		var forecast, actual money.Amount
		for _, e := range m.Income {
			if e.IsTransfer() {
				continue
			}
			forecast += e.Forecast()
			actual += e.Actual()
		}
		return max(forecast, actual)
	}
	var sum money.Amount
	for _, c := range m.Categories() {
		sum += c.Projected
	}
	return sum
}

// ActualTotal sums what the bank has reported for one kind, without transfers.
func (m *Month) ActualTotal(kind Kind) money.Amount {
	var sum money.Amount
	for _, e := range m.Entries(kind) {
		if !e.IsTransfer() {
			sum += e.Actual()
		}
	}
	return sum
}

// ForecastTotal sums the plan for one kind, without transfers.
func (m *Month) ForecastTotal(kind Kind) money.Amount {
	var sum money.Amount
	for _, e := range m.Entries(kind) {
		if !e.IsTransfer() {
			sum += e.Forecast()
		}
	}
	return sum
}

// Spent is the actual expenses so far.
func (m *Month) Spent() money.Amount { return m.ActualTotal(Expense) }

// HasActuals reports whether anything in the month came from a bank file.
func (m *Month) HasActuals() bool {
	for _, kind := range []Kind{Income, Expense} {
		for _, e := range m.Entries(kind) {
			if !e.IsForecast() {
				return true
			}
		}
	}
	return false
}

// CategoryTotal is one expense category of a month.
type CategoryTotal struct {
	// Name is empty for uncategorised expenses.
	Name     string
	Forecast money.Amount
	Actual   money.Amount
	// Projected is what the category counts for in the month's totals; zero
	// for Transfers.
	Projected money.Amount
	Count     int
}

// Categories groups the expenses: uncategorised first, then by name, with
// Transfers last. Only categories that have entries this month appear.
func (m *Month) Categories() []CategoryTotal {
	byName := map[string]*CategoryTotal{}
	var names []string
	for _, e := range m.Expenses {
		c, ok := byName[e.Category]
		if !ok {
			c = &CategoryTotal{Name: e.Category}
			byName[e.Category] = c
			names = append(names, e.Category)
		}
		c.Forecast += e.Forecast()
		c.Actual += e.Actual()
		c.Count++
	}
	slices.SortFunc(names, func(a, b string) int {
		if ra, rb := categoryRank(a), categoryRank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	out := make([]CategoryTotal, 0, len(names))
	for _, name := range names {
		c := *byName[name]
		switch name {
		case Transfers:
			c.Projected = 0
		case "":
			c.Projected = c.Forecast + c.Actual
		default:
			c.Projected = max(c.Forecast, c.Actual)
		}
		out = append(out, c)
	}
	return out
}

func categoryRank(name string) int {
	switch name {
	case "":
		return 0
	case Transfers:
		return 2
	}
	return 1
}

// InCategory returns the expenses of one category, in file order.
func (m *Month) InCategory(name string) []Entry {
	var out []Entry
	for _, e := range m.Expenses {
		if e.Category == name {
			out = append(out, e)
		}
	}
	return out
}

func (m *Month) TotalIncome() money.Amount   { return m.Total(Income) }
func (m *Month) TotalExpenses() money.Amount { return m.Total(Expense) }

// Left is what is projected to remain after every expense.
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

// SetCategory files an expense under a category; an empty name makes it
// uncategorised again.
func (m *Month) SetCategory(id, category string) error {
	for i := range m.Expenses {
		if m.Expenses[i].ID == id {
			m.Expenses[i].Category = strings.TrimSpace(category)
			return nil
		}
	}
	return fmt.Errorf("%s %q: %w", Expense, id, ErrNotFound)
}

// HasKey reports whether a bank row is already in the month, or was imported
// once and then deleted.
func (m *Month) HasKey(key string) bool {
	if key == "" {
		return false
	}
	for _, kind := range []Kind{Income, Expense} {
		for _, e := range m.Entries(kind) {
			if e.Key == key {
				return true
			}
		}
	}
	return slices.Contains(m.SkippedKeys, key)
}

// AddImported appends a bank transaction as an actual entry. It reports false
// and changes nothing when that row is already known to the month.
func (m *Month) AddImported(kind Kind, imp Imported) (string, bool) {
	if m.HasKey(imp.Key) {
		return "", false
	}
	e := Entry{
		ID:     m.newID(),
		Label:  imp.Label,
		Amount: imp.Amount,
		Date:   imp.Date,
		Payee:  imp.Payee,
		Source: imp.Source,
		Key:    imp.Key,
	}
	if kind == Expense {
		e.Category = imp.Category
	}
	*m.list(kind) = append(*m.list(kind), e)
	return e.ID, true
}

// ReplaceForecast turns a forecast entry into the actual that fulfils it. The
// entry keeps its id, label, recurring flag and category (taking the imported
// one only if it had none); its old amount is remembered as Planned.
func (m *Month) ReplaceForecast(kind Kind, id string, imp Imported) error {
	if m.HasKey(imp.Key) {
		return fmt.Errorf("%s %q: that bank row is already in %s", kind, id, m.Key)
	}
	entries := *m.list(kind)
	for i := range entries {
		if entries[i].ID != id {
			continue
		}
		e := &entries[i]
		if !e.IsForecast() {
			return fmt.Errorf("%s %q is not a forecast entry", kind, id)
		}
		e.Planned = e.Amount
		e.Amount = imp.Amount
		e.Date = imp.Date
		e.Payee = imp.Payee
		e.Source = imp.Source
		e.Key = imp.Key
		if kind == Expense && e.Category == "" {
			e.Category = imp.Category
		}
		return nil
	}
	return fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
}

// Remove deletes an entry. A deleted bank row is remembered so that importing
// the same file again does not restore it.
func (m *Month) Remove(kind Kind, id string) error {
	entries := *m.list(kind)
	for i, e := range entries {
		if e.ID == id {
			if e.Key != "" {
				m.SkippedKeys = append(m.SkippedKeys, e.Key)
			}
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
