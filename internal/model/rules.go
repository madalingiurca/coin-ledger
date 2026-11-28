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
	// NextMonth lists payees whose late rows count for the month after.
	NextMonth []Shift `json:"next_month,omitempty"`
	// PaidBack lists the categories whose expenses are expected to be paid
	// back, so the ones no refund is linked to yet show as open.
	PaidBack []string `json:"paid_back,omitempty"`
}

// IsPaidBack reports whether expenses in category are expected to come back.
func (r *Rules) IsPaidBack(category string) bool {
	return category != "" && slices.Contains(r.PaidBack, category)
}

// TogglePaidBack flips whether a category's expenses are expected to be paid
// back and reports the new state. Transfers never are.
func (r *Rules) TogglePaidBack(category string) bool {
	if category == "" || category == Transfers {
		return false
	}
	if r.IsPaidBack(category) {
		r.PaidBack = slices.DeleteFunc(r.PaidBack, func(c string) bool { return c == category })
		if len(r.PaidBack) == 0 {
			r.PaidBack = nil
		}
		return false
	}
	r.PaidBack = append(r.PaidBack, category)
	return true
}

// Shift remembers that rows from a payee booked on FromDay or later belong to
// the next month: a salary paid early because the 1st falls on a weekend or a
// holiday.
type Shift struct {
	Match   string `json:"match"`
	FromDay int    `json:"from_day"`
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
	if r.IsPaidBack(from) {
		r.PaidBack = slices.DeleteFunc(r.PaidBack, func(c string) bool { return c == from || c == to })
		r.PaidBack = append(r.PaidBack, to)
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

// Apply files the expenses rule matches: the uncategorised ones, and the ones
// an earlier rule filed, so changing a payee's category moves its entries.
// Entries filed by hand stay where they were put, and so do entries a longer,
// more specific rule in all claims. It returns how many entries changed.
func (m *Month) Apply(rule Rule, all *Rules) int {
	n := 0
	for i := range m.Expenses {
		e := &m.Expenses[i]
		if (e.Category != "" && !e.Auto) || !rule.Matches(e.Payee) {
			continue
		}
		if all != nil {
			if best, _ := all.CategoryFor(e.Payee); best != rule.Category {
				continue
			}
		}
		if e.Category != rule.Category || !e.Auto {
			e.Category, e.Auto = rule.Category, true
			n++
		}
	}
	// Money from the user's own account is a transfer on the income side too.
	for i := range m.Income {
		e := &m.Income[i]
		if (e.Category != "" && !e.Auto) || !rule.Matches(e.Payee) {
			continue
		}
		if all != nil {
			if best, _ := all.CategoryFor(e.Payee); best != rule.Category {
				continue
			}
		}
		want := ""
		if rule.Category == Transfers {
			want = Transfers
		}
		if e.Category != want {
			e.Category, e.Auto = want, want != ""
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

// CountsForNextMonth reports whether a row from payee booked on day of its
// month belongs to the month after.
func (r *Rules) CountsForNextMonth(payee string, day int) bool {
	payee = NormalizePayee(payee)
	if payee == "" {
		return false
	}
	for _, s := range r.NextMonth {
		if s.Match != "" && strings.Contains(payee, s.Match) && day >= s.FromDay {
			return true
		}
	}
	return false
}

// SetNextMonth remembers that rows from payee booked on day or later count for
// the next month. An earlier day already remembered is kept, so the rule
// widens to the earliest early payment seen. It reports whether anything
// changed.
func (r *Rules) SetNextMonth(payee string, day int) bool {
	match := NormalizePayee(payee)
	if match == "" || day < 1 {
		return false
	}
	for i := range r.NextMonth {
		if r.NextMonth[i].Match == match {
			if r.NextMonth[i].FromDay <= day {
				return false
			}
			r.NextMonth[i].FromDay = day
			return true
		}
	}
	r.NextMonth = append(r.NextMonth, Shift{Match: match, FromDay: day})
	return true
}

// DropNextMonth forgets every rule that moves payee's rows to the next month
// and reports whether there was one.
func (r *Rules) DropNextMonth(payee string) bool {
	payee = NormalizePayee(payee)
	n := len(r.NextMonth)
	r.NextMonth = slices.DeleteFunc(r.NextMonth, func(s Shift) bool {
		return s.Match != "" && strings.Contains(payee, s.Match)
	})
	if len(r.NextMonth) == 0 {
		r.NextMonth = nil
	}
	return len(r.NextMonth) != n
}
