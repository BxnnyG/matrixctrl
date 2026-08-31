# Etappe 61 — The trigger that was offered and never fired

Found while writing the user guide (E60), by checking a sentence about triggers against
the code instead of writing what seemed obvious.

## What is wrong

`hooks.TriggerPostRollback` is declared, the hook editor offers it in its dropdown as
**"Nach Rollback"**, the list view has a label for it — and nothing anywhere fires it:

```
$ grep -rn 'RunTrigger(' internal/ | grep -v _test
  RunTrigger(ctx, hooks.TriggerPostUpgrade, "deploy:"+h.essRelease, userID)
  RunTrigger(ctx, hooks.TriggerPostUpgrade, upgradeUUID.String(), userID)
```

So an operator can create a hook, set it to run after a rollback, save it, and see it
listed as enabled. It will never run, and nothing says so.

## Why this is worse than a dead dropdown entry

A Helm rollback recreates objects from the *old* revision's manifests. That drops
manual patches for exactly the same reason an upgrade does — and dropping them is the
failure this whole project was built to prevent:

> That works. It also lasts exactly until the next `helm upgrade` … Calling breaks.
> Nothing turns red.

Rolling ESS back therefore removes the SFU's `hostNetwork` and the
`externalTrafficPolicy: Local` patches, breaks Element Call's media path, and leaves a
green dashboard behind. The one operation an operator reaches for *when something is
already wrong* is the one that silently undoes the fixes.

The built-in hooks are `post-upgrade`, so they would not have covered rollback even if
the trigger fired. Both halves need doing: fire the trigger, and let the built-ins
answer to it.

## Scope

**Ships:** hooks run after a successful rollback, the built-in RTC patches respond to
rollback as well as upgrade, and the rollback reports hook failure the way an upgrade
does rather than claiming success.

**Does not ship: firing hooks after a *failed* rollback.** If Helm could not complete
the rollback, the cluster is in a state nobody described, and re-applying patches on top
of it is guessing. The upgrade path makes the same distinction already.

**Does not ship: a second trigger for config-apply.** Applying config is a `helm
upgrade` and already fires `post-upgrade`; adding a separate trigger would be two names
for one event.

## Definition of done

- A rollback runs `post-rollback` hooks, and a hook set to that trigger actually runs
- The built-in SFU patches survive a rollback, not only an upgrade
- A rollback whose hooks fail says so, and does not report plain success
- A failed rollback runs no hooks
- The four S11 checks pass, one of which is exactly this promise
- `make check` green
