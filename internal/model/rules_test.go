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

	if n := m.Apply(Rule{Match: "BOLT", Category: "Transport"}); n != 1 {
		t.Errorf("applied to %d entries, want 1", n)
	}
	got := []string{m.Expenses[0].Category, m.Expenses[1].Category, m.Expenses[2].Category, m.Expenses[3].Category}
	if strings.Join(got, "|") != "Transport|Work travel||" {
		t.Errorf("categories = %q", got)
	}
	if n := m.Apply(Rule{Match: "", Category: "Everything"}); n != 0 {
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
