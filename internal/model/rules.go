package model

import (
	"slices"
	"strings"
)

// RulesVersion is the categories file format this build reads and writes.
const RulesVersion = 1

// Rule files every expense whose payee contains Match under Category.
type Rule struct {
	Match    string `json:"match"`
	Category string `json:"category"`
}

// Link remembers that bank rows from a payee fulfil the forecast entry with a
// given label, so the same replacement can be proposed next month.
type Link struct {
	Match string `json:"match"`
	Label string `json:"label"`
}

// Rules is what the user has taught the app, shared by every month: the
// categories they use, which payee belongs where, and which bank rows stand in
// for which forecast entries.
type Rules struct {
	Version    int      `json:"version"`
	Categories []string `json:"categories"`
	Rules      []Rule   `json:"rules"`
	Links      []Link   `json:"links"`
}

// NewRules returns the rules a fresh install starts with: the Transfers
// category and the patterns that recognise money moving between the user's own
// ING and Revolut accounts.
func NewRules() *Rules {
	return &Rules{
		Version:    RulesVersion,
		Categories: []string{Transfers},
		Rules: []Rule{
			{Match: "REVOLUT**", Category: Transfers}, // ING card top-up of Revolut
			{Match: "TOP-UP BY", Category: Transfers}, // the same money arriving in Revolut
		},
		Links: []Link{},
	}
}

// NormalizePayee folds a payee to the form rules match on: upper case, single
// spaces, no surrounding blanks.
func NormalizePayee(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// CategoryFor returns the category of the rule that matches payee. When several
// match, the longest pattern wins, so "CARREFOUR TABAC" can override
// "CARREFOUR".
func (r *Rules) CategoryFor(payee string) (string, bool) {
	payee = NormalizePayee(payee)
	if payee == "" {
		return "", false
	}
	best := -1
	for i, rule := range r.Rules {
		if rule.Match == "" || !strings.Contains(payee, rule.Match) {
			continue
		}
		if best < 0 || len(rule.Match) > len(r.Rules[best].Match) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return r.Rules[best].Category, true
}

// Matches reports whether a rule applies to a payee.
func (rule Rule) Matches(payee string) bool {
	return rule.Match != "" && strings.Contains(NormalizePayee(payee), rule.Match)
}

// SetRule records that payees containing match belong to category, replacing
// any rule already stored for that pattern. It returns the rule as stored.
func (r *Rules) SetRule(match, category string) Rule {
	rule := Rule{Match: NormalizePayee(match), Category: r.AddCategory(category)}
	for i := range r.Rules {
		if r.Rules[i].Match == rule.Match {
			r.Rules[i] = rule
			return rule
		}
	}
	r.Rules = append(r.Rules, rule)
	return rule
}

// AddCategory makes sure a category is listed and returns the name as stored.
// Names differing only in case are the same category; the first spelling wins.
func (r *Rules) AddCategory(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for _, have := range r.Categories {
		if strings.EqualFold(have, name) {
			return have
		}
	}
	r.Categories = append(r.Categories, name)
	return name
}

// SortedCategories lists the categories by name with Transfers last.
func (r *Rules) SortedCategories() []string {
	out := slices.Clone(r.Categories)
	slices.SortFunc(out, func(a, b string) int {
		if ra, rb := categoryRank(a), categoryRank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return out
}

// RenameCategory renames a category in the list and in every rule. Transfers
// is built in and cannot be renamed; renaming onto an existing category merges
// the two. It reports whether anything changed.
func (r *Rules) RenameCategory(from, to string) bool {
	to = strings.TrimSpace(to)
	if from == "" || to == "" || from == to || from == Transfers {
		return false
	}
	changed := false
	kept := r.Categories[:0:0]
	seenTo := false
	for _, c := range r.Categories {
		if c == from {
			c, changed = to, true
		}
		if c == to {
			if seenTo {
				continue
			}
			seenTo = true
		}
		kept = append(kept, c)
	}
	r.Categories = kept
	for i := range r.Rules {
		if r.Rules[i].Category == from {
			r.Rules[i].Category, changed = to, true
		}
	}
	return changed
}

// LinkFor returns the forecast label that bank rows from payee have replaced
// before.
func (r *Rules) LinkFor(payee string) (string, bool) {
	payee = NormalizePayee(payee)
	if payee == "" {
		return "", false
	}
	best := -1
	for i, l := range r.Links {
		if l.Match == "" || !strings.Contains(payee, l.Match) {
			continue
		}
		if best < 0 || len(l.Match) > len(r.Links[best].Match) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return r.Links[best].Label, true
}

// SetLink remembers that rows from payee fulfil the forecast entry labelled
// label.
func (r *Rules) SetLink(payee, label string) {
	l := Link{Match: NormalizePayee(payee), Label: strings.TrimSpace(label)}
	if l.Match == "" || l.Label == "" {
		return
	}
	for i := range r.Links {
		if r.Links[i].Match == l.Match {
			r.Links[i] = l
			return
		}
	}
	r.Links = append(r.Links, l)
}

// Apply files every uncategorised expense that rule matches. Entries that
// already have a category were put there on purpose and are left alone. It
// returns how many entries changed.
func (m *Month) Apply(rule Rule) int {
	n := 0
	for i := range m.Expenses {
		e := &m.Expenses[i]
		if e.Category == "" && rule.Matches(e.Payee) {
			e.Category = rule.Category
			n++
		}
	}
	return n
}

// RenameCategory moves every expense from one category to another and reports
// how many moved.
func (m *Month) RenameCategory(from, to string) int {
	n := 0
	for i := range m.Expenses {
		if m.Expenses[i].Category == from {
			m.Expenses[i].Category = to
			n++
		}
	}
	return n
}

// SetLinkIfNew remembers a link and reports whether anything changed.
func (r *Rules) SetLinkIfNew(payee, label string) bool {
	if NormalizePayee(payee) == "" || strings.TrimSpace(label) == "" {
		return false
	}
	for _, l := range r.Links {
		if l.Match == NormalizePayee(payee) && l.Label == strings.TrimSpace(label) {
			return false
		}
	}
	r.SetLink(payee, label)
	return true
}
