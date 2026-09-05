# Etappe 72 — One backup, not three apologies

The operator, looking at the Backup page:

> und wiso sind da die gelben texte wiso gibbet nicht ein full backup mit allem!?

Both halves are fair.

## What the page had become

Three cards, each with a yellow block explaining what it could *not* do. Every sentence
in them was true and each was written for a good reason — §4.66's rule that an archive
must say what it is not. Assembled on one screen the effect is different from the intent:
the product nags. An operator wanting a backup has to read three architecture lessons and
then work out which two files to download and keep together.

**Honesty about a limit is not a licence to make the limit the operator's problem.** The
split existed because *I* built it in two etappes, and the page showed my build order
rather than their task.

## What ships

One archive, one button. `config` + MatrixCtrl's database + Synapse's database in a
single file with a single manifest, so "the backup" is one thing you can hold.

The separate downloads stay, demoted to a line beneath, because there is a real use for
them — a configuration archive is 1 MB and a homeserver dump is 300, and someone
migrating configuration alone should not move 300 MB.

And **one** statement of the remaining gap instead of three, in normal text rather than
warning colour: uploaded files are not in it.

## Why media is still out, and what it would take

Checked while writing this, rather than assumed from E68's "the volume is not mounted":

| route | result |
|---|---|
| `/_matrix/media/v3/download/...` (unauthenticated) | **404** — removed in current Synapse |
| `/_matrix/client/v1/media/download/...` | **401** — needs a Matrix token |

So there *is* a path: MatrixCtrl holds the operator's Matrix token for rooms and
moderation, and the media IDs are in the database it already exports. 67 files, 40 MB.

It is not built here because it cannot be tested here — exercising it needs a live token,
the same wall as P2-32 — and a backup path that has never successfully run is worse than
a documented gap. Recorded as the next step with the route that works.

## Scope

**Ships:** the combined archive, the button, and one calm sentence about media.

**Does not ship: removing the honest limits.** They move from three yellow blocks to one
ordinary line. Deleting them would be the opposite failure, and a restore that surprises
someone about missing messages is worse than a page that reads slightly cautiously.

## Definition of done

- One button produces one archive containing configuration, MatrixCtrl's database and
  Synapse's database
- Its manifest lists all three, and names media as the one thing absent
- The two single-purpose downloads remain reachable but subordinate
- The page carries one statement of the gap, not three
- `make check` green
