# Etappe 63 — The schema promised a feature nobody built

Third pass of the sweep that produced E61 and E62. Those looked for *constants* declared
and never consumed; this one looks at **columns**.

## What is dead, verified against the live database

| column / table | rows or non-NULL values |
|---|---|
| `config_snapshots` (whole table) | **0 rows** |
| `upgrade_history.values_snapshot` | 0 of 7 |
| `upgrade_history.helm_output` | 0 of 7 |
| `upgrade_history.pre_flight` | 0 of 7 |
| `ess_versions.chart_digest` | 0 of 175 |
| `ess_versions.changelog` | 0 of 175 |
| `ess_versions.breaking_changes` | 0 of 175 |
| `ess_versions.published_at` | 0 of 175 |

Eight columns and an entire table, none of which any Go code reads or writes. Checked
on the running database rather than inferred from the absence of a query, because
"nothing in the repository writes it" and "it is empty" are different claims and only
the second one licenses dropping it.

## They are not all the same thing

**Superseded** — the design changed and the columns were left behind:

- `config_snapshots` and the `values_snapshot` foreign key into it. Config history is
  git-backed (E4/E5); a second, unused snapshot table is the second source of truth
  §4.49 warns about, sitting there waiting for someone to wire it up.
- `ess_versions.changelog`, `breaking_changes`, `chart_digest`, `published_at`. Release
  notes are fetched from the published releases (E32) and version dates from the release
  index (E43). This is what P2-4 was really describing.

**Never built, and one of them is now worth building** — `upgrade_history.pre_flight`.

## The one that is worth filling

E55 added a capacity preflight: before an apply, the chart is rendered and every
workload measured against the node. Its findings go into the **live log stream**, and
nowhere else. The stream is a WebSocket; when it closes, the finding is gone.

So after this month's outage, "did the panel warn us before we applied that?" is not
answerable. The column designed for exactly that answer has existed since migration 003
and has never held a row.

Filling it turns the preflight from something you had to be watching into something the
upgrade history can be asked about afterwards — which is when the question actually gets
asked.

`helm_output` is not filled: `error_message` already carries what a failed upgrade
said, and a second column holding the whole log would be a large blob nobody reads.
Dropped with the rest.

## Scope

**Ships:** the preflight result stored on the upgrade row and shown in its history
entry; the superseded columns and the dead table removed.

**Does not ship: backfilling the seven existing rows.** They ran before the preflight
existed. An empty `pre_flight` on an old upgrade means "not checked", and inventing a
verdict for it would be exactly the fabrication §4.59 removed from the sparkline.

## Definition of done

- An apply records its capacity findings on the upgrade row
- An upgrade that was not checked is distinguishable from one that was checked and found
  nothing
- The dead table and superseded columns are gone, after verifying they are empty
- `make check` green, and the four S11 checks pass
