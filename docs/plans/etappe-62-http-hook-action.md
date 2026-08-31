# Etappe 62 — The third action type, offered and unimplemented

Found by sweeping for the shape E61 had just produced: a constant that is declared,
surfaced in the UI, and consumed by nothing that does the work.

```
$ grep -rn 'ActionHTTPRequest' internal/ | grep -v _test
  internal/hooks/types.go:22:   ActionHTTPRequest ActionType = "http_request"
  internal/hooks/runner.go:34:  case ActionHTTPRequest:
  internal/hooks/runner.go:85:  return fmt.Errorf("http_request actions not yet implemented (Phase 1)")
```

The hook editor offers **"HTTP-Request"** in its action dropdown, with a complete form —
method, URL, body. Saving works. The hook then fails the first time it runs, which is
during an upgrade, which is the worst moment to discover it.

It is a shade better than E61's dead trigger: this one fails loudly rather than doing
nothing quietly, and since E61 a failing hook makes the upgrade end in `hooks-failed`.
Still: a form that saves a configuration the engine cannot execute is a promise the
software does not keep.

## An error of mine, on the way

E60's guide listed `http_request` as one of three working action types. It was written
from `types.go` without checking the runner — in the same document whose stated rule
was "every technical claim checked against the source while writing", one paragraph
after the rule caught the rollback bug. Corrected in the same commit as this fix.

The lesson is not "check more". It is that **a list of declared constants reads exactly
like a list of features**, which is why both defects in this family were found by
looking for consumers rather than by reading declarations.

## What ships

The action implemented, deliberately small:

- one request, with the method, URL and body the form already collects
- a bounded timeout, because a hook that hangs blocks an upgrade's hook phase
- **2xx is success, everything else fails the hook** — a notification that silently
  404s is not a notification
- the status code recorded in the run log, so a failure says what came back

Enough for the use it was put in the UI for: telling something outside the cluster that
an upgrade happened and the patches went back on.

## Scope

**Does not ship: templating the body.** Interpolating the release version or the hook
result into the payload is the obvious next request and a language of its own. A fixed
body is honest about what it does.

**Does not ship: retries.** A hook that failed is reported as failed and can be run
again by hand. Silent retries would hide a broken endpoint.

**Does not ship: credentials.** No header field, so no place to put a secret that would
then live in the hooks table in plain text. A notification endpoint that needs auth can
carry a token in its URL path, which is the caller's decision to make, not this
feature's to encourage.

## Definition of done

- A hook with an `http_request` action performs the request and succeeds on 2xx
- A non-2xx response fails the hook, with the status code in the run log
- An unreachable host fails rather than hanging
- The other two action types are untouched
- `docs/GUIDE.md` no longer describes it as something it is not
- `make check` green
