package model

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizePayee(t *testing.T) {
	for in, want := range map[string]string{
		"  Bolt  ":              "BOLT",
		"carrefour   lujerului": "CARREFOUR LUJERULUI",
		"":                      "",
		"Mega\tImage\n":         "MEGA IMAGE",
	} {
		if got := NormalizePayee(in); got != want {
			t.Errorf("NormalizePayee(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCategoryFor(t *testing.T) {
	r := NewRules()
	r.SetRule("bolt", "Transport")
	r.SetRule("Carrefour", "Groceries")
	r.SetRule("carrefour tabac", "Tobacco")

	for payee, want := range map[string]string{
		"BOLT.EU/O/2610":           "Transport",
		"Carrefour Lujerului":      "Groceries",
		"CARREFOUR TABAC MILITARI": "Tobacco", // the longer pattern wins
	} {
		if got, ok := r.CategoryFor(payee); !ok || got != want {
			t.Errorf("CategoryFor(%q) = %q, %v; want %q", payee, got, ok, want)
		}
	}
	for _, payee := range []string{"Uber", ""} {
		if got, ok := r.CategoryFor(payee); ok {
			t.Errorf("CategoryFor(%q) = %q, want no match", payee, got)
		}
	}
}

// Only money moving between the user's own ING and Revolut accounts is a
// transfer out of the box. Round ups count as spending until the user says
// otherwise.
func TestDefaultRules(t *testing.T) {
	r := NewRules()
	for _, payee := range []string{"Revolut**1234* IE Dublin", "Top-up by *1234"} {
		if got, _ := r.CategoryFor(payee); got != Transfers {
			t.Errorf("%q is not a transfer by default (got %q)", payee, got)
		}
	}
	for _, payee := range []string{"Round Up", "Revolut EUR"} {
		if got, ok := r.CategoryFor(payee); ok {
			t.Errorf("%q is filed under %q by default, want uncategorised", payee, got)
		}
	}
}

func TestSetRuleReplacesAndListsCategory(t *testing.T) {
	r := NewRules()
	r.SetRule("Bolt", "Transport")
	r.SetRule(" bolt ", "transport") // same pattern, same category in another case
	n := 0
	for _, rule := range r.Rules {
		if rule.Match == "BOLT" {
			n++
			if rule.Category != "Transport" {
				t.Errorf("category = %q, want the first spelling", rule.Category)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d rules for BOLT, want 1", n)
	}
	r.SetRule("Bolt", "Taxi")
	if got, _ := r.CategoryFor("bolt"); got != "Taxi" {
		t.Errorf("rule was not replaced: %q", got)
	}
	if got := strings.Join(r.SortedCategories(), "|"); got != "Taxi|Transport|Transfers" {
		t.Errorf("categories = %q", got)
	}
}

func TestAddCategory(t *testing.T) {
	r := NewRules()
	if got := r.AddCategory("  Savings "); got != "Savings" {
		t.Errorf("got %q", got)
	}
	if got := r.AddCategory("SAVINGS"); got != "Savings" {
		t.Errorf("case variant created a second category: %q", got)
	}
	if got := r.AddCategory("   "); got != "" {
		t.Errorf("blank name stored as %q", got)
	}
	if len(r.Categories) != 2 {
		t.Errorf("categories = %v", r.Categories)
	}
}

func TestRenameCategory(t *testing.T) {
	r := NewRules()
	r.SetRule("Bolt", "Transport")
	r.SetRule("Uber", "Taxi")

	if !r.RenameCategory("Transport", "Travel") {
		t.Fatal("rename reported no change")
	}
	if got, _ := r.CategoryFor("Bolt"); got != "Travel" {
		t.Errorf("rule still points at %q", got)
	}

	// Renaming onto an existing category merges.
	r.RenameCategory("Taxi", "Travel")
	if got := strings.Join(r.SortedCategories(), "|"); got != "Travel|Transfers" {
		t.Errorf("categories after merge = %q", got)
	}

	if r.RenameCategory(Transfers, "Moves") {
		t.Error("the built-in Transfers category was renamed")
	}
	if r.RenameCategory("Travel", "  ") || r.RenameCategory("Travel", "Travel") {
		t.Error("a no-op rename reported a change")
	}
}

func TestLinks(t *testing.T) {
	r := NewRules()
	if _, ok := r.LinkFor("Rata Credit"); ok {
		t.Error("link found before any was set")
	}
	r.SetLink("Rata Credit", "Mortgage")
	r.SetLink("rata  credit", "Home loan") // replaces
	if got, ok := r.LinkFor("RATA CREDIT"); !ok || got != "Home loan" {
		t.Errorf("LinkFor = %q, %v", got, ok)
	}
	if len(r.Links) != 1 {
		t.Errorf("links = %+v", r.Links)
	}
	r.SetLink("", "x")
	r.SetLink("x", " ")
	if len(r.Links) != 1 {
		t.Errorf("blank link stored: %+v", r.Links)
	}
}

func TestApplyLeavesCategorisedEntriesAlone(t *testing.T) {
	m := New(Key{2026, time.October})
	m.AddImported(Expense, bankRow("k1", "Bolt", lei(20), ""))
	m.AddImported(Expense, bankRow("k2", "Bolt", lei(30), "Work travel")) // filed by hand
	m.AddImported(Expense, bankRow("k3", "Uber", lei(40), ""))
	m.Add(Expense, "Bolt budget", lei(100), false) // a forecast has no payee

	if n := m.Apply(Rule{Match: "BOLT", Category: "Transport"}, nil); n != 1 {
		t.Errorf("applied to %d entries, want 1", n)
	}
	got := []string{m.Expenses[0].Category, m.Expenses[1].Category, m.Expenses[2].Category, m.Expenses[3].Category}
	if strings.Join(got, "|") != "Transport|Work travel||" {
		t.Errorf("categories = %q", got)
	}
	if n := m.Apply(Rule{Match: "", Category: "Everything"}, nil); n != 0 {
		t.Errorf("an empty pattern matched %d entries", n)
	}
}

func TestMonthRenameCategory(t *testing.T) {
	m := New(Key{2026, time.October})
	m.AddImported(Expense, bankRow("k1", "Bolt", lei(20), "Transport"))
	m.AddImported(Expense, bankRow("k2", "Lidl", lei(30), "Groceries"))
	if n := m.RenameCategory("Transport", "Travel"); n != 1 || m.Expenses[0].Category != "Travel" {
		t.Errorf("renamed %d, category %q", n, m.Expenses[0].Category)
	}
}

func TestSetLinkIfNew(t *testing.T) {
	r := NewRules()
	if !r.SetLinkIfNew("Rata Credit", "Mortgage") {
		t.Error("a new link reported no change")
	}
	if r.SetLinkIfNew("rata credit", "Mortgage") {
		t.Error("the same link reported a change")
	}
	if !r.SetLinkIfNew("Rata Credit", "Home loan") {
		t.Error("a changed link reported no change")
	}
	if r.SetLinkIfNew("", "x") || r.SetLinkIfNew("x", "") {
		t.Error("a blank link reported a change")
	}
	if len(r.Links) != 1 {
		t.Errorf("links = %+v", r.Links)
	}
}

// Changing a payee's rule moves the entries the old rule filed, but never the
// ones filed by hand, and never past a more specific rule.
func TestApplyMovesWhatAnEarlierRuleFiled(t *testing.T) {
	r := NewRules()
	r.SetRule("Round Up", Transfers)
	m := New(Key{2026, time.October})
	auto := bankRow("k1", "Round Up", lei(2), Transfers)
	auto.Auto = true
	m.AddImported(Expense, auto)
	manual := bankRow("k2", "Round Up", lei(3), "Gifts")
	m.AddImported(Expense, manual)

	rule := r.SetRule("Round Up", "Savings")
	if n := m.Apply(rule, r); n != 1 {
		t.Errorf("moved %d entries, want 1", n)
	}
	if m.Expenses[0].Category != "Savings" || !m.Expenses[0].Auto || m.Expenses[1].Category != "Gifts" {
		t.Errorf("categories = %q / %q", m.Expenses[0].Category, m.Expenses[1].Category)
	}
	if n := m.Apply(rule, r); n != 0 {
		t.Errorf("applying again changed %d entries", n)
	}

	// A longer rule keeps its own entries.
	r.SetRule("Carrefour Tabac", "Tobacco")
	tob := bankRow("k3", "Carrefour Tabac", lei(30), "Tobacco")
	tob.Auto = true
	m.AddImported(Expense, tob)
	if n := m.Apply(r.SetRule("Carrefour", "Groceries"), r); n != 0 || m.Expenses[2].Category != "Tobacco" {
		t.Errorf("a shorter rule took %d entries; category %q", n, m.Expenses[2].Category)
	}

	// Picking by hand takes the entry out of the rules' reach.
	m.SetCategory(m.Expenses[0].ID, "Fun")
	if m.Expenses[0].Auto {
		t.Error("a hand-picked category is still marked as a rule's")
	}
}

func TestIncomeTransfers(t *testing.T) {
	m := New(Key{2026, time.October})
	topup := bankRow("k1", "Top-up by *1234", lei(500), Transfers)
	topup.Auto = true
	m.AddImported(Income, topup)
	m.AddImported(Income, bankRow("k2", "Friend", lei(50), "Food")) // a non-transfer category is dropped
	if m.Income[0].Category != Transfers || m.Income[1].Category != "" {
		t.Errorf("income categories = %q / %q", m.Income[0].Category, m.Income[1].Category)
	}
	if got := m.TotalIncome(); got != lei(50) {
		t.Errorf("income = %v, want the top-up left out", got)
	}

	// A rule made later catches income from the user's own account too.
	m2 := New(Key{2026, time.October})
	m2.AddImported(Income, bankRow("k1", "Savings account", lei(300), ""))
	r := NewRules()
	if n := m2.Apply(r.SetRule("Savings account", Transfers), r); n != 1 || m2.Income[0].Category != Transfers {
		t.Errorf("moved %d, category %q", n, m2.Income[0].Category)
	}
	// Any other rule leaves income alone.
	m3 := New(Key{2026, time.October})
	m3.AddImported(Income, bankRow("k1", "Bolt refund", lei(10), ""))
	if n := m3.Apply(r.SetRule("Bolt", "Transport"), r); n != 0 || m3.Income[0].Category != "" {
		t.Errorf("income took a spending category: %q", m3.Income[0].Category)
	}
}

func TestNextMonthRules(t *testing.T) {
	r := NewRules()
	if r.CountsForNextMonth("ACME SRL", 30) {
		t.Fatal("no rule yet")
	}
	if !r.SetNextMonth("Acme  srl", 28) {
		t.Fatal("first rule not stored")
	}
	if r.SetNextMonth("ACME SRL", 30) {
		t.Error("a later day narrowed the rule")
	}
	if !r.SetNextMonth("ACME SRL", 27) || r.NextMonth[0].FromDay != 27 || len(r.NextMonth) != 1 {
		t.Errorf("rules = %+v", r.NextMonth)
	}
	for day, want := range map[int]bool{2: false, 26: false, 27: true, 31: true} {
		if got := r.CountsForNextMonth("ACME SRL BUCURESTI", day); got != want {
			t.Errorf("day %d: %v, want %v", day, got, want)
		}
	}
	if r.CountsForNextMonth("OTHER", 30) {
		t.Error("another payee moved")
	}
	if !r.DropNextMonth("ACME SRL BUCURESTI") || r.NextMonth != nil || r.DropNextMonth("ACME SRL") {
		t.Errorf("drop left %+v", r.NextMonth)
	}
}

func TestPaidBackCategories(t *testing.T) {
	r := NewRules()
	if r.IsPaidBack("PayAndReceiveLater") {
		t.Fatal("a fresh category is paid back")
	}
	if !r.TogglePaidBack("PayAndReceiveLater") || !r.IsPaidBack("PayAndReceiveLater") {
		t.Fatal("toggle on")
	}
	if r.TogglePaidBack(Transfers) || r.IsPaidBack(Transfers) {
		t.Error("Transfers became paid back")
	}
	r.AddCategory("PayAndReceiveLater")
	r.RenameCategory("PayAndReceiveLater", "Reimbursable")
	if r.IsPaidBack("PayAndReceiveLater") || !r.IsPaidBack("Reimbursable") {
		t.Errorf("rename lost the flag: %v", r.PaidBack)
	}
	if r.TogglePaidBack("Reimbursable") || r.PaidBack != nil {
		t.Errorf("toggle off left %v", r.PaidBack)
	}
}
