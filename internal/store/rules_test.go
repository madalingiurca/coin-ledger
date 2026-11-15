package store

import (
	"os"
	"strings"
	"testing"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

func imported(key, payee string, lei int64, category string) model.Imported {
	return model.Imported{
		Label: payee, Amount: money.FromLei(lei), Date: "2026-10-02",
		Payee: model.NormalizePayee(payee), Source: model.SourceING, Key: key, Category: category,
	}
}

func TestLoadRulesDefaultsWhenMissing(t *testing.T) {
	s := newStore(t)
	r, err := s.LoadRules()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := r.CategoryFor("Top-up by *1234"); got != model.Transfers {
		t.Errorf("defaults are missing the transfer rules: %q", got)
	}
	if _, err := os.Stat(s.RulesPath()); err == nil {
		t.Error("reading the defaults wrote a file")
	}
}

func TestRulesRoundTrip(t *testing.T) {
	s := newStore(t)
	r, _ := s.LoadRules()
	r.SetRule("Bolt", "Transport")
	r.SetLink("Rata Credit", "Mortgage")
	if err := s.SaveRules(r); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadRules()
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := got.CategoryFor("bolt.eu"); c != "Transport" {
		t.Errorf("rule lost: %q", c)
	}
	if l, _ := got.LinkFor("rata credit"); l != "Mortgage" {
		t.Errorf("link lost: %q", l)
	}

	// The categories file must not be mistaken for a month.
	keys, err := s.Keys()
	if err != nil || len(keys) != 0 {
		t.Errorf("keys = %v, %v", keys, err)
	}
	entries, _ := os.ReadDir(s.Dir())
	if len(entries) != 1 || entries[0].Name() != "categories.json" {
		t.Errorf("directory = %v", entries)
	}
}

func TestLoadRulesDamagedIsAnError(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.RulesPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRules(); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadRulesRejectsNewerFormat(t *testing.T) {
	s := newStore(t)
	os.WriteFile(s.RulesPath(), []byte(`{"version": 99}`), 0o600)
	if _, err := s.LoadRules(); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadRulesHandEdited(t *testing.T) {
	s := newStore(t)
	raw := `{"version": 1, "rules": [{"match": "bolt", "category": "Transport"}]}`
	os.WriteFile(s.RulesPath(), []byte(raw), 0o600)
	r, err := s.LoadRules()
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := r.CategoryFor("BOLT.EU"); !ok || c != "Transport" {
		t.Errorf("lower-case hand-edited pattern does not match: %q, %v", c, ok)
	}
	if r.Links == nil || !strings.Contains(strings.Join(r.Categories, "|"), model.Transfers) {
		t.Errorf("missing lists were not filled in: %+v", r)
	}
}

func TestImportedEntryRoundTrip(t *testing.T) {
	s := newStore(t)
	m := model.New(oct)
	id := m.Add(model.Expense, "Mortgage", money.FromLei(2300), true)
	m.SetCategory(id, "Loan")
	if err := m.ReplaceForecast(model.Expense, id, imported("k1", "Rata Credit", 2154, "")); err != nil {
		t.Fatal(err)
	}
	gone, _ := m.AddImported(model.Expense, imported("k2", "Bolt", 20, ""))
	m.Remove(model.Expense, gone)
	if err := s.Save(m); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load(oct)
	if err != nil {
		t.Fatal(err)
	}
	e := got.Expenses[0]
	if e.Planned != money.FromLei(2300) || e.Amount != money.FromLei(2154) || e.Category != "Loan" ||
		e.Payee != "RATA CREDIT" || e.Source != model.SourceING || e.Key != "k1" || e.Date != "2026-10-02" {
		t.Errorf("entry = %+v", e)
	}
	if !got.HasKey("k2") {
		t.Error("the deleted bank row was forgotten on disk")
	}
}

func TestApplyRuleAcrossMonths(t *testing.T) {
	s := newStore(t)
	for _, k := range []model.Key{sep, oct} {
		m := model.New(k)
		m.AddImported(model.Expense, imported(k.String()+"-1", "Bolt", 20, ""))
		m.AddImported(model.Expense, imported(k.String()+"-2", "Bolt", 30, "Work travel"))
		m.AddImported(model.Expense, imported(k.String()+"-3", "Lidl", 40, ""))
		if err := s.Save(m); err != nil {
			t.Fatal(err)
		}
	}
	untouched := model.New(nov)
	untouched.AddImported(model.Expense, imported("n1", "Lidl", 40, ""))
	s.Save(untouched)
	before, _ := os.ReadFile(s.Path(nov))
	os.WriteFile(s.Path(model.Key{Year: 2026, Month: 8}), []byte("{broken"), 0o600)

	sweep, err := s.ApplyRule(model.Rule{Match: "BOLT", Category: "Transport"})
	if err != nil {
		t.Fatal(err)
	}
	if sweep.Entries != 2 || sweep.Months != 2 || len(sweep.Skipped) != 1 {
		t.Errorf("sweep = %+v", sweep)
	}
	for _, k := range []model.Key{sep, oct} {
		m, _ := s.Load(k)
		got := m.Expenses[0].Category + "|" + m.Expenses[1].Category + "|" + m.Expenses[2].Category
		if got != "Transport|Work travel|" {
			t.Errorf("%s categories = %q", k, got)
		}
	}
	// A month with nothing to change is not rewritten, and a damaged one is
	// never touched.
	after, _ := os.ReadFile(s.Path(nov))
	if string(before) != string(after) {
		t.Error("a month with no matching entries was rewritten")
	}
	broken, _ := os.ReadFile(s.Path(model.Key{Year: 2026, Month: 8}))
	if string(broken) != "{broken" {
		t.Error("a damaged month was overwritten")
	}
}

func TestRenameCategoryAcrossMonths(t *testing.T) {
	s := newStore(t)
	m := model.New(oct)
	m.AddImported(model.Expense, imported("k1", "Bolt", 20, "Transport"))
	s.Save(m)

	sweep, err := s.RenameCategory("Transport", "Travel")
	if err != nil || sweep.Entries != 1 || sweep.Months != 1 {
		t.Fatalf("sweep = %+v, %v", sweep, err)
	}
	got, _ := s.Load(oct)
	if got.Expenses[0].Category != "Travel" {
		t.Errorf("category = %q", got.Expenses[0].Category)
	}
}

// Summaries feed the history screen; transfers must not distort them.
func TestSummariesIgnoreTransfers(t *testing.T) {
	s := newStore(t)
	m := model.New(oct)
	m.Add(model.Income, "Salary", money.FromLei(10000), true)
	m.AddImported(model.Expense, imported("k1", "Revolut**1234*", 500, model.Transfers))
	m.AddImported(model.Expense, imported("k2", "Lidl", 100, "Groceries"))
	s.Save(m)

	sums, err := s.Summaries(0)
	if err != nil || len(sums) != 1 {
		t.Fatalf("summaries = %+v, %v", sums, err)
	}
	if sums[0].Expenses != money.FromLei(100) || sums[0].Left != money.FromLei(9900) {
		t.Errorf("summary = %+v", sums[0])
	}
}
