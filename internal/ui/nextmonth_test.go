package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

var november = october.Next()

// lateSalary puts a copy of the invented ING statement in Downloads with the
// salary booked on 30 October, as when the 1st of November is a Sunday.
func lateSalary(t *testing.T, home string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "importer", "testdata", "ing.csv"))
	if err != nil {
		t.Fatal(err)
	}
	late := strings.Replace(string(data), "02 octombrie 2026,,,Incasare", "30 octombrie 2026,,,Incasare", 1)
	if late == string(data) {
		t.Fatal("the salary row was not found in the test statement")
	}
	if err := os.WriteFile(filepath.Join(home, "Downloads", "ing-late.csv"), []byte(late), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestImportCountsAnEarlySalaryForNextMonth(t *testing.T) {
	home := importHome(t)
	lateSalary(t, home)
	s := testStore(t)
	oct := model.New(october)
	octSalary := oct.Add(model.Income, "Salary Oct", money.FromLei(12650), false)
	saveMonth(t, s, oct)
	nov := model.New(november)
	novSalary := nov.Add(model.Income, "Salary Nov", money.FromLei(12650), false)
	saveMonth(t, s, nov)

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing-late.csv")
	m = press(t, m, "enter", "enter")
	if m.imp.stage != stageMatches {
		t.Fatalf("stage = %v", m.imp.stage)
	}
	mustContain(t, plain(m), "Salary Oct", "30 EXEMPLU SRL", "n counts a row for the month after")

	// The salary row is the only pair: count it for November.
	m = press(t, m, "n")
	screen := plain(m)
	mustContain(t, screen, "NOVEMBER 2026", "Salary Nov", "30 Oct EXEMPLU SRL", "replace")
	if strings.Contains(screen, "Salary Oct") {
		t.Errorf("October's salary is still offered:\n%s", screen)
	}
	checkWidth(t, m)

	m = press(t, m, "enter")
	mustContain(t, plain(m), "into 2 months", "1 forecast replaced")

	gotOct, _ := s.Load(october)
	if e, _ := gotOct.Find(model.Income, octSalary); !e.IsForecast() {
		t.Errorf("October's salary was replaced: %+v", e)
	}
	if len(gotOct.Income) != 2 { // the forecast and the 75 lei from Andrei
		t.Errorf("October income = %+v", gotOct.Income)
	}
	gotNov, _ := s.Load(november)
	e, _ := gotNov.Find(model.Income, novSalary)
	if e.IsForecast() || e.Date != "2026-10-30" || e.Planned != money.FromLei(12650) {
		t.Errorf("November salary = %+v", e)
	}

	// The payee is remembered from that day on.
	rules, _ := s.LoadRules()
	if !rules.CountsForNextMonth("EXEMPLU SRL", 30) || rules.CountsForNextMonth("EXEMPLU SRL", 29) {
		t.Errorf("next-month rules = %+v", rules.NextMonth)
	}

	// Importing the file again finds the row in November and adds nothing.
	m = press(t, m, "I")
	m = pick(t, m, "ing-late.csv")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "already in your budget")
}

func TestARememberedPayeeMovesItsRowAndCanBeUndone(t *testing.T) {
	importHome(t)
	s := testStore(t)
	rules, _ := s.LoadRules()
	rules.SetNextMonth("EXEMPLU SRL", 2)
	s.SaveRules(rules)

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter")
	mustContain(t, plain(m), "NOVEMBER 2026", "income · 1", "12.650,00 lei", "1 booked in October counts here",
		"Press enter to check")

	m = press(t, m, "enter")
	screen := plain(m)
	mustContain(t, screen, "Check before importing", "1 row", "EXEMPLU SRL", "moved", "02 Oct · counts for November")
	checkWidth(t, m)

	// Back to October: the review is empty and the rule is forgotten.
	m = press(t, m, "n")
	mustContain(t, plain(m), "0 rows", "Nothing left to check")
	m = press(t, m, "space", "n", "down") // nothing to act on
	m = press(t, m, "enter")
	mustContain(t, plain(m), "Imported 10 entries into 1 month")
	if s.Exists(november) {
		t.Error("November was written although nothing went there")
	}
	got, _ := s.Load(october)
	if len(got.Income) != 2 {
		t.Errorf("October income = %+v", got.Income)
	}
	rules, _ = s.LoadRules()
	if len(rules.NextMonth) != 0 {
		t.Errorf("the rule was kept: %+v", rules.NextMonth)
	}
}

func TestARememberedPayeeStaysRemembered(t *testing.T) {
	importHome(t)
	s := testStore(t)
	rules, _ := s.LoadRules()
	rules.SetNextMonth("EXEMPLU SRL", 2)
	s.SaveRules(rules)

	m := press(t, app(t, s), "I")
	m = pick(t, m, "ing.csv")
	m = press(t, m, "enter", "enter", "enter")
	mustContain(t, plain(m), "into 2 months")
	nov, err := s.Load(november)
	if err != nil || len(nov.Income) != 1 || nov.Income[0].Date != "2026-10-02" {
		t.Fatalf("November = %+v, %v", nov, err)
	}
	rules, _ = s.LoadRules()
	if !rules.CountsForNextMonth("EXEMPLU SRL", 2) {
		t.Errorf("rule lost: %+v", rules.NextMonth)
	}
}

