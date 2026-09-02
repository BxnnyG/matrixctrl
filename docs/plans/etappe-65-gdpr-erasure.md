# Etappe 65 — Erasure, and what it does not erase

P2-25. An operator with a real erasure request currently has to leave the panel: E28
sends `skip_erase: true` on every deactivation, so MatrixCtrl cannot erase at all. That
was a deliberate choice — a one-click irreversible erasure is the wrong default — but
"not the default" and "not available" are different things, and for a homeserver in the
EU the second one is a compliance problem.

## What erasure actually does, from Synapse's source

Read out of the running container, because the difference between what this is called
and what it does is the entire substance of the etappe.

`deactivate_account.py`, when `erase_data` is true:

- deletes displayname, avatar URL and custom profile fields — with Synapse's own caveat
  that they **"may persist as historical state events in rooms"**
- calls `mark_user_erased(user_id)`

and independently of erasure it deactivates, purges account data, deletes backup keys
and rejects pending invites.

The erased flag is then consulted in `visibility.py`:

```python
if sender_erased and not membership_result.joined:
    event = prune_event(event)
```

**Their messages are pruned only for viewers who were not joined at the time.** Everyone
who was in the room when a message was sent still sees its full content, forever.

## Why that changes the feature rather than a footnote

A button labelled "GDPR-Löschung" that leaves message content readable by every witness
is a false statement made by software, and this project has spent sixty etappes
refusing exactly that shape — a 200 that means nothing changed, a green check that
checked nothing, a sparkline that invented its past.

So the action ships **with the limits in the confirmation, not in a documentation page
nobody opens**. An operator answering a legal request needs to know that erasure does
not reach message bodies, because they may have to do something else as well — redact
the events, or delete the media, which are different operations.

## Scope

**Ships:** a separate `erase` action — not a checkbox on deactivate — with a confirmation
that states what is removed, what remains, and that none of it can be undone. Its own
audit entry.

**Does not ship: bulk erasure.** One account at a time. A loop over a list is exactly how
an irreversible action gets run against the wrong selection.

**Does not ship: media deletion alongside it.** An erased user's uploads are still on
disk, and deleting media is a separate irreversible operation which E47 deliberately
left out. The confirmation says so rather than the button quietly doing more than its
name.

**Does not ship: redacting their events.** Synapse offers no admin API to redact one
user's history wholesale, so promising it here would be a second false claim.

## Definition of done

- Erasure is reachable for a single account and never as a side effect of deactivation
- The confirmation names: profile cleared, account deactivated, account data purged
- The confirmation names what remains: message content for anyone who was in the room,
  the display name inside historical state events, and uploaded media
- It says the action cannot be undone, next to a Reactivate that cannot bring data back
- The audit log distinguishes an erasure from a deactivation
- `make check` green
