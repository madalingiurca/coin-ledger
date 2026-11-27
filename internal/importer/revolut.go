package importer

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// The Revolut account statement CSV is one row per transaction under a fixed
// header. It covers one currency account; the app is RON only, so rows in any
// other currency are reported and left out. Converting RON to another
// currency counts as spending the RON.

var revolutHeader = []string{"Type", "Product", "Started Date", "Completed Date",
	"Description", "Amount", "Fee", "Currency", "State", "Balance"}

func isRevolut(data []byte) bool {
	line, _, _ := bytes.Cut(data, []byte("\n"))
	return bytes.HasPrefix(bytes.TrimSpace(line), []byte("Type,Product,Started Date"))
}

func parseRevolut(data []byte) (Statement, error) {
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return Statement{}, fmt.Errorf("reading the Revolut file: %w", err)
	}
	if len(records) == 0 {
		return Statement{}, ErrUnknownFormat
	}
	col := map[string]int{}
	for i, name := range records[0] {
		col[strings.TrimSpace(name)] = i
	}
	for _, name := range revolutHeader {
		if _, ok := col[name]; !ok {
			return Statement{}, fmt.Errorf("the Revolut file has no %q column", name)
		}
	}

	st := Statement{Bank: "Revolut", Source: model.SourceRevolut}
	keys := newKeyer(model.SourceRevolut)
	var (
		notCompleted, otherCurrency int
		currencies                  = map[string]bool{}
		chained, broken             int
		lastBalance                 money.Amount
		haveBalance                 bool
	)
	for n, rec := range records[1:] {
		line := n + 2
		get := func(name string) string {
			if i := col[name]; i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		if len(rec) == 1 && get("Type") == "" {
			continue // blank line
		}
		if get("State") != "COMPLETED" {
			notCompleted++
			continue
		}
		if c := get("Currency"); c != "RON" {
			otherCurrency++
			currencies[c] = true
			continue
		}

		amount, err := parseDecimal(get("Amount"), '.')
		if err != nil {
			return Statement{}, fmt.Errorf("Revolut line %d: %w", line, err)
		}
		fee := money.Amount(0)
		if f := get("Fee"); f != "" {
			if fee, err = parseDecimal(f, '.'); err != nil {
				return Statement{}, fmt.Errorf("Revolut line %d: fee: %w", line, err)
			}
		}
		net := amount - fee
		if b := get("Balance"); b != "" {
			if bal, err := parseDecimal(b, '.'); err == nil {
				if haveBalance {
					if lastBalance+net == bal {
						chained++
					} else {
						broken++
					}
				}
				lastBalance, haveBalance = bal, true
			}
		}
		if net == 0 {
			continue
		}

		date := get("Completed Date")
		if date == "" {
			date = get("Started Date")
		}
		if len(date) < 10 {
			return Statement{}, fmt.Errorf("Revolut line %d: date %q is not a date", line, date)
		}
		date = date[:10]

		kind := model.Expense
		if net > 0 {
			kind = model.Income
		}
		label := revolutLabel(get("Type"), get("Description"))
		st.Txns = append(st.Txns, Txn{
			Date:   date,
			Kind:   kind,
			Amount: absAmount(net),
			Label:  label,
			Payee:  model.NormalizePayee(label),
			Type:   get("Type"),
			Key: keys.key(get("Type"), get("Product"), get("Started Date"), get("Description"),
				get("Amount"), get("Fee"), get("Currency")),
		})
	}

	if notCompleted > 0 {
		st.Skipped = append(st.Skipped, plural(notCompleted, "row is", "rows are")+" pending, reverted or declined")
	}
	if otherCurrency > 0 {
		names := make([]string, 0, len(currencies))
		for c := range currencies {
			names = append(names, c)
		}
		st.Skipped = append(st.Skipped, plural(otherCurrency, "row is", "rows are")+" in "+strings.Join(names, ", ")+", not RON")
	}
	if chained+broken > 0 {
		st.Check = Check{Done: true, OK: broken == 0}
		if broken == 0 {
			st.Check.Note = "every row matches the running balance"
		} else {
			st.Check.Note = plural(broken, "row does", "rows do") + " not match the running balance"
		}
	}
	if len(st.Txns) == 0 && len(st.Skipped) == 0 {
		return Statement{}, fmt.Errorf("the Revolut file has no transactions")
	}
	return st, nil
}

// revolutLabel names the counterparty of a Revolut row.
func revolutLabel(typ, description string) string {
	d := tidy(description)
	switch {
	case typ == "Exchange":
		return "Revolut exchange"
	case strings.HasPrefix(d, "Transfer to "):
		return strings.TrimPrefix(d, "Transfer to ")
	case strings.HasPrefix(d, "Transfer from "):
		return strings.TrimPrefix(d, "Transfer from ")
	}
	if d == "" {
		return typ
	}
	return d
}

func absAmount(a money.Amount) money.Amount {
	if a < 0 {
		return -a
	}
	return a
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
