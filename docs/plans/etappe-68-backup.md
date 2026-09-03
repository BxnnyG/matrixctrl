# Etappe 68 — A backup that says what it is not

S14 listed backup/restore as part of Day-2 operations. E67 checked and found **nothing
at all** behind the word — the only matches in the code were a one-off config-migration
directory and a comment. For a tool whose job is running a homeserver, that is the one
gap where a failure costs data rather than time.

## What MatrixCtrl can actually back up, and what it cannot

Measured rather than assumed:

| | reachable | size |
|---|---|---|
| MatrixCtrl's own database | yes, its own sidecar | 14 MB |
| the config repository (all ESS values **with git history**) | yes, mounted at `/data/config-repo` | 968 KB, 71 files |
| Synapse's database | network-reachable, credentials readable | 10 GB volume |
| Synapse's media | **no** — a PVC this pod does not mount | 10 GB volume |

The first two are what MatrixCtrl owns and can capture correctly. The last two are the
homeserver's own data, and capturing them properly needs a Job with those volumes
mounted — a different mechanism, not a bigger loop.

**So this backup does not contain the homeserver.** That sentence is the most important
thing this etappe ships, and it goes in the archive's own manifest rather than in
documentation nobody reads at three in the morning. An operator who believes they have
a backup of their Matrix server and finds out otherwise during a restore is exactly the
failure this project keeps writing up: something that looks complete and is not (§4.45).

What it *does* contain is worth having on its own terms. The config repository is every
ESS value with its full history — the thing that would take longest to reconstruct by
hand — and the database holds the hooks, the upgrade history, the report dispositions
and the recorded node capacity.

## The schema is not in the backup, deliberately

A dump would carry both schema and data. This carries only data, per table, and restore
re-creates the schema by running the 17 migrations. The migrations already are the
schema's definition, tested from zero as recently as E67; a second copy inside every
archive would be a second source of truth that ages differently from the first (§4.49).

It also means an archive restores onto the *current* schema rather than the one it was
taken on, which is the behaviour anyone actually wants.

## Scope

**Ships:** an on-demand archive — manifest, one CSV per table, the config repository
including `.git` — streamed as a download.

**Does not ship: restore.** Deliberately its own etappe. Taking a backup is harmless;
writing one back destroys what is there. Shipping both together would mean shipping the
dangerous half less carefully tested than the safe one, and §4.39 has a rule about that.

**Does not ship: scheduling.** A backup nobody downloads is not a backup. Automating it
needs somewhere to put it — object storage, a mounted volume — which is a decision about
the operator's infrastructure, not something to guess at.

**Does not ship: Synapse's data.** See above; it needs a Job with the media and database
volumes mounted, and it is recorded rather than half-done.

## Definition of done

- The archive contains every table with rows, the config repository, and a manifest
- The manifest names what is **not** included, in the archive itself
- The archive restores conceptually onto a schema built by migrations, not one it carries
- Telemetry tables are labelled as regenerable so a restore decision is informed
- Downloading it is one request and does not hold the whole archive in memory
- `make check` green
