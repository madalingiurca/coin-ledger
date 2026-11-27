# budget

A monthly budget you keep in the terminal. Plan what comes in and what you expect
to spend, import your ING and Revolut statements as the month goes, and see what
you have spent against the plan and what is projected to be left.

```bash
go build -o ~/bin/budget .
budget
```

It takes no arguments and no flags. Everything happens on screen:

| Key | |
|---|---|
| `a` / `i` / `x` | add (to the section under the cursor / income / expense) |
| `e`, `enter` | edit; on a category header, `e` renames the category everywhere and `enter` or `space` unfolds or folds it (categories start folded) |
| `d` | delete |
| `r` | carry the entry into future months (`↻`) |
| `c` | put an expense in a category, and optionally every expense from its payee; the cursor stays in the list you are working through |
| `space` | mark expenses, then `c` files them all in one category; `esc` clears the marks |
| `tab` | next pane: income, To categorise, expenses |
| `u` | payees that still have no category; `→` on one files its transactions one by one |
| `>` / `<` | move the entry to the next / previous month |
| `I` | import a bank statement |
| `←` / `→`, `t` | previous / next month, back to this month |
| `h` | history |
| `1`–`4` | jump to month, history, uncategorised payees, import |
| `?` | every key, filterable; `enter` runs one |

**Import.** Export the statement as CSV (ING: the *Extras de cont* CSV; Revolut: the
account statement CSV, one per currency account), press `I`, pick the file. The
preview shows what goes into which month and whether it matches the bank's balance;
nothing is written until you confirm. Bank rows that look like something you
planned, such as the mortgage, are offered to replace the planned entry. A salary
paid early, on the 27th for the 1st, can be counted for next month with `n`, and
the payee's late rows go there by themselves from then on. Importing the same file
again adds nothing.

**Categories.** Categorise one Bolt ride as Transport and every Bolt ride follows,
in every month and on every later import. Expenses still without a category wait in
their own To categorise pane, beside the month on a wide terminal. Money moving between your own ING and
Revolut accounts is filed under Transfers and not counted.

Months are stored one JSON file each in `~/.budget`, or in `$BUDGET_HOME` when set,
next to `categories.json`. The files are plain and hand-editable; writes are atomic
and every change saves immediately. A file that cannot be parsed is shown read-only
and never overwritten.

See [DESIGN.md](DESIGN.md) for how it works.
