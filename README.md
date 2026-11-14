# budget

A monthly budget you keep in the terminal. Set what comes in and the big things
you have planned to spend, then check back to see where you stand.

```bash
go build -o ~/bin/budget .
budget
```

It takes no arguments and no flags. Everything happens on screen: `a` adds to
the section the cursor is in, `e` edits, `d` deletes, `r` marks an entry as
recurring, `←`/`→` move between months, `h` opens history, `?` lists every key.

Entries marked recurring (`↻`) are carried into each new month you open. The
current month is the template — change rent in September and October inherits
it; delete it in October and November never sees it.

Months are stored one JSON file each in `~/.budget`, or in `$BUDGET_HOME` when
that is set. The files are plain and hand-editable:

```json
{
  "version": 1,
  "month": "2026-09",
  "income":   [{ "id": "94c789", "label": "Salary", "amount": 12500.00, "recurring": true }],
  "expenses": [{ "id": "5af245", "label": "Rent",   "amount":  2800.00, "recurring": true }],
  "updated_at": "2026-09-01T00:32:13+03:00"
}
```

Writes are atomic and every change saves immediately, so there is no save key
and nothing to lose. A file that cannot be parsed is shown read-only and is
never overwritten.

See [DESIGN.md](DESIGN.md) for why it is built this way.
