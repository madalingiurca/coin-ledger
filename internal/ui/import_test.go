package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
	"github.com/madalingiurca/budget/internal/store"
)

// importHome makes a fake home folder with a Downloads folder holding the
// importer's invented ING and Revolut statements, and points HOME at it.
func importHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	downloads := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(filepath.Join(downloads, "old statements"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ing.csv", "revolut.csv"} {
		data, err := os.ReadFile(filepath.Join("..", "importer", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(downloads, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The Revolut file is the newest download, so it is listed first.
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(downloads, "ing.csv"), old, old)
	os.WriteFile(filepath.Join(downloads, "notes.txt"), []byte("hello"), 0o600)
	os.WriteFile(filepath.Join(downloads, ".hidden.csv"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(downloads, "photo-list.csv"), []byte("a,b\n1,2\n"), 0o600)
	os.Chtimes(filepath.Join(downloads, "photo-list.csv"), old.Add(-time.Hour), old.Add(-time.Hour))
	return home
}

// pick moves the browser's cursor onto the entry with the given name.
func pick(t *testing.T, m Model, name string) Model {
	t.Helper()
	for i, e := range m.imp.entries {
		if e.name == name {
			m.imp.cursor = i
			return m
		}
	}
	t.Fatalf("%q is not listed in %s", name, m.imp.dir)
	return m
}

func TestImportBrowserStartsInDownloads(t *testing.T) {
	home := importHome(t)
	m := press(t, app(t, testStore(t)), "I")
	if m.screen != screenImport || m.imp.dir != filepath.Join(home, "Downloads") {
		t.Fatalf("screen %v, dir %q", m.screen, m.imp.dir)
	}
	screen := plain(m)
	mustContain(t, screen, "[4] Import", "~/Downloads", "old statements/", "revolut.csv", "ing.csv")
	for _, unwanted := range []string{"notes.txt", ".hidden.csv"} {
		if strings.Contains(screen, unwanted) {
			t.Errorf("%q is listed:\n%s", unwanted, screen)
		}
	}
	// Folders first, then files newest first.
	var names []string
	for _, e := range m.imp.entries {
		names = append(names, e.name)
	}
	if got := strings.Join(names, "|"); got != "old statements|revolut.csv|ing.csv|photo-list.csv" {
		t.Errorf("order = %q", got)
	}
	checkWidth(t, m)
}

func TestImportBrowserNavigates(t *testing.T) {
	home := importHome(t)
	m := press(t, app(t, testStore(t)), "I")
	m = pick(t, m, "old statements")
	m = press(t, m, "enter")
	if m.imp.dir != filepath.Join(home, "Downloads", "old statements") {
		t.Fatalf("dir = %q", m.imp.dir)
	}
	mustContain(t, plain(m), "no folders or CSV files here")
	m = press(t, m, "left")
	if m.imp.dir != filepath.Join(home, "Downloads") || m.imp.entries[m.imp.cursor].name != "old statements" {
		t.Errorf("going up landed in %q on %d", m.imp.dir, m.imp.cursor)
	}
	m = press(t, m, "~")
	if m.imp.dir != home {
		t.Errorf("~ went to %q", m.imp.dir)
	}
	m = press(t, m, "esc")
	if m.screen != screenMonth || m.imp != nil {
		t.Error("esc did not leave the import")
	}
}

func TestImportRejectsAFileThatIsNotAStatement(t *testing.T) {
	importHome(t)
	m := press(t, app(t, testStore(t)), "I")
	m = pick(t, m, "photo-list.csv")
	m = press(t, m, "enter")
	if m.imp.stage != stageBrowse {
		t.Fatal("an unknown file reached the preview")
	}
	mustContain(t, plain(m), "not an ING or Revolut statement")
}

func TestImportPreviewAndCommit(t *testing.T) {
	importHome(t)
	s := testStore(t)

	// September's recurring forecast should be carried into October when
	// the import creates it.
	sept := model.New(october.Prev())
	sept.Add(model.Expense, "Rent", money.FromLei(2800), true)
	saveMonth(t, s, sept)

	rules, _ := s.LoadRules()
	rules.SetRule("Rata Credit", "Loan")
	s.SaveRules(rules)

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter")
	if m.imp.stage != stagePreview {
		t.Fatalf("no preview: %v", m.imp.pickErr)
	}
	screen := plain(m)
	mustContain(t, screen, "Import ING", "10 transactions", "Matches the bank's balance",
		"OCTOBER 2026", "income · 2", "12.725,00 lei", "expenses · 7",
		"1 categorised", "6 uncategorised", "1 transfer, not counted", "Nothing is saved")
	checkWidth(t, m)

	// Nothing written yet.
	if s.Exists(october) {
		t.Fatal("the preview wrote the month")
	}

	m = press(t, m, "enter")
	if m.screen != screenMonth || m.dash.key != october {
		t.Fatalf("after import: screen %v, month %v", m.screen, m.dash.key)
	}
	mustContain(t, plain(m), "Imported 10 entries into 1 month")

	got, err := s.Load(october)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Income) != 2 || len(got.Expenses) != 9 { // 8 imported + seeded rent
		t.Fatalf("income %d, expenses %d", len(got.Income), len(got.Expenses))
	}
	cats := map[string]string{}
	for _, e := range got.Expenses {
		cats[e.Label] = e.Category
		if !e.IsForecast() && (e.Source != model.SourceING || e.Key == "" || e.Date == "") {
			t.Errorf("imported entry lacks its bank data: %+v", e)
		}
	}
	if cats["Rata Credit"] != "Loan" || cats["Revolut**9999*"] != model.Transfers || cats["Round Up"] != "" || cats["Rent"] != "" {
		t.Errorf("categories = %v", cats)
	}

	// The folder is remembered.
	if dir := s.LoadState().ImportDir; !strings.HasSuffix(dir, "Downloads") {
		t.Errorf("import dir = %q", dir)
	}

	// Importing the same file again adds nothing.
	m = press(t, m, "I")
	if !strings.HasSuffix(m.imp.dir, "Downloads") {
		t.Errorf("second import opened in %q", m.imp.dir)
	}
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "nothing new", "10 rows already imported", "already in your budget")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "Nothing new in ing.csv")
	again, _ := s.Load(october)
	if len(again.Income)+len(again.Expenses) != 11 {
		t.Errorf("re-import changed the month: %d entries", len(again.Income)+len(again.Expenses))
	}
}

func TestImportPreviewBackGoesToTheBrowser(t *testing.T) {
	importHome(t)
	s := testStore(t)
	m := press(t, app(t, s), "I")
	m = pick(t, m, "revolut.csv")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "Import Revolut", "Left out: 1 row is pending", "1 row is in EUR")
	m = press(t, m, "esc")
	if m.imp == nil || m.imp.stage != stageBrowse {
		t.Fatal("esc in the preview did not go back to the browser")
	}
	if s.Exists(october) {
		t.Error("backing out of the preview wrote the month")
	}
}

func TestImportLeavesADamagedMonthAlone(t *testing.T) {
	importHome(t)
	s := testStore(t)
	writeFile(s.Path(october), "{broken")

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "skipped, its file cannot be read")
	m = press(t, m, "enter")
	if got, _ := readFile(s.Path(october)); got != "{broken" {
		t.Errorf("the damaged month was overwritten: %q", got)
	}
}

func TestImportIsReachableFromADamagedMonth(t *testing.T) {
	importHome(t)
	s := testStore(t)
	writeFile(s.Path(model.Today()), "{broken")
	m := press(t, app(t, s), "I")
	if m.screen != screenImport {
		t.Error("I does nothing on a damaged month")
	}
}

func TestStateFileIsNotAMonth(t *testing.T) {
	s := testStore(t)
	if err := s.SaveState(store.State{ImportDir: "/x"}); err != nil {
		t.Fatal(err)
	}
	if keys, _ := s.Keys(); len(keys) != 0 {
		t.Errorf("keys = %v", keys)
	}
	writeFile(filepath.Join(s.Dir(), "state.json"), "{broken")
	if st := s.LoadState(); st.ImportDir != "" {
		t.Errorf("damaged state = %+v", st)
	}
}
