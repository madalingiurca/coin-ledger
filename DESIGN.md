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
| `reimburses` | On a refund (income): the expenses it pays back, each as `{ "month": "2026-10", "id": "c41f77", "amount": 282.24 }`. |
| `reimbursed_by` | On an expense: the refunds that paid it back, in the same form, pointing the other way. |
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
  "links": [{ "match": "RATA CREDIT", "label": "Mortgage" }],
  "next_month": [{ "match": "EXEMPLU SRL", "from_day": 27 }],
  "paid_back": ["PayAndReceiveLater"]
}
```

- A **rule** files an expense when its payee *contains* `match`; the longest match wins.
  ING names every shop separately ("CARREFOUR LUJERULUI TABAC"), so one short rule covers
  them all. A rule only ever fills an *empty* category: an expense filed by hand stays put.
- A **link** remembers that rows from a payee replaced a forecast entry with that label,
  so the next import proposes the same replacement whatever the amount.
- A **next-month rule** says rows from a payee booked on `from_day` or later count for the
  month after: the salary due on the 1st arrives early when the 1st is a weekend or a
  holiday. Moving such a row forward (in the import, or with `>` later) teaches it, with the
  earliest day seen; moving a row it moved back forgets it.
- A **paid-back** category holds expenses you expect to get back: money laid out for
  friends or for work. Its expenses with no refund linked show as awaiting one.
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
- **Income** adds up: planned and received are not tied together, so a typed salary counts beside the transfers the bank reports. A bank row that is the salary should replace it in the import; a replaced entry then counts what arrived, once.
- **Transfers** count for nothing.
- **Reimbursements** count for nothing either: the part of an expense a refund paid back,
  and the part of the refund that paid it (see below).

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
3. If rows look like forecast entries, a step lists each pair to replace or keep both. In
   that step `n` counts a row for the month after its date (a salary paid early): it moves
   there, is matched against that month's plan instead, and its payee gets a next-month
   rule. Rows a rule moved are listed there too, so they can be moved back. Duplicate
   detection looks in the row's month and the months either side, so a moved row is never
   imported twice.
4. Enter writes the months. The bank file is never copied; only date, amount, a tidied
   counterparty name and the row key are kept. The account holder's name, ID number and
   address that ING prints on every page are skipped.

Format notes, from the October 2026 exports:

- **ING CSV** is the printed statement poured into cells. A transaction is a start row
  (`01 octombrie 2026`, type, amount) followed by `Key:Value` detail rows. Pages differ in
  column count and the amount sits one column left of its header, so the Debit/Credit
  columns are found again at every page header. Amounts are `1.384,95`. The counterparty
  is the card terminal (`Terminal`, or `Tranzactie la` from November 2026) without its country and city, and without the store number,
  booking reference or order number that would make one shop many ("MOL 91022", "BOLT.EUO2601010001"),
  else `Beneficiar` / `Ordonator`, else the transaction type; Round Ups are labelled "Round Up"
  and cash withdrawals "Cash withdrawal". The `.xls` export is the
  same layout in an xlsx and the PDF is layout text; neither is supported.
- **Revolut CSV** is one row per transaction with a fixed header, ISO dates and signed
  `-117.13` amounts. Only `COMPLETED` rows in RON are imported; the fee is part of the
  cost; the booking date is the completed date. An export covers one currency account.

## Screens

Every screen is a set of bordered panes, after lazygit. A pane's title sits in its top
border and, for a list, where the cursor is (`9 of 24`) in the bottom one. The main pane's
top border names every screen as a tab, the one on show in full
(`[1] October 2026 · 2 History · 3 Payees · 4 Import`), and the numbers jump to them. On a
narrow terminal the other tabs shrink to their numbers. The focused pane's border and title are Rosso
Corsa 322 (`#D40000`); the rest are in the line colour. The selected row is a background
bar. The last line of the terminal is the key bar, one line of `Action: key` hints; hints
that do not fit are left out, and `Keys: ?` always stays. A status message takes the key
bar's place until the next key. Dialogs (the form, categorise, rename, delete, the
keybindings menu) open as a focused pane over the middle of the screen, with their keys in
their bottom border, and the screen behind keeps its place.

