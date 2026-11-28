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

// The import screen walks through up to three stages: a file browser to pick
// the bank file, a preview of what it would add to which month, and, when bank
// rows look like forecast entries already in those months, a list of those
// pairs to replace or keep. Nothing is written until the last stage is
// confirmed.
type importStage int

const (
	stageBrowse importStage = iota
	stagePreview
	stageMatches
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
	months  []*importMonth
	pickErr error
	// seen holds every month the preview looked at, listed or not, so a
	// month is loaded once however many rows touch it.
	seen map[model.Key]*importMonth

	// matches stage
	matchCursor int
}

// pairRef addresses one bank row that matched a forecast entry.
type pairRef struct{ month, row int }

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
	// deleted are rows booked in this month that were imported once and
	// then deleted. They are left out unless the user brings them back.
	deleted []importer.Txn
}

type importRow struct {
	txn      importer.Txn
	category string
	// from is the month the bank booked the row in. A row in the month after
	// it counts for that month instead: a salary paid early because the 1st
	// fell on a weekend. ruled says a remembered payee rule moved it there.
	from  model.Key
	ruled bool
	// restored says the row was deleted after an earlier import and the user
	// asked for it back.
	restored bool
	// match is a forecast entry this row looks like the actual for; choice
	// is what the user wants done about it.
	match  *model.Entry
	reason model.MatchReason
	choice pairChoice
}

// pairChoice is what becomes of a bank row that looks like a forecast entry.
type pairChoice int

const (
	// pairReplace: the row takes the forecast entry's place.
	pairReplace pairChoice = iota
	// pairKeep: both stay, the row filed in the plan's category so that the
	// category counts the larger of the two rather than both.
	pairKeep
	// pairApart: the row has nothing to do with the plan.
	pairApart
)

func (c pairChoice) String() string {
	switch c {
	case pairKeep:
		return "keep both"
	case pairApart:
		return "unrelated"
	}
	return "replace"
}

