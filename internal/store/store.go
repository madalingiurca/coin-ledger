// Package store persists months as one JSON file each. The directory listing is
// the index: there is no database and no manifest to fall out of sync.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// ErrNoMonth reports that a month has no file yet.
var ErrNoMonth = errors.New("no file for this month")

// Store reads and writes month files in one directory.
type Store struct{ dir string }

// Home is where months live: $BUDGET_HOME when set, otherwise ~/.budget.
func Home() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("BUDGET_HOME")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate your home directory: %w", err)
	}
	return filepath.Join(home, ".budget"), nil
}

// Open prepares dir for use, creating it if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string { return s.dir }

// Path is the file a month is stored in.
func (s *Store) Path(k model.Key) string { return filepath.Join(s.dir, k.String()+".json") }

// Exists reports whether a month has been written to disk.
func (s *Store) Exists(k model.Key) bool {
	_, err := os.Stat(s.Path(k))
	return err == nil
}

// Load reads one month.
func (s *Store) Load(k model.Key) (*model.Month, error) {
	path := s.Path(k)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", k, ErrNoMonth)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}

	var m model.Month
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", filepath.Base(path), err)
	}
	if m.Version > model.Version {
		return nil, fmt.Errorf("%s was written by a newer version of budget (format %d)",
			filepath.Base(path), m.Version)
	}
	// The file name is authoritative: it is what navigation and listing use.
	m.Key = k
	if m.Income == nil {
		m.Income = []model.Entry{}
	}
	if m.Expenses == nil {
		m.Expenses = []model.Entry{}
	}
	return &m, nil
}

// Save writes a month atomically: a temporary file in the same directory is
// renamed over the target, so an interrupted write cannot leave a half-written
// month behind.
func (s *Store) Save(m *model.Month) error {
	m.Version = model.Version
	m.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", m.Key, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(s.dir, "."+m.Key.String()+"-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write in %s: %w", s.dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", m.Key, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("flushing %s: %w", m.Key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", m.Key, err)
	}
	if err := os.Rename(tmpName, s.Path(m.Key)); err != nil {
		return fmt.Errorf("saving %s: %w", m.Key, err)
	}
	return nil
}

// Keys lists the months on disk, newest first.
func (s *Store) Keys() ([]model.Key, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.dir, err)
	}

	var keys []model.Key
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		k, err := model.ParseKey(name)
		if err != nil {
			continue // not a month file; leave it alone
		}
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b model.Key) int { return b.Compare(a) })
	return keys, nil
}

// Result is what opening a month produced.
type Result struct {
	Month *model.Month
	// OnDisk is false for a month that was seeded in memory and has not been
	// written yet, which is every month you merely navigate to.
	OnDisk bool
	// SeededFrom names the month the recurring entries came from, when any did.
	SeededFrom *model.Key
	// Warning is non-fatal: a month that could not be read while looking for
	// recurring entries to carry forward.
	Warning error
}

// Get opens a month, seeding it from the most recent readable earlier month
// when it does not exist yet. The seeded month is returned unwritten; it
// reaches disk on the first edit, so browsing ahead leaves no empty files.
func (s *Store) Get(k model.Key) (Result, error) {
	if s.Exists(k) {
		m, err := s.Load(k)
		if err != nil {
			return Result{}, err
		}
		return Result{Month: m, OnDisk: true}, nil
	}

	keys, err := s.Keys()
	if err != nil {
		return Result{}, err
	}

	var warning error
	for _, prevKey := range keys { // newest first
		if !prevKey.Before(k) {
			continue
		}
		prev, err := s.Load(prevKey)
		if err != nil {
			// Skip past a damaged month rather than refusing to open a new
			// one, but say so.
			if warning == nil {
				warning = fmt.Errorf("could not read %s to carry recurring entries: %w", prevKey, err)
			}
			continue
		}
		from := prevKey
		return Result{Month: model.SeedFrom(prev, k), SeededFrom: &from, Warning: warning}, nil
	}
	return Result{Month: model.New(k), Warning: warning}, nil
}

// Summary is one row of the history view.
type Summary struct {
	Key      model.Key
	Income   money.Amount
	Expenses money.Amount
	Left     money.Amount
	// Err is set when the month exists but could not be read.
	Err error
}

// Summaries totals every month on disk, newest first. At most limit months are
// returned; limit <= 0 means all of them.
func (s *Store) Summaries(limit int) ([]Summary, error) {
	keys, err := s.Keys()
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	out := make([]Summary, 0, len(keys))
	for _, k := range keys {
		m, err := s.Load(k)
		if err != nil {
			out = append(out, Summary{Key: k, Err: err})
			continue
		}
		out = append(out, Summary{
			Key:      k,
			Income:   m.TotalIncome(),
			Expenses: m.TotalExpenses(),
			Left:     m.Left(),
		})
	}
	return out, nil
}
