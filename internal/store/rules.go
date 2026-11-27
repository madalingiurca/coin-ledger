package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// rulesFile holds the categories, payee rules and forecast links. It is the
// one file that is not a month; Keys skips it because its name is not one.
const rulesFile = "categories.json"

// RulesPath is where the categories and payee rules are stored.
func (s *Store) RulesPath() string { return filepath.Join(s.dir, rulesFile) }

// LoadRules reads the categories file. A store that has none yet gets the
// defaults, unwritten. A file that cannot be read is an error rather than a
// reason to start over: the caller must not save over it.
func (s *Store) LoadRules() (*model.Rules, error) {
	data, err := os.ReadFile(s.RulesPath())
	if errors.Is(err, os.ErrNotExist) {
		return model.NewRules(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rulesFile, err)
	}

	var r model.Rules
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", rulesFile, err)
	}
	if r.Version > model.RulesVersion {
		return nil, fmt.Errorf("%s was written by a newer version of budget (format %d)",
			rulesFile, r.Version)
	}
	if r.Categories == nil {
		r.Categories = []string{}
	}
	if r.Rules == nil {
		r.Rules = []model.Rule{}
	}
	if r.Links == nil {
		r.Links = []model.Link{}
	}
	// Hand-edited patterns still have to match normalised payees.
	for i := range r.Rules {
		r.Rules[i].Match = model.NormalizePayee(r.Rules[i].Match)
	}
	for i := range r.Links {
		r.Links[i].Match = model.NormalizePayee(r.Links[i].Match)
	}
	r.AddCategory(model.Transfers)
	return &r, nil
}

// SaveRules writes the categories file atomically.
func (s *Store) SaveRules(r *model.Rules) error {
	r.Version = model.RulesVersion
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", rulesFile, err)
	}
	return s.writeAtomic("categories", s.RulesPath(), append(data, '\n'))
}

// Sweep is the outcome of changing entries across every month on disk.
type Sweep struct {
	// Entries is how many entries changed, Months how many files were rewritten.
	Entries int
	Months  int
	// Skipped are months that could not be read and were left untouched.
	Skipped []model.Key
}

// sweep runs change over every month on disk and saves the ones it altered.
func (s *Store) sweep(change func(*model.Month) int) (Sweep, error) {
	keys, err := s.Keys()
	if err != nil {
		return Sweep{}, err
	}
	var out Sweep
	for _, k := range keys {
		m, err := s.Load(k)
		if err != nil {
			out.Skipped = append(out.Skipped, k)
			continue
		}
		n := change(m)
		if n == 0 {
			continue
		}
		if err := s.Save(m); err != nil {
			return out, err
		}
		out.Entries += n
		out.Months++
	}
	return out, nil
}

// ApplyRule files the expenses rule matches in every month on disk, as
// Month.Apply does. all is the full rule set, so a more specific rule keeps
// its entries.
func (s *Store) ApplyRule(rule model.Rule, all *model.Rules) (Sweep, error) {
	return s.sweep(func(m *model.Month) int { return m.Apply(rule, all) })
}

// RenameCategory renames a category in every month on disk. The rules
// themselves are renamed with Rules.RenameCategory and saved by the caller.
func (s *Store) RenameCategory(from, to string) (Sweep, error) {
	return s.sweep(func(m *model.Month) int { return m.RenameCategory(from, to) })
}

// PayeeSummary is one payee whose expenses still have no category.
type PayeeSummary struct {
	// Payee is the normalised name rules match on; Label is how the bank
	// spelled it on the most recent entry.
	Payee string
	Label string
	Count int
	Total money.Amount
	// Months is how many different months the payee appears in.
	Months int
}

// UncategorisedPayees lists every payee with imported expenses that have no
// category yet, across all months on disk, largest total first. Months that
// cannot be read are skipped.
func (s *Store) UncategorisedPayees() ([]PayeeSummary, error) {
	keys, err := s.Keys() // newest first
	if err != nil {
		return nil, err
	}
	byPayee := map[string]*PayeeSummary{}
	var order []string
	for _, k := range keys {
		m, err := s.Load(k)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, e := range m.Expenses {
			if e.Category != "" || e.Payee == "" {
				continue
			}
			p, ok := byPayee[e.Payee]
			if !ok {
				p = &PayeeSummary{Payee: e.Payee, Label: e.Label}
				byPayee[e.Payee] = p
				order = append(order, e.Payee)
			}
			p.Count++
			p.Total += e.Amount
			if !seen[e.Payee] {
				seen[e.Payee] = true
				p.Months++
			}
		}
	}
	out := make([]PayeeSummary, 0, len(order))
	for _, name := range order {
		out = append(out, *byPayee[name])
	}
	slices.SortStableFunc(out, func(a, b PayeeSummary) int {
		if a.Total != b.Total {
			if a.Total > b.Total {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Payee, b.Payee)
	})
	return out, nil
}

// PayeeEntry is one uncategorised expense of a payee, with the month it is in.
type PayeeEntry struct {
	Key   model.Key
	Entry model.Entry
}

// PayeeEntries lists the uncategorised expenses of one payee across every
// month on disk, newest first, for filing one at a time.
func (s *Store) PayeeEntries(payee string) ([]PayeeEntry, error) {
	keys, err := s.Keys()
	if err != nil {
		return nil, err
	}
	var out []PayeeEntry
	for _, k := range keys {
		m, err := s.Load(k)
		if err != nil {
			continue
		}
		var month []PayeeEntry
		for _, e := range m.Expenses {
			if e.Category == "" && e.Payee == payee {
				month = append(month, PayeeEntry{Key: k, Entry: e})
			}
		}
		slices.SortStableFunc(month, func(a, b PayeeEntry) int { return strings.Compare(b.Entry.Date, a.Entry.Date) })
		out = append(out, month...)
	}
	return out, nil
}

// SetCategory files one expense of a month on disk.
func (s *Store) SetCategory(k model.Key, id, category string) error {
	m, err := s.Load(k)
	if err != nil {
		return err
	}
	if err := m.SetCategory(id, category); err != nil {
		return err
	}
	return s.Save(m)
}
