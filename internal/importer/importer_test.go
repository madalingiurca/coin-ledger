package importer

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

func load(t *testing.T, name string) Statement {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return st
}

type want struct {
	date   string
	kind   model.Kind
	amount money.Amount
	label  string
}

func check(t *testing.T, got []Txn, wants []want) {
	t.Helper()
	if len(got) != len(wants) {
		for _, g := range got {
			t.Logf("%s %s %v %q", g.Date, g.Kind, g.Amount, g.Label)
		}
		t.Fatalf("%d transactions, want %d", len(got), len(wants))
	}
	for i, w := range wants {
		g := got[i]
		if g.Date != w.date || g.Kind != w.kind || g.Amount != w.amount || g.Label != w.label {
			t.Errorf("row %d = %s %s %v %q, want %s %s %v %q", i,
				g.Date, g.Kind, g.Amount, g.Label, w.date, w.kind, w.amount, w.label)
		}
		if g.Payee != model.NormalizePayee(g.Label) {
			t.Errorf("row %d payee %q is not the normalised label", i, g.Payee)
		}
	}
}

func TestING(t *testing.T) {
	st := load(t, "ing.csv")
	if st.Bank != "ING" || st.Source != model.SourceING {
		t.Errorf("bank = %q / %q", st.Bank, st.Source)
	}
	in, out := model.Income, model.Expense
	check(t, st.Txns, []want{
		{"2026-10-01", out, 3350, "FAST MARKET"},
		{"2026-10-01", out, 150, "Round Up"},
		{"2026-10-02", in, 1265000, "EXEMPLU SRL"},
		{"2026-10-02", out, 50000, "Revolut**9999*"},
		{"2026-10-05", out, 120499, "RYANAIR"},
		{"2026-10-05", out, 540, "VENDING ZONE SRL"},
		{"2026-10-05", out, 540, "VENDING ZONE SRL"},
		{"2026-10-10", out, 25000, "Maria Ionescu"},
		{"2026-10-12", in, 7500, "Andrei Pop"},
		{"2026-10-15", out, 215470, "Rata Credit"},
	})
	if !st.Check.Done || !st.Check.OK {
		t.Errorf("check = %+v", st.Check)
	}
	if st.In() != 1272500 || st.Out() != 415549 {
		t.Errorf("in %v out %v", st.In(), st.Out())
	}
	if from, to := st.Span(); from != "2026-10-01" || to != "2026-10-15" {
		t.Errorf("span = %s..%s", from, to)
	}
}

// Nothing from the account holder block may leak into what is imported.
func TestINGKeepsNoPersonalData(t *testing.T) {
	st := load(t, "ing.csv")
	for _, tx := range st.Txns {
		all := tx.Label + tx.Payee + tx.Type + tx.Key
		for _, secret := range []string{"Popescu", "1000000000000", "Exemplu, Nr", "RO00INGB"} {
			if strings.Contains(all, secret) {
				t.Errorf("%q leaked into %+v", secret, tx)
			}
		}
	}
}

func TestINGBalanceMismatchIsReported(t *testing.T) {
	data, _ := os.ReadFile("testdata/ing.csv")
	tampered := strings.Replace(string(data), "9.569,51", "9.569,52", 1)
	st, err := Parse([]byte(tampered))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Check.Done || st.Check.OK || !strings.Contains(st.Check.Note, "balance moved") {
		t.Errorf("check = %+v", st.Check)
	}
}

func TestINGWithoutBalancesHasNoCheck(t *testing.T) {
	data, _ := os.ReadFile("testdata/ing.csv")
	text := string(data)
	text = text[:strings.Index(text, "Sold ini")]
	st, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if st.Check.Done {
		t.Errorf("check = %+v", st.Check)
	}
}

