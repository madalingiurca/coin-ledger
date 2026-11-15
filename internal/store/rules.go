package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/madalingiurca/budget/internal/model"
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

// ApplyRule files the uncategorised expenses that rule matches, in every month
// on disk. Entries that already have a category are left alone.
func (s *Store) ApplyRule(rule model.Rule) (Sweep, error) {
	return s.sweep(func(m *model.Month) int { return m.Apply(rule) })
}

// RenameCategory renames a category in every month on disk. The rules
// themselves are renamed with Rules.RenameCategory and saved by the caller.
func (s *Store) RenameCategory(from, to string) (Sweep, error) {
	return s.sweep(func(m *model.Month) int { return m.RenameCategory(from, to) })
}
