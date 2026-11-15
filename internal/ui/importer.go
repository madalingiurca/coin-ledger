package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/importer"
	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// The import screen walks through two stages: a file browser to pick the bank
// file, then a preview of what it would add to which month. Nothing is written
// until the preview is confirmed.
type importStage int

const (
	stageBrowse importStage = iota
	stagePreview
)

// maxImportSize keeps a wrongly picked huge file from being read whole.
const maxImportSize = 20 << 20

type importState struct {
	stage importStage

	// browser
	dir     string
	entries []fileEntry
	cursor  int
	dirErr  error

	// preview
	file    string
	st      importer.Statement
	months  []importMonth
	pickErr error
}

type fileEntry struct {
	name  string
	dir   bool
	size  int64
	mtime time.Time
}

// importMonth is what a statement brings to one month.
type importMonth struct {
	key   model.Key
	month *model.Month
	// loadErr is set when the month's file is damaged: nothing goes into it.
	loadErr error
	add     []importRow
	dupes   int
}

type importRow struct {
	txn      importer.Txn
	category string
}

func (im importMonth) count(pred func(importRow) bool) int {
	n := 0
	for _, r := range im.add {
		if pred(r) {
			n++
		}
	}
	return n
}

func (im importMonth) total(kind model.Kind) money.Amount {
	var sum money.Amount
	for _, r := range im.add {
		if r.txn.Kind == kind && r.category != model.Transfers {
			sum += r.txn.Amount
		}
	}
	return sum
}

// openImport starts the import flow in the folder used last time, or in
// Downloads, where bank exports usually land.
func (m *Model) openImport() {
	dir := m.store.LoadState().ImportDir
	if !isDir(dir) {
		dir = ""
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
			if d := filepath.Join(home, "Downloads"); isDir(d) {
				dir = d
			}
		}
	}
	m.imp = &importState{}
	m.imp.browse(dir)
	m.screen = screenImport
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// browse lists a folder: sub-folders first, then CSV files, newest first,
// since the export just downloaded is the one wanted. Hidden entries are left
// out.
func (s *importState) browse(dir string) {
	s.stage = stageBrowse
	s.dir, s.cursor, s.dirErr, s.entries = dir, 0, nil, nil
	list, err := os.ReadDir(dir)
	if err != nil {
		s.dirErr = err
		return
	}
	var dirs, files []fileEntry
	for _, e := range list {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.IsDir() || (info.Mode()&os.ModeSymlink != 0 && isDir(filepath.Join(dir, name))) {
			dirs = append(dirs, fileEntry{name: name, dir: true})
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".csv") {
			files = append(files, fileEntry{name: name, size: info.Size(), mtime: info.ModTime()})
		}
	}
	slices.SortFunc(dirs, func(a, b fileEntry) int { return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })
	slices.SortFunc(files, func(a, b fileEntry) int { return b.mtime.Compare(a.mtime) })
	s.entries = append(dirs, files...)
}

func (s *importState) up() {
	parent := filepath.Dir(s.dir)
	if parent == s.dir {
		return
	}
	from := filepath.Base(s.dir)
	s.browse(parent)
	for i, e := range s.entries {
		if e.dir && e.name == from {
			s.cursor = i
		}
	}
}

func (m Model) importKey(key string) (tea.Model, tea.Cmd) {
	s := m.imp
	if s.stage == stagePreview {
		switch key {
		case "esc", "backspace", "left":
			s.stage, s.pickErr = stageBrowse, nil
		case "enter", "y":
			m.commitImport()
		}
		return m, nil
	}

	s.pickErr = nil
	switch key {
	case "esc":
		m.imp, m.screen = nil, screenMonth
	case "up", "k":
		s.cursor = max(0, s.cursor-1)
	case "down", "j":
		s.cursor = max(0, min(len(s.entries)-1, s.cursor+1))
	case "g", "home":
		s.cursor = 0
	case "G", "end":
		s.cursor = max(0, len(s.entries)-1)
	case "backspace", "left", "h":
		s.up()
	case "~":
		if home, err := os.UserHomeDir(); err == nil {
			s.browse(home)
		}
	case "enter", "right", "l":
		if s.cursor >= len(s.entries) {
			return m, nil
		}
		e := s.entries[s.cursor]
		if e.dir {
			s.browse(filepath.Join(s.dir, e.name))
			return m, nil
		}
		m.previewImport(filepath.Join(s.dir, e.name))
	}
	return m, nil
}

