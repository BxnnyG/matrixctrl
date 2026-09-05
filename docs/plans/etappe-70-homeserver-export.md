# Etappe 70 — The volumes, and a nav entry that pointed at nothing

Two things the operator found in one message.

## 1. The greyed-out entry

The sidebar has had a **Backup** item under "Betrieb · Day-2" since the design system
shipped, greyed out because `NavRow` disables anything without a route. E68 and E69 then
put backup and restore on `/system` — so the feature existed and the label that promised
it stayed dark.

> ok aber auf der website ist backup noch ausgegraut ...

A disabled item next to a working feature is worse than no item at all: it actively tells
the reader the thing is not there. Backup and restore move to `/backup`, which is where
the navigation always said they would be, and `/system` goes back to being about the node.

## 2. Why the volumes matter

> aber wiso sollte man volumes sichern, ahh wegen chats und so, ja dann braucht man eig ..

Exactly. Measured on the live server: **4 accounts, 9 rooms, 19 057 events, 67 media
files** — a 304 MB Synapse database and 40 MB of media. E68's archive rebuilds the
*deployment*; this is the part that makes it the same server rather than a fresh one
wearing the same hostnames.

## What is reachable, and what is not

| | reachable from MatrixCtrl | why |
|---|---|---|
| Synapse's database (304 MB) | **yes** | network-reachable, and `POSTGRES_SYNAPSE_PASSWORD` is in a secret this pod may read |
| Synapse's media (40 MB) | **no** | a PVC mounted only by the Synapse pod |

So the database ships and the media does not. Stated in the export itself, exactly as
E68/E69 state their limits, rather than left for a restore to discover.

## Consistency is the part worth getting right

A homeserver database read table by table while it is running produces tables from
different moments — a backup where a room exists and its creation event does not. So the
whole export runs inside **one `REPEATABLE READ` transaction**, which is what `pg_dump`
does and for the same reason. Without it the archive would look complete and be subtly
torn, which is this project's recurring failure in a new place.

## Scope

**Ships:** an on-demand export of Synapse's database as a consistent snapshot, offered
on the Backup page beside the configuration archive, with its own manifest.

**Does not ship: restoring it.** Writing a homeserver database back is not a button. It
needs Synapse stopped, and doing it while the server runs corrupts what is there. The
export is a file the operator can restore with `psql` deliberately; a documented
procedure is the honest form for an operation this destructive (§4.39).

**Does not ship: media.** 40 MB behind a volume this pod cannot mount. It needs a Job
with the PVC attached, which is a different mechanism — recorded, not half-built.

## Definition of done

- The Backup page is reachable from the navigation entry that names it
- The export is one consistent snapshot, not a sequence of independent reads
- Its manifest states the row counts, and that media is not included
- Credentials are read from the cluster secret and never logged
- `make check` green
