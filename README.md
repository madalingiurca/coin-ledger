# Coin Ledger

A monthly budget you keep in the terminal. Plan what comes in and what you expect
to spend, import your ING and Revolut statements as the month goes, and see what
you have spent against the plan and what is projected to be left.

```bash
just install   # tag the next version and put budget in ~/.local/bin
budget
```

`just build`, `just run` and `just test` do what they say, and plain `just` lists the
rest (install it with `brew install just`). `just install` tags the next version
(`minor` or `major` to bump those) and installs the binary into `~/.local/bin`; it asks
first when you are not on `main` or have uncommitted changes.

No arguments, no flags. Everything happens on screen, and `?` lists every key: type
to filter, `enter` to run one. The ones you need most:

| Key | |
|---|---|
| `a` / `e` / `d` | add, edit, delete |
| `c` | categorise an expense, or every expense from its payee |
| `space` then `c` | mark several expenses and categorise them together |
| `r` | carry the entry into future months (`↻`) |
| `b` | link a refund to the expenses it pays back |
| `>` / `<` | move the entry to the next / previous month |
| `tab` / `shift+tab` | next / previous pane |
| `←` / `→` | fold / unfold a category; `-` / `=` for all of them |
| `s` | cycle the statistics beside the month |
| `[` / `]`, `t` | previous / next month, back to this month |
| `1`–`4` | month, history, uncategorised payees, import |
| `I` | import a bank statement |

**Import.** Export the statement as CSV (ING: *Extras de cont*; Revolut: the account
statement, one per currency), press `I` and pick the file. The preview shows what goes
into which month and whether it matches the bank's balance; nothing is written until
you confirm. Rows that look like something you planned, such as the mortgage, can
replace the plan, and an early salary can count for next month with `n`. Importing
the same file twice adds nothing.

**Categories.** Categorise one Bolt ride as Transport and every Bolt ride follows, in
every month and on every later import. Money moving between your own ING and Revolut
accounts is a Transfer and not counted.

**Reimbursements.** Press `b` on a refund and tick the expenses it pays back: they stop
counting, and a partial refund leaves the difference counted.

Months are plain JSON files in `~/.budget` (or `$BUDGET_HOME`), one per month, next to
`categories.json`. Every change saves at once, and a file that cannot be read is shown
but never overwritten.

See [DESIGN.md](DESIGN.md) for how it works.