// previewImport reads and parses the file and works out, month by month, what
// importing it would change. It writes nothing.
func (m *Model) previewImport(path string) {
	s := m.imp
	info, err := os.Stat(path)
	if err == nil && info.Size() > maxImportSize {
		err = fmt.Errorf("%s is too large to be a bank statement", filepath.Base(path))
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(path)
	}
	var st importer.Statement
	if err == nil {
		st, err = importer.Parse(data)
	}
	if err != nil {
		s.pickErr = err
		return
	}

	byKey := map[model.Key]*importMonth{}
	var keys []model.Key
	for _, txn := range st.Txns {
		t, err := time.Parse("2006-01-02", txn.Date)
		if err != nil {
			s.pickErr = fmt.Errorf("%s: date %q is not a date", filepath.Base(path), txn.Date)
			return
		}
		k := model.KeyOf(t)
		im, ok := byKey[k]
		if !ok {
			im = &importMonth{key: k}
			if res, err := m.store.Get(k); err != nil {
				im.loadErr = err
			} else {
				im.month = res.Month
			}
			byKey[k] = im
			keys = append(keys, k)
		}
		if im.month != nil && im.month.HasKey(txn.Key) {
			im.dupes++
			continue
		}
		row := importRow{txn: txn}
		if txn.Kind == model.Expense {
			row.category, _ = m.rules.CategoryFor(txn.Payee)
		}
		im.add = append(im.add, row)
	}
	slices.SortFunc(keys, func(a, b model.Key) int { return a.Compare(b) })

	s.file, s.st, s.months = path, st, nil
	for _, k := range keys {
		s.months = append(s.months, *byKey[k])
	}
	s.stage = stagePreview
}

// commitImport writes every month the statement adds to. A month is written
// whole or not at all; a failure stops the import and says how far it got,
// and importing the file again finishes the job without doubling anything.
func (m *Model) commitImport() {
	s := m.imp
	added, months := 0, 0
	var last model.Key
	for _, im := range s.months {
		if im.loadErr != nil || len(im.add) == 0 {
			continue
		}
		for _, r := range im.add {
			imp := r.txn.Imported(s.st.Source)
			imp.Category = r.category
			im.month.AddImported(r.txn.Kind, imp)
		}
		if err := m.store.Save(im.month); err != nil {
			m.setError(fmt.Errorf("%w (months before %s were imported; import the file again to finish)", err, im.key.Label()))
			m.imp, m.screen = nil, screenMonth
			m.openMonth(m.dash.key)
			return
		}
		added += len(im.add)
		months++
		last = im.key
	}

	st := m.store.LoadState()
	st.ImportDir = filepath.Dir(s.file)
	_ = m.store.SaveState(st) // only a convenience

	m.imp, m.screen = nil, screenMonth
	if added == 0 {
		m.openMonth(m.dash.key)
		m.setStatus("Nothing new in %s", filepath.Base(s.file))
		return
	}
	m.openMonth(last)
	m.setStatus("Imported %s into %s", plural(added, "entry", "entries"), plural(months, "month", "months"))
}

func (m Model) importHints() []hint {
	if m.imp.stage == stagePreview {
		if m.imp.newCount() == 0 {
			return []hint{keyHint("esc", "back"), keyHint("enter", "close")}
		}
		return []hint{keyHint("enter", "import"), keyHint("esc", "back"), keyHint("q", "quit")}
	}
	return []hint{
		keyHint("enter", "open"),
		keyHint("←", "up a folder"),
		keyHint("~", "home"),
		keyHint("esc", "cancel"),
	}
}

func (s *importState) newCount() int {
	n := 0
	for _, im := range s.months {
		if im.loadErr == nil {
			n += len(im.add)
		}
	}
	return n
}

