package model

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/madalingiurca/budget/internal/money"
)

func TestKey(t *testing.T) {
	k := Key{2026, time.September}
	if got := k.String(); got != "2026-09" {
		t.Errorf("String() = %q", got)
	}
	if got := k.Label(); got != "September 2026" {
		t.Errorf("Label() = %q", got)
	}

	parsed, err := ParseKey("2026-09")
	if err != nil || parsed != k {
		t.Errorf("ParseKey = %v, %v", parsed, err)
	}
	for _, bad := range []string{"", "2026", "2026-13", "2026-00", "26-09", "2026-9", "abcd-09"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) succeeded, want error", bad)
		}
	}
}

func TestKeyShiftCrossesYears(t *testing.T) {
	if got := (Key{2026, time.December}).Next(); got != (Key{2027, time.January}) {
		t.Errorf("December 2026 next = %v", got)
	}
	if got := (Key{2026, time.January}).Prev(); got != (Key{2025, time.December}) {
		t.Errorf("January 2026 prev = %v", got)
	}
	if got := (Key{2026, time.September}).Shift(-14); got != (Key{2025, time.July}) {
		t.Errorf("Shift(-14) = %v", got)
	}
	// Shifting into a shorter month must not spill into the next one.
	if got := (Key{2026, time.January}).Shift(1); got != (Key{2026, time.February}) {
		t.Errorf("January 2026 + 1 = %v", got)
	}
}

func TestKeyCompare(t *testing.T) {
	sep, oct, nextYear := Key{2026, time.September}, Key{2026, time.October}, Key{2027, time.January}
	if !sep.Before(oct) || !oct.Before(nextYear) || !nextYear.After(sep) {
		t.Error("ordering is wrong")
	}
	if sep.Compare(sep) != 0 {
		t.Error("a key should equal itself")
	}
}

func TestKeyJSON(t *testing.T) {
	k := Key{2026, time.September}
	b, err := json.Marshal(k)
	if err != nil || string(b) != `"2026-09"` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	var got Key
	if err := json.Unmarshal(b, &got); err != nil || got != k {
		t.Errorf("unmarshal = %v, %v", got, err)
	}
	if err := json.Unmarshal([]byte(`"2026-13"`), &got); err == nil {
		t.Error("unmarshal of month 13 succeeded, want error")
	}
}

func sample() *Month {
	m := New(Key{2026, time.September})
	m.Add(Income, "Salary", money.FromLei(12500), true)
	m.Add(Income, "Freelance", money.FromLei(2000), false)
	m.Add(Expense, "Rent", money.FromLei(2800), true)
	m.Add(Expense, "Utilities", money.FromLei(450), true)
	m.Add(Expense, "Car insurance", money.FromLei(1450), false)
	return m
}

func TestTotals(t *testing.T) {
	m := sample()
	if got := m.TotalIncome(); got != money.FromLei(14500) {
		t.Errorf("income = %s", got)
	}
	if got := m.TotalExpenses(); got != money.FromLei(4700) {
		t.Errorf("expenses = %s", got)
	}
	if got := m.Left(); got != money.FromLei(9800) {
		t.Errorf("left = %s", got)
	}
	if pct, ok := m.LeftShare(); !ok || pct != 67 {
		t.Errorf("LeftShare = %d, %v; want 67, true", pct, ok)
	}
}

func TestLeftShareWithoutIncome(t *testing.T) {
	m := New(Key{2026, time.September})
	if _, ok := m.LeftShare(); ok {
		t.Error("LeftShare should report false when there is no income")
	}
	m.Add(Expense, "Rent", money.FromLei(2800), true)
	if _, ok := m.LeftShare(); ok {
		t.Error("LeftShare should still report false with expenses but no income")
	}
	if got := m.Left(); got != money.FromLei(-2800) {
		t.Errorf("left = %s, want -2.800,00", got)
	}
}

func TestEmptyAndCount(t *testing.T) {
	m := New(Key{2026, time.September})
	if !m.IsEmpty() || m.Count() != 0 {
		t.Error("a new month should be empty")
	}
	if s := sample(); s.IsEmpty() || s.Count() != 5 {
		t.Errorf("sample month: empty=%v count=%d", s.IsEmpty(), s.Count())
	}
}