// salaryOctober has the October salary plan already replaced by a bank row
// booked on the 30th, which was really November's salary.
func salaryOctober(t *testing.T) (*model.Month, string) {
	t.Helper()
	oct := model.New(october)
	id := oct.Add(model.Income, "Salary", money.FromLei(12650), true)
	imp := bank("k-salary", "EXEMPLU SRL", 12700, "")
	imp.Date = "2026-10-30"
	if err := oct.ReplaceForecast(model.Income, id, imp); err != nil {
		t.Fatal(err)
	}
	return oct, id
}

func TestMovingAnEntryToNextMonthReplacesThePlanThere(t *testing.T) {
	s := testStore(t)
	oct, id := salaryOctober(t)
	saveMonth(t, s, oct)
	nov := model.New(november)
	novSalary := nov.Add(model.Income, "Salary Nov", money.FromLei(12650), false)
	saveMonth(t, s, nov)

	m := appOn(t, s, october, 90, 30)
	m.selectEntry(model.Income, id)
	m = press(t, m, ">")
	if m.move == nil {
		t.Fatal("no question although November plans the salary")
	}
	mustContain(t, plain(m), "Move to November 2026", "Move Salary (12.700,00 lei)", "Salary Nov (12.650,00 lei)", "r replace")
	checkWidth(t, m)

	m = press(t, m, "r")
	mustContain(t, plain(m), "Moved Salary to November 2026, in place of the plan")

	// October keeps its plan, as it was before the import.
	gotOct, _ := s.Load(october)
	if e, _ := gotOct.Find(model.Income, id); !e.IsForecast() || e.Amount != money.FromLei(12650) || !e.Recurring {
		t.Errorf("October plan = %+v", e)
	}
	gotNov, _ := s.Load(november)
	e, _ := gotNov.Find(model.Income, novSalary)
	if e.IsForecast() || e.Amount != money.FromLei(12700) || e.Date != "2026-10-30" || e.Planned != money.FromLei(12650) {
		t.Errorf("November salary = %+v", e)
	}
	rules, _ := s.LoadRules()
	if !rules.CountsForNextMonth("EXEMPLU SRL", 30) {
		t.Errorf("payee not remembered: %+v", rules.NextMonth)
	}

	// In November it shows the day and month the bank booked it.
	m = press(t, m, "n")
	mustContain(t, plain(m), "30 Oct Salary Nov")
}

func TestMovingAnEntryBackForgetsThePayee(t *testing.T) {
	s := testStore(t)
	oct := model.New(october)
	saveMonth(t, s, oct)
	nov := model.New(november)
	imp := bank("k-salary", "EXEMPLU SRL", 12700, "")
	imp.Date = "2026-10-30"
	id, _ := nov.AddImported(model.Income, imp)
	saveMonth(t, s, nov)
	rules, _ := s.LoadRules()
	rules.SetNextMonth("EXEMPLU SRL", 30)
	s.SaveRules(rules)

	m := appOn(t, s, november, 100, 30)
	m.selectEntry(model.Income, id)
	m = press(t, m, "<") // nothing planned in October: no question
	if m.move != nil {
		t.Fatal("asked although October plans nothing")
	}
	mustContain(t, plain(m), "Moved EXEMPLU SRL to October 2026")
	gotNov, _ := s.Load(november)
	gotOct, _ := s.Load(october)
	if len(gotNov.Income) != 0 || len(gotOct.Income) != 1 || gotOct.Income[0].Key != "k-salary" || gotNov.HasKey("k-salary") {
		t.Errorf("November %+v, October %+v", gotNov.Income, gotOct.Income)
	}
	rules, _ = s.LoadRules()
	if len(rules.NextMonth) != 0 {
		t.Errorf("payee still remembered: %+v", rules.NextMonth)
	}
}

func TestMovingCanKeepBothOrBeCancelled(t *testing.T) {
	s := testStore(t)
	oct, id := salaryOctober(t)
	saveMonth(t, s, oct)
	nov := model.New(november)
	nov.Add(model.Income, "Salary Nov", money.FromLei(12650), false)
	saveMonth(t, s, nov)

	m := appOn(t, s, october, 90, 30)
	m.selectEntry(model.Income, id)
	m = press(t, m, ">", "esc")
	if m.move != nil {
		t.Fatal("esc left the question open")
	}
	if got, _ := s.Load(november); len(got.Income) != 1 {
		t.Fatalf("cancel moved the entry: %+v", got.Income)
	}

	m.selectEntry(model.Income, id)
	m = press(t, m, ">", "k")
	gotNov, _ := s.Load(november)
	if len(gotNov.Income) != 2 || !gotNov.Income[0].IsForecast() || gotNov.Income[1].Key != "k-salary" {
		t.Errorf("November = %+v", gotNov.Income)
	}
	// A typed plan moves on its own, with no question.
	gotOct, _ := s.Load(october)
	if len(gotOct.Income) != 1 || !gotOct.Income[0].IsForecast() {
		t.Fatalf("October = %+v", gotOct.Income)
	}
	m.selectEntry(model.Income, gotOct.Income[0].ID)
	m = press(t, m, ">")
	if m.move != nil {
		t.Fatal("a typed entry asked to replace a plan")
	}
	if got, _ := s.Load(november); len(got.Income) != 3 {
		t.Errorf("November = %+v", got.Income)
	}
}
