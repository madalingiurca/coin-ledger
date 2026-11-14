package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

var (
	sep = model.Key{Year: 2026, Month: time.September}
	oct = model.Key{Year: 2026, Month: time.October}
	nov = model.Key{Year: 2026, Month: time.November}
)

func september() *model.Month {
	m := model.New(sep)
	m.Add(model.Income, "Salary", money.FromLei(12500), true)
	m.Add(model.Income, "Freelance", money.FromLei(2000), false)
	m.Add(model.Expense, "Rent", money.FromLei(2800), true)
	m.Add(model.Expense, "Car insurance", money.FromLei(1450), false)
	return m
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newStore(t)
	want := september()
	if err := s.Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Load(sep)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Key != sep || got.Version != model.Version {
		t.Errorf("key=%v version=%d", got.Key, got.Version)
	}
	if got.TotalIncome() != want.TotalIncome() || got.Left() != want.Left() {
		t.Errorf("totals differ: %s / %s", got.TotalIncome(), got.Left())
	}
	for i, e := range got.Entries(model.Expense) {
		if e != want.Entries(model.Expense)[i] {
			t.Errorf("expense %d = %+v, want %+v", i, e, want.Entries(model.Expense)[i])
		}
	}
	if got.UpdatedAt.IsZero() {
		t.Error("Save should stamp UpdatedAt")
	}
}

func TestSavedFileIsReadable(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path(sep))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	// Amounts must be plain decimals a person can edit, not integer bani.
	for _, want := range []string{`"month": "2026-09"`, `"amount": 12500.00`, `"amount": 2800.00`, `"recurring": true`} {
		if !strings.Contains(text, want) {
			t.Errorf("saved file is missing %s\n%s", want, text)
		}
	}
	if !strings.HasSuffix(text, "\n") {
		t.Error("saved file should end with a newline")
	}
}

func TestSaveIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	s := newStore(t)
	for range 3 {
		if err := s.Save(september()); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "2026-09.json" {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want just 2026-09.json", names)
	}
}

func TestLoadMissingMonth(t *testing.T) {
	s := newStore(t)
	if _, err := s.Load(sep); !errors.Is(err, ErrNoMonth) {
		t.Errorf("got %v, want ErrNoMonth", err)
	}
	if s.Exists(sep) {
		t.Error("Exists should be false before anything is saved")
	}
}

func TestLoadDamagedMonth(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.Path(sep), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load(sep)
	if err == nil {
		t.Fatal("loading damaged file succeeded, want error")
	}
	if !strings.Contains(err.Error(), "2026-09.json") {
		t.Errorf("error should name the file, got: %v", err)
	}
	// A damaged file must survive: reading it is never allowed to destroy it.
	if _, statErr := os.Stat(s.Path(sep)); statErr != nil {
		t.Error("damaged file was removed")
	}
}

func TestLoadRejectsNewerFormat(t *testing.T) {
	s := newStore(t)
	raw := `{"version": 99, "month": "2026-09", "income": [], "expenses": []}`
	if err := os.WriteFile(s.Path(sep), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(sep); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Errorf("got %v, want a newer-format error", err)
	}
}

func TestKeysAreNewestFirstAndIgnoreStrangers(t *testing.T) {
	s := newStore(t)
	for _, k := range []model.Key{sep, nov, oct} {
		if err := s.Save(model.New(k)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"notes.txt", "README.md", "2026-13.json", "backup.json"} {
		if err := os.WriteFile(filepath.Join(s.Dir(), name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	keys, err := s.Keys()
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Key{nov, oct, sep}
	if len(keys) != len(want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("keys[%d] = %v, want %v", i, keys[i], want[i])
		}
	}
}

func TestGetExistingMonth(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}
	res, err := s.Get(sep)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OnDisk || res.SeededFrom != nil {
		t.Errorf("OnDisk=%v SeededFrom=%v; want true, nil", res.OnDisk, res.SeededFrom)
	}
	if res.Month.Count() != 4 {
		t.Errorf("loaded %d entries, want 4", res.Month.Count())
	}
}

func TestGetSeedsFromPreviousMonth(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}

	res, err := s.Get(oct)
	if err != nil {
		t.Fatal(err)
	}
	if res.OnDisk {
		t.Error("a seeded month should not be on disk yet")
	}
	if res.SeededFrom == nil || *res.SeededFrom != sep {
		t.Errorf("SeededFrom = %v, want 2026-09", res.SeededFrom)
	}
	if got := res.Month.Count(); got != 2 {
		t.Errorf("seeded %d entries, want 2 (Salary and Rent recur)", got)
	}
	if got := res.Month.Left(); got != money.FromLei(9700) {
		t.Errorf("seeded left = %s, want 9.700,00", got)
	}
	// Browsing must not create files.
	if s.Exists(oct) {
		t.Error("Get wrote a file for a month that was only visited")
	}
}

func TestGetSkipsToTheNearestEarlierMonth(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}
	// November is empty on disk; December should seed from November, not
	// reach back to September.
	if err := s.Save(model.New(nov)); err != nil {
		t.Fatal(err)
	}

	res, err := s.Get(model.Key{Year: 2026, Month: time.December})
	if err != nil {
		t.Fatal(err)
	}
	if res.SeededFrom == nil || *res.SeededFrom != nov {
		t.Errorf("SeededFrom = %v, want 2026-11", res.SeededFrom)
	}
	if !res.Month.IsEmpty() {
		t.Errorf("seeded from an empty month, got %d entries", res.Month.Count())
	}
}

