# Etappe 32 — What is actually in this version

**Date:** 2026-08-05 · **System:** S4 · **Requested by:** the operator, after the
26.8.0 upgrade failed

## The two asks

> *"bei den update tab maby mit changelog was nice oder so gepulled"*
> *"wenn ich schon auf update klicke oder update auf xxx das er dann das bei
> /helm/upgrade übernimmt"*

Both are small. The first turned out to be less cosmetic than it looks.

## Why the changelog matters more than it sounds

ESS publishes release notes per version. 26.8.0's say, in the body:

```
- Upgrade Element Web to v1.12.25.
- Upgrade Synapse to v1.158.0.
```

Those are exactly the two upgrades the operator's pinned image tags were silently
preventing (E31). The changelog was one HTTP request away and would have said, in
the operator's own words, what the upgrade was supposed to bring — next to a warning
saying they would not get it.

So this is not decoration. It is the other half of the pin warning: one says *what
you would get*, the other says *what you will not*.

## Outbound traffic

`internal/helm.ListVersions` already fetches from `ghcr.io`, so talking to the
internet is established behaviour on this page rather than something new. Unlike
E26's reachability check, this discloses **nothing about the deployment** — it is a
public GET for a public version's notes, carrying no address, hostname or identity.
It therefore does not need the opt-in E26 does.

What it does need is to fail quietly: an air-gapped install must see "notes not
available", not an error.

## Approach

- `GET /api/v1/helm/versions/{version}/notes` fetches
  `api.github.com/repos/element-hq/ess-helm/releases/tags/<version>` and returns the
  markdown body.
- **Cached in memory, per version.** Released notes do not change, and GitHub's
  unauthenticated rate limit is 60 requests an hour — a page that refetches on every
  render would exhaust it and then show nothing at all. Bounded so the cache cannot
  grow without limit.
- Rendered with a small markdown subset — headings, lists, links, code — rather than
  a dependency. The bodies are release notes with a fixed shape, and a markdown
  library for four constructs is a lot of bundle for a little text.
- The version travels from the list to the upgrade page as a search parameter, so
  "Upgrade auf 26.8.0" arrives with 26.8.0 selected.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Reads a chart version, not a release.
2. **Helm release in a bad state.** Untouched.
3. **Not just Deployments.** N/A.
4. **Cluster slow or gone.** No cluster involved.
5. **No outbound internet.** The point of the graceful path: "notes not available"
   with the reason, never a blank page or an error box.
6. **Both auth modes.** Behind the same middleware as the rest of `/helm`.
7. **Config edge shapes.** A version that has no release, a version string with path
   characters in it, an enormous body, a rate-limited response — each covered.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- Selecting a version on `/helm/upgrade` shows its notes
- "Upgrade auf X" arrives with X selected
- A version with no published notes says so
- No outbound call is made twice for the same version
- A version string cannot escape the URL path
- S11 green **after** the deploy

## Status (2026-08-05)

**Built and committed, not deployed.** Go tests and the frontend typecheck pass; the
build-and-deploy step was stopped by the operator, so the running image is still
`0.1.32` while `Chart.yaml` reads `0.1.33`.

Nothing is tagged: the project's rule is deploy → verify → ship, and tagging an
unverified version would put a release on GHCR that nobody has watched start. The
tag waits for the deploy.

### Verified without a deploy

- Version validation refuses `../`, spaces, query strings and over-long strings
- A second request for the same version is served from cache
- The cache clears rather than growing past its bound
- An unreachable API yields `available: false` with a reason, not an error
- `tsc --noEmit` clean, `go test ./internal/...` clean

### Still unverified

That the panel renders the notes, and that "Upgrade auf X" really arrives with X
selected. Both are one deploy away and neither is provable from here.
