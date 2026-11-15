# budget — monthly budget TUI

A single-binary, full-screen terminal app. You plan the month (income, the big expenses,
a budget per category), import your bank statements as the month goes, and it shows
what you have spent against what you planned and what is projected to be left.

## Decisions

| Area | Choice |
|---|---|
| Stack | Go + Bubble Tea v2 / Bubbles v2 / Lipgloss v2 |
| Storage | One JSON file per month under `~/.budget/`, plus `categories.json` |
| Interface | Pure TUI. No subcommands, no flags. |
| Model | Income and expense entries. Typed entries are the **forecast**; imported ones the **actual**. |
| Categories | Expenses only. Payee rules file bank rows automatically. |
| Import | ING "Extras de cont" CSV and Revolut account statement CSV, picked in an in-app file browser |
| Metric | Spent so far, and projected `income − expenses = left` |
| History | Month list + leftover trend |
| Currency | RON only, formatted `12.500,00 lei` |

Out of scope: savings goals, leftover rollover between months, multi-currency (Revolut
rows in other currencies are left out; converting RON counts as spending it), budgets
per category other than as forecast entries.

## Data

### Layout

```
~/.budget/                 (override with BUDGET_HOME)
  2026-09.json             one file per month
  2026-10.json
  categories.json          categories, payee rules, forecast links
  state.json               the last import folder; losing it costs nothing
```

The directory listing is the index of months: no manifest to fall out of sync.

### Month

```json
{
  "version": 1,
  "month": "2026-10",
  "income": [
    { "id": "8f2a1c", "label": "Salary", "amount": 12500.00, "recurring": true }
  ],
  "expenses": [
    { "id": "c41f77", "label": "Groceries budget", "amount": 1500.00, "recurring": true,
      "category": "Groceries" },
    { "id": "0d8ea2", "label": "Mortgage", "amount": 2154.70, "recurring": true,
      "category": "Loan", "planned": 2300.00,
      "date": "2026-10-02", "payee": "RATA CREDIT", "source": "ing", "key": "a41c09e7d3b2f001" },
    { "id": "77b105", "label": "Bolt", "amount": 27.00, "recurring": false,
      "category": "Transport",
      "date": "2026-10-05", "payee": "BOLT", "source": "revolut", "key": "5e1d7c0a9b3f2e44" }
  ],
  "skipped_keys": ["9c2b…"],
  "updated_at": "2026-10-31T10:14:22+02:00"
}
```

Every field after `recurring` is optional and left out when empty, so a hand-typed month
reads as before.

| Field | Meaning |
|---|---|
| `category` | Expense category; empty means uncategorised. |
| `source` | `ing` or `revolut` on an imported (actual) entry; absent on a forecast. |
| `date`, `payee`, `key` | The bank row: booking date, normalised payee rules match on, and a stable hash so a row imported twice is added once. |
| `planned` | The forecast amount an actual replaced, kept so the difference shows. |
| `skipped_keys` | Bank rows imported and then deleted, so re-importing does not bring them back. |

### Categories file

```json
{
  "version": 1,
  "categories": ["Groceries", "Loan", "Savings", "Transfers", "Transport"],
  "rules": [
    { "match": "REVOLUT**",  "category": "Transfers" },
    { "match": "TOP-UP BY",  "category": "Transfers" },
    { "match": "BOLT",       "category": "Transport" },
    { "match": "CARREFOUR",  "category": "Groceries" }
  ],
  "links": [{ "match": "RATA CREDIT", "label": "Mortgage" }]
}
```

- A **rule** files an expense when its payee *contains* `match`; the longest match wins.
  ING names every shop separately ("CARREFOUR LUJERULUI TABAC"), so one short rule covers
  them all. A rule only ever fills an *empty* category: an expense filed by hand stays put.
- A **link** remembers that rows from a payee replaced a forecast entry with that label,
  so the next import proposes the same replacement whatever the amount.
- **Transfers** is built in and never counted. Out of the box it catches money moving
  between your own ING and Revolut accounts (the ING card top-up and the matching Revolut
  `Top-up by` row). Round Ups and savings deposits *do* count: they arrive as "Round Up" or
  the savings transfer, for you to file as Savings.

A damaged categories file is reported and never written over; the app runs on the
defaults in memory.

### Money

Amounts are stored as plain decimals but **never held as `float64`**: they are decoded via
`json.Number` into `int64` bani, and all arithmetic is integer. Typed input is lenient
(`2800`, `2.800`, `2800,50`, `2 800.50`, `2800 lei`); bank files are parsed strictly, since
a bank changing its format must fail loudly rather than be guessed at.

### Writes

Every change saves immediately through a temporary file renamed over the target, so an
interrupted write cannot corrupt a month. A month file that cannot be parsed is shown
read-only and never overwritten; imports and rule sweeps step around it.

## Forecast and actual

Typed entries are the plan; imported entries are what the bank reports. Totals lay one
over the other:

- **Each category counts the larger of its forecast and its actual.** Plan "Groceries 1.500"
  and spend 1.230 and the month still expects 1.500; spend 1.700 and it counts 1.700. A plan
  and the bank rows that fulfil it are never counted twice.
