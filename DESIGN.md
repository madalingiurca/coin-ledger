# budget — monthly budget TUI

A single-binary, full-screen terminal app. You open it, set your income and your big
expenses for the month, and it tells you what's left. Later you reopen it to check where
you stand, and browse how past months went.

## Decisions (locked)

| Area | Choice |
|---|---|
| Stack | Go + Bubble Tea v2 / Bubbles v2 / Lipgloss v2 |
| Storage | One JSON file per month under `~/.budget/` |
| Interface | Pure TUI. No subcommands, no flags. |
| Model | Income lines + expense lines. Items flagged *recurring* carry into new months. |
| Metric | Plan only: `income − planned expenses = left`. No paid/unpaid tracking. |
| History | Month list + leftover trend |
| Currency | RON, formatted `12.500,00 lei` |

Explicitly **out of scope**: categories, savings goals, leftover rollover between months,
transaction-level tracking, multi-currency.

## Data

### Layout

```
~/.budget/                 (override with BUDGET_HOME)
  2026-07.json
  2026-08.json
  2026-09.json
```

One file per month, named `YYYY-MM.json`. No index file, no database — the directory
listing *is* the index. Readable, diffable, syncable via git or iCloud, trivial to back up.

### Shape

```json
{
  "version": 1,
  "month": "2026-09",
  "income": [
    { "id": "8f2a1c", "label": "Salary",   "amount": 12500.00, "recurring": true  },
    { "id": "3b90de", "label": "Freelance","amount":  2000.00, "recurring": false }
  ],
  "expenses": [
    { "id": "c41f77", "label": "Rent",          "amount": 2800.00, "recurring": true  },
    { "id": "0d8ea2", "label": "Utilities",     "amount":  450.00, "recurring": true  },
    { "id": "77b105", "label": "Car insurance", "amount": 1450.00, "recurring": false }
  ],
  "updated_at": "2026-09-01T10:14:22+03:00"
}
```

Income and expenses are the same shape, so one entry type and one editor serve both.
`version` exists so a future format change can migrate rather than guess.

### Money

Amounts are stored in the file as plain decimal numbers (readable), but **never held as
`float64`**. On load they are decoded via `json.Number` and converted to `int64` bani; all
arithmetic is integer. On save they're rendered back with exactly two decimals. This kills
the classic `0.1 + 0.2` class of bug while keeping the file human-friendly.

Input parsing is deliberately lenient — `2800`, `2.800`, `2800,50`, `2 800.50`, `2800 lei`
all resolve to the same value. Display is always canonical: `2.800,50 lei`.

### Writes

Every mutation saves immediately: write to `2026-09.json.tmp` in the same directory, then
`os.Rename` over the target. Atomic, so an interrupted write can't corrupt a month. There
is no save key and no dirty state — nothing to forget.

## Recurring items

No separate template file. An entry simply carries a `recurring` flag.

When you navigate to a month that has no file yet, the app finds the **most recent existing
month before it** and seeds the new month with that month's recurring entries (fresh ids,
same labels and amounts). Non-recurring entries are not carried.

The consequence, which is the point: you mark Rent recurring once and it follows you
forever. Change its amount in September and October inherits the new amount. Delete it in
October because you moved out, and November never sees it again. The current month is
always the template — there's no second place to keep in sync.

The seeded month lives in memory only; it is written to disk on your **first edit**. So
arrowing forward through months to peek doesn't litter `~/.budget` with empty files.

## Screens

### 1. Dashboard (home)

```
  ╭─ budget ─────────────────────────────── September 2026 ─╮
  │                                                          │
  │   INCOME                                   14.500,00 lei │
  │     Salary                            ↻       12.500,00  │
  │   › Freelance project                          2.000,00  │
  │                                                          │
  │   EXPENSES                                  6.750,00 lei │
  │     Rent                              ↻        2.800,00  │
  │     Utilities                         ↻          450,00  │
  │     Car insurance                              1.450,00  │
  │     Vacation deposit                           2.050,00  │
  │                                                          │
  │   ────────────────────────────────────────────────────   │
  │   LEFT                                      7.750,00 lei │
  │   ████████████████████░░░░░░░░░░░░░░░░  53% of income    │
  ╰──────────────────────────────────────────────────────────╯
   a add   e edit   d delete   r recurring   ←/→ month   h history   ? help   q quit
```

One cursor moves through both sections (`↑`/`↓`, `j`/`k`, `g`/`G` for the ends).
`↻` marks a recurring entry. An empty section still shows one selectable line,
so `a` always knows which section it is adding to and there is no section picker
to build — `i` and `x` also add income or an expense directly, from anywhere.
`enter` edits, like `e`.

`←`/`→` step months (`[`/`]` and `p`/`n` do the same), and `t` jumps back to
today. The footer wraps onto a second line rather than dropping hints, because
the narrow terminals where hints get dropped are exactly the ones where `q quit`
is hardest to guess.

### 2. History

