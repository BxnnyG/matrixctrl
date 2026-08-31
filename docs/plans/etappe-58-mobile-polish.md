# Etappe 58 — The part of the phone view that was left cramped

E57 made the panel usable on a phone. The operator's verdict was "könnte man besser
machen aber ist ok", which is fair: it stopped at the shell.

## What was actually still wrong

Audited rather than guessed, by grepping for the things that break a 360px viewport:

| suspected | reality |
|---|---|
| dashboard's two-column grid (`minmax(300px, 1fr)`) | **already handled** — `.mc-dash-grid` collapses at 920px |
| per-route `padding: 28` | real: 28px each side of a 360px screen is 16% of the width |
| the 5-column component table | real: name, status, ready, restarts and a chevron across 360px |

The first line is the useful one. The obvious culprit was already fixed months ago, and
had this etappe started from assumption rather than measurement it would have "fixed"
it twice.

## A convention I should have found in E57

`index.css` already carries:

```css
@media (max-width: 920px) { .mc-dash-grid { grid-template-columns: 1fr !important; } }
```

A class plus a media query plus `!important`, overriding an inline style — precisely
the technique E57's plan dismissed as "clever, fragile, unreadable in six months". It
is neither clever nor fragile when the class is *put there for the purpose*; what E57
rejected was matching on the style string itself (`[style*="padding: 28px"]`), which is
a different thing and still wrong.

So this etappe follows the existing convention rather than adding a second one. The
split that emerges is worth stating: **CSS classes for layout, the hook for behaviour.**
The drawer needs to know whether it is mobile in order to *exist*; a padding does not.

## Scope

**Ships:** `.mc-page` on the route containers with responsive padding, and the
component rows folding onto two lines on a phone instead of squeezing five columns.

**Does not ship: a visual check.** The authenticated screens still cannot be rendered
here — same token wall as P2-32 and the `verify-ui` run. This etappe reduces guessing
by auditing the source; it does not replace looking at it, and the operator's eyes stay
the verification.

## Definition of done

- No route sets its own page padding
- Below 860px the padding is 14px, above it 28px
- A component row on a phone shows its name on one line and its three numbers below
- Nothing changes above 860px — asserted by leaving the desktop rules untouched
- `make check` green