- **Uncategorised forecast and actual add up**, because nothing ties them together.
- **Income** works the same way as one group.
- **Transfers** count for nothing.

So a month with no imports totals exactly its plan, and the header shows *spent so far*
(actuals) above *left* (projected).

When an imported row looks like a specific forecast entry (a remembered link, the same
amount, or within 10%) the import asks what it is. **Replace**: the entry keeps its id,
label, category and recurring flag, takes the bank's amount and date, and remembers
`planned`. **Keep both**: the row is added in the plan's category, so the category's max
rule prevents double counting. **Unrelated**: the row is added on its own. A link or the
same amount starts as replace, a near amount as keep both.

## Recurring entries

No separate template. When you open a month that has no file yet, the app finds the most
recent readable month before it and seeds the new one with that month's recurring entries,
as forecasts: a replaced entry carries its *planned* amount forward, not last month's bank
figure, and no bank data is copied. The seeded month stays in memory until the first edit
or import, so browsing ahead leaves no files.

## Import

1. `I` opens a file browser: folders, then CSV files newest first, starting in the folder
   of the last import or `~/Downloads`.
2. The bank is detected from the file's contents. The preview shows, per month, the income
   and expenses to add, how many the rules file, how many are uncategorised, transfers,
   rows already imported, and whether the file matches the bank's own balance (ING: opening
   + transactions = closing; Revolut: the running balance on every row).
3. If rows look like forecast entries, a step lists each pair to replace or keep both.
4. Enter writes the months. The bank file is never copied; only date, amount, a tidied
   counterparty name and the row key are kept. The account holder's name, ID number and
   address that ING prints on every page are skipped.

Format notes, from the October 2026 exports:

- **ING CSV** is the printed statement poured into cells. A transaction is a start row
  (`01 octombrie 2026`, type, amount) followed by `Key:Value` detail rows. Pages differ in
  column count and the amount sits one column left of its header, so the Debit/Credit
  columns are found again at every page header. Amounts are `1.384,95`. The counterparty
  is the card `Terminal` without its country and city, and without the store number,
  booking reference or order number that would make one shop many ("MOL 91022", "BOLT.EUO2601010001"),
  else `Beneficiar` / `Ordonator`, else the transaction type; Round Ups are labelled "Round Up"
  and cash withdrawals "Cash withdrawal". The `.xls` export is the
  same layout in an xlsx and the PDF is layout text; neither is supported.
- **Revolut CSV** is one row per transaction with a fixed header, ISO dates and signed
  `-117.13` amounts. Only `COMPLETED` rows in RON are imported; the fee is part of the
  cost; the booking date is the completed date. An export covers one currency account.

## Screens

**Month** (home). Income, then expenses under collapsible category headers
(`▾ Groceries (3)  1.230,00 of 1.500,00`), Uncategorised first and Transfers last. A month
where nothing has a category is one plain list, as before. Forecast entries the bank has
not reported yet are dimmed once a month has actuals; imported entries show their day, and
a replaced one its planned amount. The list scrolls around the cursor, keeping the title
and totals in place.

**Categorise** (`c` on an expense). Pick a category or type a new one. For an imported
expense the dialog also offers to remember the payee: the match text starts as the whole
payee and can be shortened, and with the box ticked every uncategorised expense from that
payee, in every month, is filed too. Untick it to file just this one.

**Uncategorised payees** (`u`). Every payee with uncategorised expenses, across all months,
with count and total. `enter` gives the payee a category for good. `→` opens it to file its
transactions one by one, for shops where one visit is fuel and the next is coffee.

**Import** (`I`), described above. **History** (`h`): the last 24 months with projected
leftover bars. **Form** (`a`, `i`, `x`, `e`): label, amount, category for expenses (`→`
completes a known one), recurring. **Help** (`?`).

## Code layout

```
internal/
  money/      Amount (int64 bani): Parse, String, Lei, JSON
  model/      model.go   Entry, Month, totals, categories, forecast and actual, SeedFrom
              rules.go   categories, payee rules, forecast links
              match.go   forecast entries a bank row may fulfil
  store/      store.go   month files: Load, Save (atomic), Keys, Get, Summaries
              rules.go   categories.json, rule and rename sweeps, payee lists
              state.go   state.json
  importer/   importer.go, ing.go, revolut.go; testdata holds invented statements in the
              real layouts
  ui/         app.go routing and framing; month.go; form.go; categorise.go; payees.go;
              importer.go; history.go; help.go; confirm.go; theme.go; layout.go
```

Nothing below `ui` imports Bubble Tea, and the UI itself is tested headlessly by feeding
key presses into `Update` and reading the rendered frame (`go test ./...`).
`go test ./internal/ui -run TestPreview -v` prints every screen for eyeballing.

## Edge cases

- **Importing the same file twice** adds nothing; an overlapping export adds only the new
  rows. Two identical rows in one file (same shop, same amount, same day) stay two.
- **A statement that does not reconcile** is flagged in red in the preview; you can still
  import it.
- **Damaged month or categories file**: shown, never overwritten, skipped by imports and
  sweeps.
- **First run, zero income, overspending, narrow terminals**: as before. Prose wraps, data
  truncates, and the footer wraps rather than dropping hints.