func TestINGRejectsBrokenRows(t *testing.T) {
	header := ",Data,,,Detalii tranzactie,,,Debit,,Credit\n"
	for name, body := range map[string]string{
		"bad amount":   "01 octombrie 2026,,,Cumparare POS,,,\"33.5\",,,\n",
		"no amount":    "01 octombrie 2026,,,Cumparare POS,,,,,,\n",
		"two amounts":  "01 octombrie 2026,,,Cumparare POS,,,\"1,00\",,\"2,00\",\n",
		"stray column": "01 octombrie 2026,,,Cumparare POS,\"1,00\",,,,,\n",
		"bad month":    "01 brumarel 2026,,,Cumparare POS,,,\"1,00\",,,\n",
		"empty":        "",
	} {
		if _, err := Parse([]byte(header + body)); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

// The same row in two exports gets the same key; identical rows in one file
// do not collide.
func TestKeysAreStableAndUnique(t *testing.T) {
	for _, name := range []string{"ing.csv", "revolut.csv"} {
		a, b := load(t, name), load(t, name)
		seen := map[string]bool{}
		for i := range a.Txns {
			if a.Txns[i].Key != b.Txns[i].Key {
				t.Errorf("%s row %d: key changed between parses", name, i)
			}
			if seen[a.Txns[i].Key] {
				t.Errorf("%s row %d: duplicate key %s", name, i, a.Txns[i].Key)
			}
			seen[a.Txns[i].Key] = true
		}
	}

	// A row keeps its key when other rows around it change.
	data, _ := os.ReadFile("testdata/revolut.csv")
	lines := strings.Split(string(data), "\n")
	shorter := strings.Join(append(lines[:1:1], lines[2:]...), "\n") // drop the top-up
	st, err := Parse([]byte(shorter))
	if err != nil {
		t.Fatal(err)
	}
	full := load(t, "revolut.csv")
	if st.Txns[0].Key != full.Txns[1].Key {
		t.Error("a row's key depends on the rows before it")
	}
}

func TestRevolut(t *testing.T) {
	st := load(t, "revolut.csv")
	if st.Bank != "Revolut" || st.Source != model.SourceRevolut {
		t.Errorf("bank = %q / %q", st.Bank, st.Source)
	}
	in, out := model.Income, model.Expense
	check(t, st.Txns, []want{
		{"2026-10-02", in, 50000, "Top-up by *1111"},
		{"2026-10-03", out, 5999, "Netflix"}, // booked when completed
		{"2026-10-04", out, 40000, "Revolut exchange"},
		{"2026-10-06", out, 2050, "Maria Ionescu"}, // the fee is part of the cost
		{"2026-10-07", in, 1500, "Andrei Pop"},
		{"2026-10-09", out, 1200, "Bolt"},
		{"2026-10-09", out, 1200, "Bolt"},
	})
	if !st.Check.Done || !st.Check.OK {
		t.Errorf("check = %+v", st.Check)
	}
	got := strings.Join(st.Skipped, "; ")
	if !strings.Contains(got, "1 row is pending") || !strings.Contains(got, "1 row is in EUR") {
		t.Errorf("skipped = %q", got)
	}
}

func TestRevolutBalanceMismatchIsReported(t *testing.T) {
	data, _ := os.ReadFile("testdata/revolut.csv")
	tampered := strings.Replace(string(data), "-59.99,0.00,RON,COMPLETED,440.57", "-59.98,0.00,RON,COMPLETED,440.57", 1)
	st, err := Parse([]byte(tampered))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Check.Done || st.Check.OK {
		t.Errorf("check = %+v", st.Check)
	}
}

func TestRevolutRejectsBrokenRows(t *testing.T) {
	header := "Type,Product,Started Date,Completed Date,Description,Amount,Fee,Currency,State,Balance\n"
	for name, body := range map[string]string{
		"bad amount": "Card Payment,Current,2026-10-02 13:28:50,2026-10-03 16:20:26,X,-59.9,0.00,RON,COMPLETED,1.00\n",
		"bad date":   "Card Payment,Current,x,,X,-59.90,0.00,RON,COMPLETED,1.00\n",
		"nothing":    "",
	} {
		if _, err := Parse([]byte(header + body)); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
	if _, err := Parse([]byte("Type,Product,Started Date\nx,y,z\n")); err == nil {
		t.Error("a file missing columns parsed")
	}
}

func TestUnknownFile(t *testing.T) {
	for _, data := range []string{"", "date,amount\n2026-10-01,5\n", "%PDF-1.4"} {
		if _, err := Parse([]byte(data)); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("Parse(%q) = %v", data, err)
		}
	}
}

func TestByteOrderMarkIsIgnored(t *testing.T) {
	data, _ := os.ReadFile("testdata/revolut.csv")
	if _, err := Parse(append([]byte("\xef\xbb\xbf"), data...)); err != nil {
		t.Error(err)
	}
}

func TestDefaultRulesRecogniseOwnTransfers(t *testing.T) {
	rules := model.NewRules()
	ing, rev := load(t, "ing.csv"), load(t, "revolut.csv")
	for _, tx := range []Txn{ing.Txns[3], rev.Txns[0]} {
		if c, _ := rules.CategoryFor(tx.Payee); c != model.Transfers {
			t.Errorf("%q is not a transfer by default", tx.Label)
		}
	}
	if c, ok := rules.CategoryFor(ing.Txns[1].Payee); ok {
		t.Errorf("Round Up is filed under %q by default", c)
	}
}

func TestParseDecimal(t *testing.T) {
	for _, c := range []struct {
		in   string
		sep  byte
		want money.Amount
	}{
		{"1.384,95", ',', 138495}, {"33,50", ',', 3350}, {"0,37", ',', 37}, {"12.650,00", ',', 1265000},
		{"-117.13", '.', -11713}, {"500.00", '.', 50000}, {"1,234.50", '.', 123450},
	} {
		got, err := parseDecimal(c.in, c.sep)
		if err != nil || got != c.want {
			t.Errorf("parseDecimal(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "1", "1,5", "1,500", ",50", "1.384,95x", "a,bc"} {
		if _, err := parseDecimal(bad, ','); err == nil {
			t.Errorf("parseDecimal(%q) succeeded", bad)
		}
	}
}

func TestShopName(t *testing.T) {
	for in, want := range map[string]string{
		"MOL 91022 Bucuresti":       "MOL",
		"OMV 1318":                  "OMV",
		"PENNY 4498 BUC BDTIMISOAR": "PENNY",
		"MEGAIMAGE 0002 MILITARI":   "MEGAIMAGE",
		"CARREFOUR LUJERULUI SCO 1": "CARREFOUR LUJERULUI SCO",
		"BOLT.EUO2601010001":        "BOLT.EU",
		"BOLT.EU/O/2601020002":      "BOLT.EU",
		"RYANAIR     Q47ZRT2":       "RYANAIR",
		"Wizz Air MalVK9VWI":        "Wizz Air",
		"METRO TIM 16B C2":          "METRO TIM 16B C2",
		"W061 JYSK FRIGOCOM C1":     "W061 JYSK FRIGOCOM C1",
		"Revolut**9999*":            "Revolut**9999*",
		"FAST MARKET":               "FAST MARKET",
		"5513":                      "5513",
		"INMBUCPTAROMANA677":        "INMBUCPTAROMANA677",
	} {
		if got := shopName(in); got != want {
			t.Errorf("shopName(%q) = %q, want %q", in, got, want)
		}
	}
}

// From November 2026 ING names the card terminal "Tranzactie la".
func TestINGTranzactieLa(t *testing.T) {
	st := load(t, "ing-nov.csv")
	in, out := model.Income, model.Expense
	check(t, st.Txns, []want{
		{"2026-11-01", out, 32081, "MOL"},
		{"2026-11-02", out, 498, "Round Up"},
		{"2026-11-02", in, 2192, "EXEMPLU SHOP"},
		{"2026-11-05", out, 10000, "Revolut**9999*"},
	})
	if !st.Check.Done || !st.Check.OK {
		t.Errorf("check = %+v", st.Check)
	}
	if c, _ := model.NewRules().CategoryFor(st.Txns[3].Payee); c != model.Transfers {
		t.Errorf("the card top-up to Revolut is filed under %q", c)
	}
}
