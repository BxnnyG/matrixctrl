# Etappe 69 — Restore, and what "the same homeserver" means

E68 shipped a backup whose card says *"not included: the homeserver"*. The operator
pushed back, correctly:

> aber wäre es dann nicht schlau beim setup, backup einspielen und er baut es genau so
> auf — also gleiche versionen gleiche config

They are right, and the framing was mine to fix. The archive holds `hostnames.yaml`,
`rtc.yaml`, `tls.yaml` and every other slice **with git history**. That is not "not the
homeserver" — it is everything needed to stand the same homeserver up again, minus the
accounts, rooms, messages and media inside it.

Two very different sentences, and E68 used the pessimistic one for both:

- **Comes back:** the deployment. Same hostnames, same server name, same TLS issuer,
  same RTC settings, the hooks that keep the SFU patched, the upgrade history.
- **Does not come back:** what users created. Accounts, rooms, messages, uploaded files.

## The gap the operator's question exposed

"Gleiche Versionen" is not currently possible from the archive. The manifest records
MatrixCtrl's own version and says nothing about ESS — the live release is
`matrix-stack-26.8.0`, revision 30, and none of that is captured. A restore could
therefore reproduce the configuration onto whatever chart version happened to be newest,
which is not the same homeserver.

So the manifest gains the ESS release: name, namespace, chart version, revision.

## Restoring across a schema change

The archive carries data and no schema, deliberately (§4.66) — migrations rebuild it.
That makes the interesting case explicit rather than accidental: **a backup taken at
schema N restored onto schema N+1.**

Each CSV carries its header, so columns are matched by name: ones that still exist are
restored, ones since dropped are skipped, ones since added take their defaults. An
archive from before migration 017 therefore restores cleanly onto today's schema, which
is the entire reason for not carrying a schema in the first place.

`schema_migrations` is never restored. It is the live database's bookkeeping about
itself, and overwriting it with a backup's copy would tell the application that
migrations it has run are pending, or the reverse.

## Safety

Restore destroys what is there, which is why it is a separate etappe from taking one.

- one transaction — a failure part-way leaves the database as it was
- the format version is checked, and an unknown one is refused rather than guessed at
- the archive's ESS release is *shown* before anything is written, so an operator can
  see they are about to restore a 26.8.0 configuration onto a 26.9 cluster

## Scope

**Ships:** upload and restore of an archive — config repository and database — with the
ESS release recorded and displayed.

**Does not ship: deploying ESS as part of the restore.** The setup flow already installs
a release from the config repository. Restoring the values and then *also* triggering an
install would make one button do two irreversible things, and the second one already has
a screen of its own.

**Does not ship: restoring Synapse's data.** Unchanged from E68 and stated in the same
place — it lives on volumes this pod does not mount.

## Definition of done

- The manifest carries the ESS release name, chart version and revision
- An archive restores the config repository and the database in one transaction
- A CSV column that no longer exists is skipped rather than failing the restore
- `schema_migrations` is never written
- An unknown format version is refused
- The UI shows what the archive holds *before* restoring, and says what will not come back
- `make check` green