**Month** (home). Income and expenses are two panes, one over the other: income sized to
what it holds (at most a third of the height), expenses taking the rest. The pane holding
the cursor is the focused one, and `tab` moves the cursor to the next (`shift+tab` to the previous). A Totals pane sits
beside them from 100 columns up, and a Left pane under them below that. The panes fill
the terminal whatever its size, with no maximum width; the column beside the month takes
a third of it. Below its figures the Totals pane fills the rest of that column with a
breakdown of the month, which `s` steps through and `S` steps back (the choice stays while
paging months; the heading says `2/6 s`):
by category, largest first, each as a bar of its share of the expenses; by label, the
expenses added up by payee (or typed label) so every Bolt ride is one bar; the largest
single expenses; by week, what the bank booked on the 1st–7th, 8th–14th and so on, with
rows counted from the month before or after on bars of their own (plans have no date, so
only bank rows count); income by source, without transfers; and against the month before,
each category's amount now and how much more (red) or less (green) that is, biggest change
first. By category, by label and largest all add up to the month's expense total: a
category counts its plan or its bank rows, whichever is more, and only that side's entries
are taken by label, so a groceries budget and the shop rows that spend it are not both
counted. `s` was picked because the arrows, brackets and `tab` already move around.
A dim dotted leader (`Bolt ········ 27,00`) runs from each entry, category, payee and
total to its amount, so a wide pane stays easy to read across; a recurring entry's `↻`
sits on that leader in one column. Section and day headings keep plain space. Income, then expenses under category headers
(`▸ Groceries (3)  1.230,00 of 1.500,00`), Transfers last. Expenses with no category
are not a category there but a To categorise pane of their own, laid out by day and always
open: beside the month above the Totals, only as tall as what it lists, when the terminal
has 44 columns to spare for it and 70 for the month, otherwise between income and expenses. `tab` visits income, To categorise and expenses in
turn, `shift+tab` the other way round. Categories start folded, header and total only; `enter` or `space` on a header unfolds it (`▾`), and it stays unfolded while paging months. The arrows work as in a file tree: `→` unfolds the header under the cursor and, on one already open, steps onto its first entry; `←` folds the category from its header or any of its entries and leaves the cursor on the header. Months page with `[` `]` (or `p` `n`), not the arrows, so opening a category never changes month by accident. Inside a category the bank rows sit under a heading per day (`Wed 04 Nov`, with that day's total), the plan under `Planned`. A month
where nothing has a category is one plain list, as before. Forecast entries the bank has
not reported yet are dimmed once a month has actuals; imported entries show their day, and
a replaced one its planned amount; one booked in another month shows that month too
(`30 Oct Salary`). `>` and `<` move the selected entry to the next or previous month. If a
bank row lands on something planned there, a popup asks whether it replaces the plan or
sits beside it; a bank row that had replaced a plan leaves that plan behind as it was.
The list scrolls around the cursor; the notes above
it and the totals stay in place.

**Categorise** (`c` on an expense). Pick a category or type a new one. For an imported
expense the dialog also offers to remember the payee: the match text starts as the whole
payee and can be shortened, and with the box ticked every uncategorised expense from that
payee, in every month, is filed too. The box starts unticked: only this one is filed.
Afterwards the cursor stays in the list it was in, on the next entry, rather than following
the expense to its category, so a run of uncategorised expenses is worked through top to
bottom. `space` marks an expense (`✓`) and moves down; with marks set, `c` files every
marked expense in one category (no payee rule, as they can come from different payees).
The pane title counts the marks, `esc` clears them, and changing month drops them.

**Link a refund** (`b` on an entry). On a refund, the expenses it can pay back; on an
expense, the income that can pay it back (see Reimbursements). Already linked ones are
ticked, the same amount comes first, then expenses in paid-back categories; typing filters.
`space` ticks or unticks and `enter` saves the difference. `b` on a category header marks
it as paid back later, or not.

**Rename a category** (`e` on its header). The new name replaces the old in the category list,
in every payee rule and in every month on disk; naming it after an existing category merges
the two. Transfers is built in and cannot be renamed.

**Uncategorised payees** (`u`). Every payee with uncategorised expenses, across all months,
with count and total. `enter` gives the payee a category for good. `→` opens it to file its
transactions one by one, for shops where one visit is fuel and the next is coffee.

**Import** (`I`), described above. **History** (`h`): the last 24 months with projected
leftover bars. **Form** (`a`, `i`, `x`, `e`): label, amount, category for expenses (`→`
completes a known one), recurring. **Keybindings** (`?`): every key the screen answers
to, then the global ones. Typing filters the list and `enter` runs the chosen binding by
pressing its key. The key bar is drawn from the same table (`keys.go`), so the two agree.

## Reimbursements

Some expenses come back: a team pizza the employer refunds, a dinner friends pay their
share of. Counted as they are, the expense and the refund both inflate the month. So a
refund (income) can be linked to the expenses it pays back, with `b` on either side.

- A link carries an amount, the smaller of what is still open on each side. A refund
  smaller than the expense leaves the difference counted; a refund larger than its expenses
  keeps the rest, counted as income until it is linked to another expense. One expense can
  be paid back by several refunds (each friend's share), and one refund can pay back several
  expenses.
- Both sides record the link, the refund in `reimburses` and the expense in `reimbursed_by`,
  so each month counts its totals from its own file. A refund usually lands in a later
  month: the dialog looks for expenses in the refund's month and the two before, and for
  refunds in the expense's month and the two after.
- A linked pair reads `refund, not counted` and `paid back`, dimmed like transfers; a
  partial one says how much. In a **paid-back category** (`b` on its header) an expense
  with nothing linked reads `awaiting refund`, the header counts the open ones, and the
  totals pane shows what is *still to come back*.
- Deleting a linked entry removes the link on the other side too, even in another month,
  and what it paid back counts again. Moving a linked entry to another month is refused
  until it is unlinked, since the other side points at it by month and id.

## Code layout

```
internal/
  money/      Amount (int64 bani): Parse, String, Lei, JSON
  model/      model.go   Entry, Month, totals, categories, forecast and actual, SeedFrom
              rules.go   categories, payee rules, forecast links
              match.go   forecast entries a bank row may fulfil
              refund.go  reimbursement links between refunds and expenses
  store/      store.go   month files: Load, Save (atomic), Keys, Get, Summaries
              rules.go   categories.json, rule and rename sweeps, payee lists
              state.go   state.json
  importer/   importer.go, ing.go, revolut.go; testdata holds invented statements in the
              real layouts
  ui/         app.go routing and framing; month.go; form.go; categorise.go; payees.go;
              importer.go; history.go; keys.go (key table, key bar, ? menu); confirm.go; move.go;
              link.go (refund dialog); refund.go (paid-back categories);
              frame.go (panes, key bar, popups); theme.go; layout.go
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
  truncates. The key bar is one line and leaves out the hints that do not fit; `?`
  lists all of them.
