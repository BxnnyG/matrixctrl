# Etappe 57 — The panel on a phone

Requested by the operator: phone-sized views, and installable on the home screen.

## What was actually wrong

The app was never unusable on a phone — it was built with `minmax()` grids that reflow
on their own. Three things broke it:

- **A 240px navigation rail** permanently occupying two thirds of a 360px screen
- **A topbar** carrying a cluster label, a chart version and a `⌘K` hint, on a device
  with no keyboard, all fighting for the same row
- **`100vh`**, which on a phone reserves the space the browser chrome *used* to occupy
  and leaves a dead strip below the fold

And the tab title was `web` — Vite's default, never changed. On a home screen that is
the app's name.

## Decisions

**A hook, not a media query.** This codebase styles inline. A CSS rule cannot reach an
inline style without `!important` and a selector matching on the style string itself —
clever, fragile, and unreadable in six months. `useIsMobile()` is `matchMedia` with a
listener, and it reads as what it is.

**860px, not a device.** A narrow window on a laptop has exactly the same problem as a
phone, and nothing in the layout should care which one it is.

**A new `menu` icon.** `sliders` was the closest existing one and means *settings*. A
button that opens navigation must not look like one that opens preferences — the icon
set gained three lines rather than the button borrowing a wrong meaning.

## The icons

A manifest needs PNGs and the build host has no SVG rasteriser — no `rsvg-convert`, no
ImageMagick, no Pillow, and nothing in `node_modules`. Adding a dependency for three
files that change roughly never is the wrong trade, so `web/scripts/make-icons.py`
draws them: the brand mark's path flattened (including its one elliptical arc, properly
converted rather than cut to a straight line), filled with a supersampled nonzero
scanline, white on the brand square.

Committed *with* the generator, so the PNGs are reproducible rather than mystery
binaries — the §4.49 rule is that a generated artefact under version control is a second
source of truth, and the defence is that regenerating it is one documented command.

iOS needs its own tags: it ignores the manifest for both the icon and the status bar,
and without them Safari puts a screenshot of the page on the home screen and a light
status bar over a dark app.

## Scope

**Ships:** the drawer, a topbar that fits, `100dvh`, safe-area insets, the manifest,
the icons, and a title that is not `web`.

**Does not ship: an offline service worker.** "Installable" and "works offline" are
different features, and an admin panel that shows a cached cluster state from twenty
minutes ago is worse than one that says it cannot reach the server. If offline is
wanted it should cache the shell and refuse the data, deliberately.

**Does not ship: per-route padding.** Every route sets its own `padding: 28`. The shell
stops adding its own on mobile, which is most of the gain; making the routes themselves
responsive means touching ten files and is worth doing when someone has looked at them
on a real phone.

## Definition of done

- Below 860px the rail is a drawer, closing on navigation and on viewport growth
- The topbar fits 360px: menu button, title, a status dot with a short label
- Manifest, icons and iOS tags are served
- The icons are legible at 192px — checked by looking at them
- `make check` green