```
  ╭─ history ────────────────────────────────────────────────╮
  │   MONTH        INCOME     EXPENSES         LEFT          │
  │ › 2026-09   14.500,00     6.750,00     7.750,00 ████████ │
  │   2026-08   12.500,00     9.100,00     3.400,00 ███      │
  │   2026-07   12.500,00     5.200,00     7.300,00 ███████  │
  │   2026-06   12.500,00    11.900,00       600,00 █        │
  │   2026-05   12.500,00    14.000,00    −1.500,00 ▌over    │
  │                                                          │
  │   Average left over 5 months: 3.510,00 lei               │
  ╰──────────────────────────────────────────────────────────╯
   ↑/↓ select   enter open month   esc back   q quit
```

Bars are scaled to the largest absolute leftover in the window. A month in the
red draws its bar in red and shows its amount in red too, rather than mirroring
around a baseline: a centre line would halve the resolution of every bar to
annotate the rare month, and the colour already carries the sign.

`enter` opens the selected month. The view holds the last 24 months.

### 3. Entry form (modal over dashboard)

```
      ╭─ Add expense ──────────────────────╮
      │  Label   [ Car insurance         ] │
      │  Amount  [ 1450                  ] │
      │  [×] Recurring — carry to future   │
      │                                    │
      │  tab next · enter save · esc cancel│
      ╰────────────────────────────────────╯
```

Same modal for add and edit. Invalid amounts block save with an inline message rather than
an error screen. `space` toggles the checkbox.

### 4. Delete confirmation & help

Delete asks `Delete "Car insurance" (1.450,00 lei)?  y / n` in the footer — one keypress,
no modal. `?` toggles a full-screen key reference.

## Code layout

```
montly-budget/
  go.mod                      module github.com/madalingiurca/budget
  main.go                     resolve BUDGET_HOME, open the store, run the program
  DESIGN.md  README.md
  internal/
    money/
      money.go                Amount (int64 bani): Parse, String, Lei, JSON
      money_test.go
    model/
      model.go                Kind, Entry, Key, Month, totals, SeedFrom
      model_test.go
    store/
      store.go                Load, Save (atomic), Keys, Get, Summaries
      store_test.go           against t.TempDir()
    ui/
      app.go                  root model, screen routing, key dispatch, framing
      month.go                the month screen and every mutation
      form.go                 add/edit modal
      confirm.go              delete prompt
      history.go              month list and trend
      help.go                 key reference
      theme.go                every style, resolved for a light or dark terminal
      layout.go               widths, truncation, wrapping, bars, footers
      ui_test.go              the whole app driven headlessly, key by key
      preview_test.go         prints each screen for eyeballing (not assertions)
```

The rule that keeps this maintainable: `internal/ui` may import `model`, `store`
and `money`; nothing below `ui` ever imports Bubble Tea. The whole budget engine
is testable without a terminal — and the UI turns out to be testable without one
too, by feeding `tea.KeyPressMsg` values straight into `Update` and reading the
rendered frame back.

## Built

All six steps are done; `go test ./...` is green.

1. **Engine.** `money`, `model`, `store`, fully tested headless: lenient RON
   parsing, exact integer arithmetic, atomic saves, month listing, seeding.
2. **Month screen.** Income and expenses, totals, leftover, share-of-income bar.
3. **Editing.** Add, edit, delete, toggle recurring, saved after every change.
4. **Month navigation.** Lazy creation with recurring seeding, written on first edit.
5. **History.** Month list, trend bars, average, `enter` to open a month.
6. **Polish.** Help overlay, empty states, narrow-terminal handling, damaged-file
   handling.

### Running it

```bash
go run .
```

To keep it around:

```bash
go build -o ~/bin/budget .
```

It takes no arguments. `BUDGET_HOME` overrides where months are stored.

## Edge cases the design has to answer

- **First ever run** — dashboard shows `No income yet. Press a to add your first entry.`
  rather than an empty frame.
- **Zero income** — the percentage bar is hidden entirely; no division by zero.
- **Negative left** — rendered red as `−1.500,00 lei · over budget`, and the bar flips.
- **Corrupt month file** — an error banner naming the file, the month opens read-only, and
  the app refuses to overwrite it. Bad JSON should never cost you the file.
- **Narrow terminal** (< ~60 cols) — labels truncate with `…`, the bar drops out, the
  footer collapses to `a e d r ? q`.
- **Very long month history** — history view scrolls and the trend window caps at the last
  24 months.

## Decisions taken while building

Four things the plan left open, resolved in the obvious direction. Say the word
on any of them and they are small changes.

- **`~/.budget` is the home**, overridable with `BUDGET_HOME`. Point that at an
  iCloud or Dropbox folder to sync months between machines.
- **A month is created only when you edit it**, so a month you opened but left
  alone leaves no file and does not appear in history.
- **A damaged earlier month does not block a new one.** When seeding, the store
  walks back to the most recent month it can actually read and shows a warning
  naming the one it skipped, rather than refusing to open the new month.
- **Prose wraps, data truncates.** Explanations and errors break across lines;
  labels and amounts get an ellipsis, so columns stay aligned.

## Not built, by choice

Categories, savings goals, leftover rollover, paid/unpaid tracking and
multi-currency were all ruled out up front and nothing in the code anticipates
them. The shape that would absorb most of them cheaply is `Entry` — it already
carries an id, so an added field would not disturb existing files, and `Load`
tolerates fields being absent.