func (m Model) importView(cw, maxH int) string {
	if m.imp.stage == stagePreview {
		return m.previewView(cw)
	}
	t := m.theme
	s := m.imp
	name, right := spread("import", "pick a bank CSV", cw)
	lines := []string{t.Title.Render(name) + t.Muted.Render(right)}
	lines = append(lines, t.Muted.Render(truncate(tildePath(s.dir), cw)), "")

	if s.dirErr != nil {
		lines = append(lines, wrap(t.Danger, cw, s.dirErr.Error()))
		return strings.Join(lines, "\n")
	}

	var body []string
	if len(s.entries) == 0 {
		body = append(body, t.Muted.Render("  no folders or CSV files here"))
	}
	for i, e := range s.entries {
		text := e.name
		right := ""
		if e.dir {
			text += "/"
		} else {
			right = e.mtime.Format("2 Jan 2006")
		}
		rw := lipgloss.Width(right)
		text = padRight(truncate(text, max(4, cw-2-rw-1)), max(0, cw-2-rw))
		switch {
		case i == s.cursor:
			body = append(body, t.Selected.Bold(true).Render(padRight("› "+text+right, cw)))
		case e.dir:
			body = append(body, t.Marker.Render("  "+text)+t.Muted.Render(right))
		default:
			body = append(body, t.Label.Render("  "+text)+t.Muted.Render(right))
		}
	}

	var tail []string
	if s.pickErr != nil {
		tail = append(tail, "", wrap(t.Danger, cw, s.pickErr.Error()))
	}
	if maxH > 0 {
		fixed := lipgloss.Height(strings.Join(lines, "\n")) + lipgloss.Height(strings.Join(tail, "\n"))
		body = scrollWindow(t, body, s.cursor, maxH-fixed, cw)
	}
	lines = append(lines, body...)
	lines = append(lines, tail...)
	return strings.Join(lines, "\n")
}

func (m Model) previewView(cw int) string {
	t := m.theme
	s := m.imp
	name, right := spread("import "+s.st.Bank, plural(len(s.st.Txns), "transaction", "transactions"), cw)
	lines := []string{t.Title.Render(name) + t.Month.Render(right)}
	lines = append(lines, t.Muted.Render(truncate(filepath.Base(s.file), cw)), "")

	switch c := s.st.Check; {
	case !c.Done:
		lines = append(lines, wrap(t.Warning, cw, "The file has no balances to check the transactions against."))
	case c.OK:
		lines = append(lines, wrap(t.Positive, cw, "✓ Matches the bank's balance: "+c.Note))
	default:
		lines = append(lines, wrap(t.Danger, cw, "✗ Does not match the bank's balance: "+c.Note+". Some rows may be missing; check the file before importing."))
	}
	for _, note := range s.st.Skipped {
		lines = append(lines, wrap(t.Muted, cw, "Left out: "+note))
	}

	for _, im := range s.months {
		lines = append(lines, "", t.Section.Render(strings.ToUpper(im.key.Label())))
		if im.loadErr != nil {
			lines = append(lines, wrap(t.Danger, cw, "  skipped, its file cannot be read: "+im.loadErr.Error()))
			continue
		}
		if len(im.add) == 0 {
			lines = append(lines, t.Muted.Render("  nothing new · "+plural(im.dupes, "row", "rows")+" already imported"))
			continue
		}
		line := func(label string, amount money.Amount, style lipgloss.Style) string {
			a := amount.Lei()
			return style.Render(padRight("  "+label, cw-lipgloss.Width(a))) + t.Amount.Render(a)
		}
		in := im.count(func(r importRow) bool { return r.txn.Kind == model.Income })
		out := len(im.add) - in
		if in > 0 {
			lines = append(lines, line("income · "+itoa(in), im.total(model.Income), t.Label))
		}
		if out > 0 {
			lines = append(lines, line("expenses · "+itoa(out), im.total(model.Expense), t.Label))
		}

		var parts []string
		if n := im.count(func(r importRow) bool { return r.category != "" && r.category != model.Transfers }); n > 0 {
			parts = append(parts, itoa(n)+" categorised")
		}
		if n := im.count(func(r importRow) bool { return r.txn.Kind == model.Expense && r.category == "" }); n > 0 {
			parts = append(parts, itoa(n)+" uncategorised")
		}
		if n := im.count(func(r importRow) bool { return r.category == model.Transfers }); n > 0 {
			parts = append(parts, plural(n, "transfer", "transfers")+", not counted")
		}
		if im.dupes > 0 {
			parts = append(parts, itoa(im.dupes)+" already imported")
		}
		if len(parts) > 0 {
			lines = append(lines, wrap(t.Muted, cw, "  "+strings.Join(parts, " · ")))
		}
	}

	lines = append(lines, "")
	if s.newCount() == 0 {
		lines = append(lines, wrap(t.Muted, cw, "Everything in this file is already in your budget."))
	} else {
		lines = append(lines, wrap(t.Muted, cw, "Nothing is saved until you press enter. The bank file itself is not copied."))
	}
	return strings.Join(lines, "\n")
}

// tildePath shortens the home folder to ~ for display.
func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
			return "~" + string(filepath.Separator) + rest
		}
	}
	return p
}
