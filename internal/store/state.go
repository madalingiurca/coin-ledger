package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// stateFile remembers small conveniences between runs. Losing it costs
// nothing, so it is read leniently and never reported as damaged.
const stateFile = "state.json"

// State is what the app remembers about how it was last used.
type State struct {
	// ImportDir is the folder the last bank file was imported from.
	ImportDir string `json:"import_dir,omitempty"`
}

// LoadState reads the remembered state; a missing or unreadable file gives
// the zero State.
func (s *Store) LoadState() State {
	var st State
	if data, err := os.ReadFile(filepath.Join(s.dir, stateFile)); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

// SaveState writes the remembered state atomically.
func (s *Store) SaveState(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return s.writeAtomic("state", filepath.Join(s.dir, stateFile), append(data, '\n'))
}