func TestIDsAreUnique(t *testing.T) {
	m := New(Key{2026, time.September})
	seen := map[string]bool{}
	for i := range 200 {
		kind := Income
		if i%2 == 0 {
			kind = Expense
		}
		id := m.Add(kind, "x", money.FromLei(1), false)
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestUpdateAndRemove(t *testing.T) {
	m := sample()
	rent := m.Entries(Expense)[0]

	if err := m.Update(Expense, rent.ID, "Rent", money.FromLei(3000), true); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, ok := m.Find(Expense, rent.ID)
	if !ok || got.Amount != money.FromLei(3000) {
		t.Errorf("after update: %+v", got)
	}

	if err := m.SetRecurring(Expense, rent.ID, false); err != nil {
		t.Fatalf("SetRecurring: %v", err)
	}
	if got, _ := m.Find(Expense, rent.ID); got.Recurring {
		t.Error("entry should no longer be recurring")
	}

	if err := m.Remove(Expense, rent.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := m.Find(Expense, rent.ID); ok {
		t.Error("removed entry is still there")
	}
	if len(m.Entries(Expense)) != 2 {
		t.Errorf("expenses left = %d, want 2", len(m.Entries(Expense)))
	}
	// The remaining entries must keep their order and values.
	if m.Entries(Expense)[0].Label != "Utilities" || m.Entries(Expense)[1].Label != "Car insurance" {
		t.Errorf("order after remove: %+v", m.Entries(Expense))
	}
}

func TestMissingEntry(t *testing.T) {
	m := sample()
	for _, err := range []error{
		m.Update(Expense, "nope", "x", 0, false),
		m.SetRecurring(Income, "nope", true),
		m.Remove(Income, "nope"),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	}
}

// Remove must not scribble over the slice a caller is still holding.
func TestRemoveDoesNotAliasOldSlice(t *testing.T) {
	m := sample()
	before := m.Entries(Expense)
	labels := []string{before[0].Label, before[1].Label, before[2].Label}

	if err := m.Remove(Expense, before[0].ID); err != nil {
		t.Fatal(err)
	}
	for i, want := range labels {
		if before[i].Label != want {
			t.Errorf("previously held slice mutated at %d: %q, want %q", i, before[i].Label, want)
		}
	}
}

func TestSeedFrom(t *testing.T) {
	prev := sample()
	next := SeedFrom(prev, prev.Key.Next())

	if next.Key != (Key{2026, time.October}) {
		t.Errorf("seeded key = %v", next.Key)
	}
	if next.Version != Version {
		t.Errorf("seeded version = %d", next.Version)
	}

	if got := len(next.Entries(Income)); got != 1 {
		t.Errorf("seeded income entries = %d, want 1 (only Salary recurs)", got)
	}
	if got := len(next.Entries(Expense)); got != 2 {
		t.Errorf("seeded expense entries = %d, want 2 (Rent and Utilities recur)", got)
	}
	if next.Entries(Income)[0].Label != "Salary" {
		t.Errorf("seeded income = %+v", next.Entries(Income))
	}
	if got := next.Left(); got != money.FromLei(9250) {
		t.Errorf("seeded left = %s, want 9.250,00", got)
	}

	// Fresh ids, and editing the copy must not reach back into the original.
	for _, e := range next.Entries(Expense) {
		if _, clash := prev.Find(Expense, e.ID); clash {
			t.Errorf("seeded entry reused id %q", e.ID)
		}
	}
	rentID := next.Entries(Expense)[0].ID
	if err := next.Update(Expense, rentID, "Rent", money.FromLei(9999), true); err != nil {
		t.Fatal(err)
	}
	if prev.Entries(Expense)[0].Amount != money.FromLei(2800) {
		t.Error("editing the seeded month changed the previous month")
	}
}

func TestSeedFromNothing(t *testing.T) {
	m := SeedFrom(nil, Key{2026, time.September})
	if !m.IsEmpty() {
		t.Error("seeding from no previous month should give an empty month")
	}
	if m.Income == nil || m.Expenses == nil {
		t.Error("entry lists should be empty, not nil, so they marshal as []")
	}
}

// A month with nothing recurring hands nothing to the next one.
func TestSeedFromMonthWithNoRecurring(t *testing.T) {
	prev := New(Key{2026, time.September})
	prev.Add(Income, "One-off gig", money.FromLei(500), false)
	if got := SeedFrom(prev, prev.Key.Next()); !got.IsEmpty() {
		t.Errorf("seeded month should be empty, has %d entries", got.Count())
	}
}

func TestMovingAnEntryBetweenMonths(t *testing.T) {
	nov := New(Key{2026, time.November})
	dec := New(Key{2026, time.December})
	id, _ := nov.AddImported(Income, Imported{Label: "ACME SRL", Amount: money.FromLei(15342), Date: "2026-11-27",
		Payee: "ACME SRL", Source: SourceING, Key: "k1"})

	e, err := nov.Take(Income, id)
	if err != nil || len(nov.Income) != 0 {
		t.Fatalf("take: %v, %d left", err, len(nov.Income))
	}
	if nov.HasKey("k1") {
		t.Error("a moved row is remembered as deleted in the month it left")
	}
	dec.SkippedKeys = []string{"k1"}
	newID, err := dec.Put(Income, e)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := dec.Find(Income, newID)
	if !ok || got.Date != "2026-11-27" || got.Key != "k1" || got.Amount != money.FromLei(15342) {
		t.Errorf("moved = %+v", got)
	}
	if dec.SkippedKeys != nil {
		t.Errorf("skipped keys = %v", dec.SkippedKeys)
	}
	if _, err := dec.Put(Income, e); err == nil {
		t.Error("the same bank row went into a month twice")
	}
	if _, err := nov.Take(Income, id); err == nil {
		t.Error("took an entry that is gone")
	}
}

func TestTakingAReplacedForecastLeavesThePlan(t *testing.T) {
	oct := New(Key{2026, time.October})
	id := oct.Add(Income, "Salary", money.FromLei(15000), true)
	if err := oct.ReplaceForecast(Income, id, Imported{Label: "ACME SRL", Amount: money.FromLei(15342),
		Date: "2026-10-30", Payee: "ACME SRL", Source: SourceING, Key: "k1"}); err != nil {
		t.Fatal(err)
	}
	row, err := oct.Take(Income, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Recurring || row.Planned != 0 || row.Key != "k1" || row.Amount != money.FromLei(15342) || row.Date != "2026-10-30" {
		t.Errorf("row = %+v", row)
	}
	plan, ok := oct.Find(Income, id)
	if !ok || !plan.IsForecast() || plan.Amount != money.FromLei(15000) || !plan.Recurring || plan.Label != "Salary" || plan.Date != "" {
		t.Errorf("plan = %+v", plan)
	}
	if oct.HasKey("k1") {
		t.Error("the row is still in October")
	}
}
