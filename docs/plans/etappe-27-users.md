# Etappe 27 — Users, read-only

**Date:** 2026-08-04 · **System:** S13 · **Starts:** Phase 2

## Scope

List, search and inspect users. **No writes.** Locking, deactivating, granting
admin and resetting passwords are each destructive in a different way and need
confirmation UX plus audit entries; bundling them into the etappe that introduces
the whole subsystem would mean shipping the dangerous half untested. The list is a
complete deliverable on its own — it is what an operator opens first, and today the
answer is "go use element-admin".

## Inventory first ([CLAUDE.md](../../CLAUDE.md) rule 2)

MAS admin access **already exists** in `internal/auth/oidc.go`: a
`client_credentials` grant with scope `urn:mas:admin`, used to check
`can_request_admin` at login. It mints a fresh token per call and knows exactly one
endpoint.

So this etappe does not add a second way to talk to MAS. It extracts the existing
one into `internal/mas` and has the login path use it — rule 3. Two independent
MAS clients would drift the day someone changes the scope or the issuer handling.

## What the API actually is (read from the live spec, not the docs)

`GET /api/admin/v1/users` — verified against the running MAS:

- **Cursor pagination**, not offset: `page[after]`, `page[before]`, `page[first]`,
  `page[last]`. There are no page numbers, so a UI with page numbers would be a lie
  told in the client. Next/previous and a total from `count`.
- `filter[search]` matches the username, `filter[status]`, `filter[admin]`.
- A user is: `username`, `created_at`, `locked_at`, `deactivated_at`, `admin`,
  `legacy_guest`.

## The two honesty problems

**1. MAS is not the only place users exist.** Synapse has its own user table, and
on a stack migrated to MAS the two can disagree — a pre-migration account, a user
deactivated on one side only. This etappe reports **MAS**, which is authoritative
for accounts under MSC3861, and the page says so rather than implying it lists
every user Synapse has ever seen. Reconciling the two is real work and belongs in
its own etappe, not in a footnote here.

**2. Locked and deactivated are not the same thing**, and both arrive as
timestamps. A UI that renders both as "disabled" throws away the distinction the
operator needs to act: locked is reversible and usually temporary, deactivated is
the account being gone. They get separate states and separate wording.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** No MAS configured → the page says the feature needs
   MAS, rather than showing an empty list that reads as "no users".
2. **Helm release in a bad state.** Untouched — this talks to MAS, not Helm.
3. **Not just Deployments.** N/A.
4. **Cluster slow or gone.** MAS unreachable → an error that says so; never an
   empty list.
5. **No outbound internet.** MAS is in-cluster; no third party involved.
6. **Both auth modes.** Bootstrap mode has no OIDC client credentials and therefore
   no admin token — the page must say that plainly instead of failing obscurely.
7. **Config edge shapes.** Missing fields, null timestamps, an empty page, a
   malformed cursor — each covered by a test.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- The live user list renders, searchable, with working next/previous
- Locked and deactivated are visibly different states
- Bootstrap mode explains itself instead of erroring
- Token minting is shared with the login path, not duplicated
- Parsing and paging logic tested with no MAS
- S11 green **after** the deploy

## Outcome (2026-08-04)

Shipped in `0.1.28`. S11 all four green after the deploy (revision 30); `/api/v1/users`
returns `401` without a token and the page renders.

Verified against the **live** MAS through the shared client — the link the unit
tests cannot reach, because a wrong envelope assumption renders an empty list that
looks exactly like a deployment with no accounts:

```
total=4 returned=4
  … active       admin=true
  … deactivated  admin=true
  … active       admin=false
  … active       admin=false
search -> narrows correctly (substring match)
```

One account is deactivated and reads as such rather than as merely disabled, which
was the point of keeping the two timestamps apart.

### Read from the spec, not the docs

The API shape came from the running MAS's own `/api/spec.json`. That is where the
design constraint appeared: **cursor pagination**, so there is no page number to
render, and a UI with page numbers would have been a lie told in the client. It also
settled the envelope (`meta.count`, `data[].attributes`, `links.next/prev`) and the
exact filter names, none of which were guessed.

### Found while wiring it

The connect-OIDC flow rebuilds the OIDC service at runtime, so a MAS client captured
at startup would stay `nil` for the rest of the process on a greenfield install —
someone configures OIDC, and users never appear until a restart nobody knows to do.
The handler holds a *function* returning the current client, read under the same lock
the reload writes.

### What it deliberately does not do

Writes. Lock, deactivate, set-admin and set-password each need confirmation and an
audit entry, and bundling them into the etappe that introduces the whole subsystem
would mean shipping the dangerous half with the least attention on it.