func TestGetNeverSeedsFromTheFuture(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}
	res, err := s.Get(model.Key{Year: 2026, Month: time.August})
	if err != nil {
		t.Fatal(err)
	}
	if res.SeededFrom != nil {
		t.Errorf("SeededFrom = %v; earlier months must not inherit from later ones", res.SeededFrom)
	}
	if !res.Month.IsEmpty() {
		t.Error("August should have come up empty")
	}
}

func TestGetStepsOverADamagedMonth(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(oct), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := s.Get(nov)
	if err != nil {
		t.Fatalf("a damaged earlier month should not block opening a new one: %v", err)
	}
	if res.SeededFrom == nil || *res.SeededFrom != sep {
		t.Errorf("SeededFrom = %v, want 2026-09", res.SeededFrom)
	}
	if res.Warning == nil || !strings.Contains(res.Warning.Error(), "2026-10") {
		t.Errorf("Warning = %v; should name the unreadable month", res.Warning)
	}
}

func TestGetDamagedMonthItselfIsFatal(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.Path(sep), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(sep); err == nil {
		t.Error("opening a damaged month should report the error, not silently reseed")
	}
}

func TestGetOnEmptyStore(t *testing.T) {
	s := newStore(t)
	res, err := s.Get(sep)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Month.IsEmpty() || res.OnDisk || res.SeededFrom != nil || res.Warning != nil {
		t.Errorf("first ever run gave %+v", res)
	}
}

func TestSummaries(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}
	overspent := model.New(oct)
	overspent.Add(model.Income, "Salary", money.FromLei(12500), true)
	overspent.Add(model.Expense, "Holiday", money.FromLei(14000), false)
	if err := s.Save(overspent); err != nil {
		t.Fatal(err)
	}

	got, err := s.Summaries(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}
	if got[0].Key != oct || got[0].Left != money.FromLei(-1500) {
		t.Errorf("newest summary = %+v, want October at -1.500,00", got[0])
	}
	if got[1].Key != sep || got[1].Income != money.FromLei(14500) || got[1].Expenses != money.FromLei(4250) {
		t.Errorf("older summary = %+v", got[1])
	}

	if limited, err := s.Summaries(1); err != nil || len(limited) != 1 || limited[0].Key != oct {
		t.Errorf("Summaries(1) = %+v, %v", limited, err)
	}
}

func TestSummariesReportDamagedMonths(t *testing.T) {
	s := newStore(t)
	if err := s.Save(september()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(oct), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := s.Summaries(0)
	if err != nil {
		t.Fatalf("one damaged month should not fail the whole listing: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}
	if got[0].Key != oct || got[0].Err == nil {
		t.Errorf("damaged month summary = %+v, want an Err", got[0])
	}
	if got[1].Err != nil {
		t.Errorf("healthy month reported an error: %v", got[1].Err)
	}
}

// A hand-edited file using shorthand should still load.
func TestLoadHandEditedFile(t *testing.T) {
	s := newStore(t)
	raw := `{
	  "month": "2026-09",
	  "income":   [{"id": "a1", "label": "Salary", "amount": 12500, "recurring": true}],
	  "expenses": [{"id": "b2", "label": "Rent",   "amount": "2.800,50"}]
	}`
	if err := os.WriteFile(s.Path(sep), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := s.Load(sep)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := m.TotalIncome(); got != money.FromLei(12500) {
		t.Errorf("income = %s", got)
	}
	if got := m.TotalExpenses(); got != money.Amount(280050) {
		t.Errorf("expenses = %s, want 2.800,50", got)
	}
	if e, _ := m.Find(model.Expense, "b2"); e.Recurring {
		t.Error("a missing recurring field should read as false")
	}
}

// Guard the on-disk shape itself: it is the user's data, not an internal detail.
func TestFileShape(t *testing.T) {
	s := newStore(t)
	if err := s.Save(model.New(sep)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path(sep))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "month", "income", "expenses", "updated_at"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("saved file has no %q field", field)
		}
	}
	if string(raw["income"]) != "[]" {
		t.Errorf("empty income marshalled as %s, want []", raw["income"])
	}
}