// moved reports whether the row counts for another month than its date's.
func (r importRow) moved(in model.Key) bool { return r.from != in }

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
	switch s.stage {
	case stagePreview:
		switch key {
		case "esc", "backspace", "left":
			s.stage, s.pickErr = stageBrowse, nil
		case "r":
			m.restoreDeleted()
		case "enter", "y":
			if len(s.pairs()) > 0 {
				s.stage, s.matchCursor = stageMatches, 0
				return m, nil
			}
			m.commitImport()
		}
		return m, nil
	case stageMatches:
		pairs := s.pairs()
		if len(pairs) == 0 && key != "enter" && key != "y" && key != "esc" {
			return m, nil
		}
		switch key {
		case "esc", "backspace", "left":
			s.stage = stagePreview
		case "up", "k":
			s.matchCursor = max(0, s.matchCursor-1)
		case "down", "j":
			s.matchCursor = min(len(pairs)-1, s.matchCursor+1)
		case "space", " ", "r":
			p := pairs[s.matchCursor]
			if row := &s.months[p.month].add[p.row]; row.match != nil {
				row.choice = (row.choice + 1) % 3
			}
		case "n", ">":
			m.toggleNextMonth(pairs[s.matchCursor])
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

	s.seen = map[model.Key]*importMonth{}
	var keys []model.Key
	use := func(k model.Key) *importMonth {
		im := m.importMonth(k)
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
		return im
	}
	for _, txn := range st.Txns {
		t, err := time.Parse("2006-01-02", txn.Date)
		if err != nil {
			s.pickErr = fmt.Errorf("%s: date %q is not a date", filepath.Base(path), txn.Date)
			return
		}
		k := model.KeyOf(t)
		home := use(k)
		switch m.alreadyImported(txn.Key, k) {
		case rowPresent:
			home.dupes++
			continue
		case rowDeleted:
			home.deleted = append(home.deleted, txn)
			continue
		}
		row, target := m.placeRow(txn, k)
		use(target.key)
		target.add = append(target.add, row)
	}
	slices.SortFunc(keys, func(a, b model.Key) int { return a.Compare(b) })

	s.file, s.st, s.months = path, st, nil
	for _, k := range keys {
		im := s.seen[k]
		m.matchForecasts(im, st.Source)
		s.months = append(s.months, im)
	}
	s.stage = stagePreview
}

// importMonth loads a month for the import once and keeps it.
func (m *Model) importMonth(k model.Key) *importMonth {
	s := m.imp
	if im, ok := s.seen[k]; ok {
		return im
	}
	im := &importMonth{key: k}
	if res, err := m.store.Get(k); err != nil {
		im.loadErr = err
	} else {
		im.month = res.Month
	}
	s.seen[k] = im
	return im
}

// placeRow works out a new bank row's category and the month it counts for:
// the month it was booked in, k, or the one after when a payee rule says so.
func (m *Model) placeRow(txn importer.Txn, k model.Key) (importRow, *importMonth) {
	row := importRow{txn: txn, from: k}
	if c, ok := m.rules.CategoryFor(txn.Payee); ok && (txn.Kind == model.Expense || c == model.Transfers) {
		row.category = c
	}
	target := m.importMonth(k)
	if row.category != model.Transfers && m.rules.CountsForNextMonth(txn.Payee, day(txn.Date)) {
		if next := m.importMonth(k.Next()); next.loadErr == nil {
			target, row.ruled = next, true
		}
	}
	return row, target
}

// rowState is what the budget already knows of a bank row.
type rowState int

const (
	rowNew rowState = iota
	// rowPresent: the row is an entry already.
	rowPresent
	// rowDeleted: the row was imported and then deleted.
	rowDeleted
)

// alreadyImported reports whether a bank row booked in month k is in the
// budget, in k or moved to the month before or after it, or was deleted from
// one of those months after an earlier import.
func (m *Model) alreadyImported(key string, k model.Key) rowState {
	state := rowNew
	for _, near := range []model.Key{k, k.Prev(), k.Next()} {
		im := m.importMonth(near)
		switch {
		case im.month == nil:
		case im.month.Contains(key):
			return rowPresent
		case im.month.Deleted(key):
			state = rowDeleted
		}
	}
	return state
}

// deletedCount is how many rows in the file were deleted after an earlier
// import and are left out.
func (s *importState) deletedCount() int {
	n := 0
	for _, im := range s.months {
		if im.loadErr == nil {
			n += len(im.deleted)
		}
	}
	return n
}

// restoreDeleted brings back the rows that were deleted after an earlier
// import: they are placed and matched against the plan like new rows.
func (m *Model) restoreDeleted() {
	s := m.imp
	for _, im := range slices.Clone(s.months) {
		if im.loadErr != nil {
			continue
		}
		for _, txn := range im.deleted {
			row, target := m.placeRow(txn, im.key)
			row.restored = true
			target.add = append(target.add, row)
			if !slices.Contains(s.months, target) {
				s.months = append(s.months, target)
			}
		}
		im.deleted = nil
	}
	slices.SortFunc(s.months, func(a, b *importMonth) int { return a.key.Compare(b.key) })
	for _, im := range s.months {
		m.matchForecasts(im, s.st.Source)
	}
}

// toggleNextMonth moves a row in the review to the month after its date, or
// back to its date's month, and looks for a forecast it fulfils there.
func (m *Model) toggleNextMonth(p pairRef) {
	s := m.imp
	src := s.months[p.month]
	row := src.add[p.row]
	if row.category == model.Transfers {
		return
	}
	to := row.from
	if !row.moved(src.key) {
		to = row.from.Next()
	}
	dst := m.importMonth(to)
	if dst.loadErr != nil {
		m.setStatus("%s cannot be read, so the row stays in %s", to.Label(), src.key.Label())
		return
	}
	if !slices.Contains(s.months, dst) {
		s.months = append(s.months, dst)
		slices.SortFunc(s.months, func(a, b *importMonth) int { return a.key.Compare(b.key) })
	}
	src.add = slices.Delete(src.add, p.row, p.row+1)
	row.match, row.reason, row.choice = nil, 0, pairReplace
	dst.add = append(dst.add, row)
	m.matchForecasts(dst, s.st.Source)

	// Keep the cursor on the row it moved, or near where it was when the
	// row left the list.
	s.matchCursor = max(0, min(s.matchCursor, len(s.pairs())-1))
	for i, q := range s.pairs() {
		if s.months[q.month] == dst && q.row == len(dst.add)-1 {
			s.matchCursor = i
		}
	}
}

// matchForecasts pairs bank rows with the forecast entries they look like:
// first rows whose payee has replaced a forecast before, then rows for exactly
// a forecast's amount. Each forecast entry is offered to one row at most.
// A pair starts out as "replace", or "keep both" for a near amount; the user
// decides.
func (m *Model) matchForecasts(im *importMonth, source string) {
	if im.month == nil {
		return
	}
	claimed := map[string]bool{}
	for _, r := range im.add {
		if r.match != nil {
			claimed[r.match.ID] = true
		}
	}
	for _, want := range []model.MatchReason{model.MatchLinked, model.MatchAmount, model.MatchClose} {
		for i := range im.add {
			r := &im.add[i]
			if r.match != nil || r.category == model.Transfers {
				continue
			}
			for _, c := range im.month.ForecastMatches(r.txn.Kind, r.txn.Imported(source), m.rules) {
				if c.Reason != want || claimed[c.Entry.ID] {
					continue
				}
				e := c.Entry
				// A near amount is only a guess: the plan is kept beside it.
				r.match, r.reason, r.choice = &e, c.Reason, pairReplace
				// Income has no category to tie a kept plan to, so keeping both
				// would count it twice: only expenses default to keep both.
				if c.Reason == model.MatchClose && r.txn.Kind == model.Expense {
					r.choice = pairKeep
				}
				claimed[e.ID] = true
				break
			}
		}
	}
}

// pairs lists the bank rows to look at before importing: those that matched a
// forecast entry and those that count for another month than their date's,
// in preview order.
func (s *importState) pairs() []pairRef {
	var out []pairRef
	for mi, im := range s.months {
		if im.loadErr != nil {
			continue
		}
		for ri, r := range im.add {
			if r.match != nil || r.moved(im.key) {
				out = append(out, pairRef{mi, ri})
			}
		}
	}
	return out
}

// commitImport writes every month the statement adds to. A month is written
// whole or not at all; a failure stops the import and says how far it got,
// and importing the file again finishes the job without doubling anything.
func (m *Model) commitImport() {
	s := m.imp
	added, months, replaced, taught := 0, 0, 0, false
	var last model.Key
	for _, im := range s.months {
		if im.loadErr != nil || len(im.add) == 0 {
			continue
		}
		for _, r := range im.add {
			// Moving a row teaches the payee; moving back what a rule moved
			// forgets the rule.
			switch moved := r.moved(im.key); {
			case moved && !r.ruled:
				taught = m.rules.SetNextMonth(r.txn.Payee, day(r.txn.Date)) || taught
			case !moved && r.ruled:
				taught = m.rules.DropNextMonth(r.txn.Payee) || taught
			}
			imp := r.txn.Imported(s.st.Source)
			imp.Category, imp.Auto = r.category, r.category != ""
			if r.restored {
				im.month.Undelete(imp.Key)
			}
			if r.match != nil && r.choice == pairKeep && imp.Category == "" && r.txn.Kind == model.Expense {
				imp.Category, imp.Auto = r.match.Category, false
			}
			if r.match != nil && r.choice == pairReplace {
				if err := im.month.ReplaceForecast(r.txn.Kind, r.match.ID, imp); err == nil {
					replaced++
					if m.rules.SetLinkIfNew(imp.Payee, r.match.Label) {
						taught = true
					}
					continue
				}
			}
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

	if taught && m.rulesErr == nil {
		if err := m.store.SaveRules(m.rules); err != nil {
			m.setError(err)
		}
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
	if replaced > 0 {
		m.setStatus("Imported %s into %s · %s replaced",
			plural(added, "entry", "entries"), plural(months, "month", "months"), plural(replaced, "forecast", "forecasts"))
		return
	}
	m.setStatus("Imported %s into %s", plural(added, "entry", "entries"), plural(months, "month", "months"))
}

// day is the day of the month of a "2026-11-27" date.
func day(date string) int {
	if t, err := time.Parse("2006-01-02", date); err == nil {
		return t.Day()
	}
	return 0
}

// allMatched reports whether every row in the review matched a forecast.
func (s *importState) allMatched() bool {
	for _, p := range s.pairs() {
		if s.months[p.month].add[p.row].match == nil {
			return false
		}
	}
	return true
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

// importTitle names the import pane after the stage it is at.
func (m Model) importTitle() string {
	s := m.imp
	switch s.stage {
	case stagePreview:
		return "[4] Import " + s.st.Bank + " · " + plural(len(s.st.Txns), "transaction", "transactions")
	case stageMatches:
		if n := len(s.pairs()); n == 0 || !s.allMatched() {
			return "[4] Check before importing · " + plural(n, "row", "rows")
		}
		return "[4] Already planned · " + plural(len(s.pairs()), "match", "matches")
	}
	return "[4] Import · pick a bank CSV"
}

func (m Model) importCounter() string {
	s := m.imp
	switch s.stage {
	case stagePreview:
		return ""
	case stageMatches:
		return counter(s.matchCursor, len(s.pairs()))
	}
	return counter(s.cursor, len(s.entries))
}

func (m Model) importView(cw, maxH int) string {
	switch m.imp.stage {
	case stagePreview:
		return m.previewView(cw)
	case stageMatches:
		return m.matchesView(cw, maxH)
	}
	t := m.theme
	s := m.imp
	lines := []string{t.Muted.Render(truncate(tildePath(s.dir), cw)), ""}

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
			body = append(body, t.Selected.Bold(true).Render(padRight("  "+text+right, cw)))
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
		body = scrollWindow(body, s.cursor, maxH-fixed)
	}
	lines = append(lines, body...)
	lines = append(lines, tail...)
	return strings.Join(lines, "\n")
}

func (m Model) previewView(cw int) string {
	t := m.theme
	s := m.imp
	lines := []string{t.Muted.Render(truncate(filepath.Base(s.file), cw)), ""}

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
			var parts []string
			if im.dupes > 0 || len(im.deleted) == 0 {
				parts = append(parts, plural(im.dupes, "row", "rows")+" already imported")
			}
			if n := len(im.deleted); n > 0 {
				parts = append(parts, plural(n, "row", "rows")+" you deleted")
			}
			lines = append(lines, wrap(t.Muted.PaddingLeft(2), cw, "nothing new · "+strings.Join(parts, " · ")))
			continue
		}
		line := func(label string, amount money.Amount, style lipgloss.Style) string {
			a := amount.Lei()
			return style.Render("  ") + leaderRow(t, label, style, a, t.Amount, max(1, cw-2))
		}
		counted := func(kind model.Kind) func(importRow) bool {
			return func(r importRow) bool { return r.txn.Kind == kind && r.category != model.Transfers }
		}
		in, out := im.count(counted(model.Income)), im.count(counted(model.Expense))
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
		if n := im.count(func(r importRow) bool { return r.moved(im.key) }); n > 0 {
			parts = append(parts, itoa(n)+" booked in "+im.key.Prev().Month.String()+" "+
				map[bool]string{true: "counts", false: "count"}[n == 1]+" here")
		}
		if n := im.count(func(r importRow) bool { return r.match != nil }); n > 0 {
			parts = append(parts, plural(n, "looks", "look")+" like a forecast entry")
		}
		if n := im.count(func(r importRow) bool { return r.restored }); n > 0 {
			parts = append(parts, itoa(n)+" deleted before, brought back")
		}
		if im.dupes > 0 {
			parts = append(parts, itoa(im.dupes)+" already imported")
		}
		if n := len(im.deleted); n > 0 {
			parts = append(parts, itoa(n)+" you deleted")
		}
		if len(parts) > 0 {
			lines = append(lines, wrap(t.Muted.PaddingLeft(2), cw, strings.Join(parts, " · ")))
		}
	}

	lines = append(lines, "")
	if n := s.deletedCount(); n > 0 {
		lines = append(lines, wrap(t.Warning, cw, map[bool]string{
			true:  "1 row in this file was deleted after an earlier import, so it is left out. Press r to import it again.",
			false: itoa(n) + " rows in this file were deleted after an earlier import, so they are left out. Press r to import them again.",
		}[n == 1]))
		if s.newCount() == 0 {
			return strings.Join(lines, "\n")
		}
	}
	if s.newCount() == 0 {
		lines = append(lines, wrap(t.Muted, cw, "Everything in this file is already in your budget."))
	} else if n := len(s.pairs()); n > 0 && s.allMatched() {
		lines = append(lines, wrap(t.Muted, cw, "Press enter to choose whether "+
			map[bool]string{true: "the row that looks", false: "the " + itoa(n) + " rows that look"}[n == 1]+
			" like a forecast entry should replace it. Nothing is saved before that."))
	} else if n > 0 {
		lines = append(lines, wrap(t.Muted, cw, "Press enter to check the rows that look like a forecast entry or count for another month. Nothing is saved before that."))
	} else {
		lines = append(lines, wrap(t.Muted, cw, "Nothing is saved until you press enter. The bank file itself is not copied."))
	}
	return strings.Join(lines, "\n")
}

// matchesView lists the forecast entries that bank rows look like, each to be
// replaced by its row or kept beside it.
func (m Model) matchesView(cw, maxH int) string {
	t := m.theme
	s := m.imp
	pairs := s.pairs()
	lines := []string{wrap(t.Muted, cw,
		"These bank rows look like entries you planned. Replace takes the bank's amount and date and keeps your label, category and recurring flag. Keep both adds the row next to the plan, in its category, so the larger of the two counts. Unrelated adds the row on its own."),
		wrap(t.Muted, cw, "n counts a row for the month after its date, as a salary paid early, and does so for that payee from then on; n again moves it back."), ""}

	amtW := min(amountCol, max(6, cw-4-6))
	choiceW := len("keep both") + 2
	labelW := max(4, cw-2-amtW-choiceW)
	var body []string
	cursorLine := 0
	lastMonth := -1
	for i, p := range pairs {
		im := s.months[p.month]
		r := im.add[p.row]
		if p.month != lastMonth && len(s.months) > 1 {
			body = append(body, t.Section.Render(strings.ToUpper(im.key.Label())))
			lastMonth = p.month
		}
		when := r.txn.Date[8:]
		if r.moved(im.key) {
			when += " " + r.from.Month.String()[:3]
		}
		var plan, choice, why string
		cstyle := t.Positive
		if r.match != nil {
			choice = r.choice.String()
			switch r.choice {
			case pairKeep:
				cstyle = t.Warning
			case pairApart:
				cstyle = t.Muted
			}
			plan = padRight(truncate(r.match.Label, labelW-1), labelW) + padLeft(r.match.Amount.String(), amtW)
			why = "same amount"
			switch r.reason {
			case model.MatchLinked:
				why = "replaced it before"
			case model.MatchClose:
				why = "close amount"
			}
		} else {
			// Nothing planned: the row is here because it counts for this
			// month although the bank booked it in the one before.
			choice, cstyle = "moved", t.Warning
			plan = padRight(truncate(r.txn.Label, labelW-1), labelW) + padLeft("", amtW)
			why = "counts for " + im.key.Month.String()
		}
		bankLine := "  ↳ " + when + " " + r.txn.Label + " · " + why
		if r.match == nil {
			bankLine = "  ↳ " + when + " · " + why
		}
		bank := padRight(truncate(bankLine, labelW-1), labelW) + padLeft(r.txn.Amount.String(), amtW)
		if i == s.matchCursor {
			cursorLine = len(body)
			body = append(body,
				t.Selected.Bold(true).Render(padRight("  "+plan+padLeft(choice, choiceW), cw)),
				t.Selected.Render(padRight("  "+bank, cw)))
			continue
		}
		body = append(body,
			t.Label.Render("  "+plan)+cstyle.Render(padLeft(choice, choiceW)),
			t.Muted.Render(padRight("  "+bank, cw)))
	}
	if len(pairs) == 0 {
		body = append(body, t.Muted.Render("  Nothing left to check. Press enter to import."))
	}
	if maxH > 0 {
		fixed := lipgloss.Height(strings.Join(lines, "\n"))
		body = scrollWindow(body, cursorLine, maxH-fixed)
	}
	lines = append(lines, body...)
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
