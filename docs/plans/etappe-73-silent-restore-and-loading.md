# Etappe 73 — The restore that would have restored nothing

The operator asked whether everything works now:

> sonst sollte jetzt alles gehen also backup import mit allem?

Checking instead of answering found that it did not, and in the worst possible way.

## The bug I shipped an hour earlier

E72 moved the archive's contents under `matrixctrl/` and `homeserver/`. `Read()` still
looked at the top level. The root manifest of a full archive carries a matching
`format_version`, so **nothing was refused**: no tables found, no config files found, and
the restore reported

> 0 Konfigurationsdateien und 0 Tabellen wiederhergestellt

as success. A silent no-op is the worst failure a backup feature can have — it is only
discovered by the person who needed it to work, at the moment they needed it.

This is §4.40's family in the place it can do the most damage, and I built it by changing
a producer without re-checking its consumer. The test was written before the fix, and it
failed with exactly that symptom rather than an error.

Both layouts are now read. Flat archives already exist, and a reader that quietly ignores
half of a file it does not recognise is what caused this.

`homeserver/` is deliberately skipped by the restore: Synapse's tables belong to another
database and are put back with `psql`, not written into MatrixCtrl's.

## Loading states

> zudem maby skeleten loading oder progressbar

Two different things, and only one of them can be honest.

**Skeletons** where a table is about to appear — the room list, the report queues, the
member list. A spinner says "something is happening"; a skeleton says "a table is coming,
this wide, with this many rows", which stops the layout jumping when the data lands. Full
page loads keep the spinner, because there is no shape to promise yet.

**No progress bar for the download.** The archive is streamed, so there is no
`Content-Length` to divide by, and a bar with an invented denominator is precisely what
§4.41 exists to forbid. What is actually known is how many bytes have arrived, so the
button counts them: `142.3 MB…`. On a 300 MB homeserver dump that is the difference
between a working download and a dead button.

The pulse respects `prefers-reduced-motion`, and it is a slow opacity change rather than
a shimmer sweep — on a page with a dozen placeholders the sweep is what the eye follows,
and a placeholder that draws attention is doing the opposite of its job.

## Definition of done

- A full archive restores its configuration and database parts
- A flat archive from before E72 still restores
- Synapse's tables never enter MatrixCtrl's restore set
- The download button reports bytes received, not a fabricated percentage
- Lists show their shape while loading
- `make check` green
