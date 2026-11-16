#!/usr/bin/env node
// Post-deploy UI verification (docs/PROZESS.md §4).
//
// An HTTP 200 only proves the server answered — it does not prove the page
// rendered. This walks every functional route in a real browser, fails on
// console errors, failed requests, or an empty root, and writes a screenshot
// per route so a change can be eyeballed after the fact.
//
// Deliberately a smoke check, not visual regression: no golden images, because
// those fail on every intentional design change and get disabled within two
// etappes.
//
//   node scripts/verify-ui.mjs --base https://matrixctrl.example.com
//   node scripts/verify-ui.mjs --base http://localhost:8080 --out /tmp/shots
//
// Auth: set MATRIXCTRL_TOKEN to a valid JWT to reach authenticated routes. The
// token is injected into localStorage, never put in a URL or logged.
//
// Redaction (--redact from=to, repeatable): rewrites visible text before the
// screenshot is taken. The only instance with real data is production, and its
// node name must never reach a public repository (docs/DESIGN.md §4.14) — so
// publishable screenshots are a command, not a manual cleanup someone forgets:
//
//   node scripts/verify-ui.mjs --base … --redact my-node-01=matrix-node-01
//
// The count of replacements per route is reported, because a redaction that
// silently matched nothing is worse than none at all.
//
// --redact-ips replaces every IPv4 literal with an RFC 5737 documentation
// address. Needed for pages that display an address resolved inside the cluster,
// where the value cannot be known in advance to be passed as --redact.
//
// Independently of both flags, the rendered text is scanned for sensitive
// *categories* before the screenshot is written (see below). A route that would
// leak fails and produces no image.

import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { checkRoute } from "./lib/ui-check.mjs";
import { EXCLUDE, nameOf, routerPaths } from "./lib/routes.mjs";

const args = process.argv.slice(2);
const argOf = (name, fallback) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback;
};

const allOf = (name) =>
  args.flatMap((a, i) => (a === name && args[i + 1] ? [args[i + 1]] : []));

const BASE = (argOf("--base", process.env.MATRIXCTRL_BASE) || "").replace(/\/$/, "");
const OUT = argOf("--out", "/tmp/matrixctrl-verify");
const TOKEN = process.env.MATRIXCTRL_TOKEN || "";

// Detection works on categories; removal used to work only on strings you could
// name in advance. That gap is why /rtc could not be photographed: the page shows
// whatever IP the *pod* resolved, which is not necessarily what this host
// resolves, so no --redact value could be known beforehand. --redact-ips closes
// it by replacing every IPv4 literal, value unknown and unneeded.
const REDACT_IPS = args.includes("--redact-ips");

// Accepts a run in which some routes were never rendered. Without it, a skip is a
// failure — see the exit logic at the bottom of this file for why.
const ALLOW_SKIP = args.includes("--allow-skip");

// A room id to check the room *detail* screen with. Opt-in rather than a fixed entry
// in ROUTES: there is no id that is valid on every instance, and a route that always
// skips would — now that skips fail — be a check that always fails, which is a check
// that gets deleted.
const ROOM_ID = argOf("--room-id", process.env.MATRIXCTRL_ROOM_ID || "");

const REDACTIONS = allOf("--redact").map((spec) => {
  const i = spec.indexOf("=");
  if (i <= 0) {
    console.error(`--redact expects from=to, got: ${spec}`);
    process.exit(2);
  }
  return { from: spec.slice(0, i), to: spec.slice(i + 1) };
});

if (!BASE) {
  console.error("usage: verify-ui.mjs --base <url>   (or set MATRIXCTRL_BASE)");
  process.exit(2);
}

// Routes worth proving, read from the router's generated table (lib/routes.mjs). The
// hand-kept list that stood here had drifted twice: four Phase 2 screens were missing until
// etappe 49 (§4.48), eight more until etappe 121 (§4.130). `auth: false` must render without
// a token. A route with a parameter needs a real id, which only `make e2e` resolves (it
// logs in and asks the API) — except the room detail screen, when --room-id names one.
const PARAM_ROUTES = [];
const ROUTES = [];
for (const p of await routerPaths()) {
  if (EXCLUDE[p]) continue;
  if (p.includes("$")) { PARAM_ROUTES.push(p); continue; }
  ROUTES.push({ path: p, name: p === "/auth/login" ? "login" : nameOf(p), auth: p !== "/auth/login" });
}
if (ROOM_ID) ROUTES.push({ path: `/rooms/${encodeURIComponent(ROOM_ID)}`, name: "room-detail", auth: true });
console.log(`routes from the router: ${ROUTES.length}; with a parameter, opened only by make e2e: ${PARAM_ROUTES.join(", ") || "none"}`);

// The page check itself (console noise, sensitive scan, screenshot) lives in
// lib/ui-check.mjs and is shared with e2e.mjs.

const results = [];

const browser = await chromium.launch();
const ctx = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  colorScheme: "dark", // the app is dark-only
  ignoreHTTPSErrors: true,
});

if (TOKEN) {
  await ctx.addInitScript((t) => {
    try { window.localStorage.setItem("matrixctrl_token", t); } catch { /* ignore */ }
  }, TOKEN);
}

await mkdir(OUT, { recursive: true });

for (const route of ROUTES) {
  if (route.auth && !TOKEN) {
    results.push({ ...route, status: "skipped", reason: "no MATRIXCTRL_TOKEN" });
    continue;
  }

  results.push(await checkRoute(ctx, route, { base: BASE, out: OUT, redactions: REDACTIONS, redactIps: REDACT_IPS }));
}

await browser.close();

// Report
//
// The redactions apply to what is printed too. The first version of this header
// spelled out the production hostname on every run — the tool that exists to keep
// that string out of published artefacts was putting it in its own output, which
// is where terminal logs and pasted results come from.
const safe = (s) => REDACTIONS.reduce((acc, { from, to }) => acc.split(from).join(to), String(s));

let failed = 0;
console.log(`\nUI verification — ${safe(BASE)}\n`);
for (const r of results) {
  if (r.status === "pass") {
    const red = r.redacted ? `, ${r.redacted} redacted` : "";
    // The API-call count is printed on purpose. A data page that made zero calls
    // rendered *something*, and the screenshot will look plausible — that number
    // is the cheapest way to notice it was the wrong something.
    console.log(`  PASS  ${r.path.padEnd(18)} ${r.name}.png  (${r.apiCalls} API call(s)${red})`);
  } else if (r.status === "skipped") {
    console.log(`  SKIP  ${r.path.padEnd(18)} ${r.reason}`);
  } else {
    failed++;
    console.log(`  FAIL  ${r.path.padEnd(18)} HTTP ${r.httpStatus}  (${r.apiCalls} API call(s))`);
    // Problem strings carry URLs ("request failed: …"), so they go through the
    // same redaction as everything else that leaves this process.
    for (const p of r.problems) console.log(`          ${safe(p)}`);
  }
}

const redactedResults = results.map((r) => ({
  ...r,
  problems: (r.problems ?? []).map(safe),
}));
await writeFile(path.join(OUT, "results.json"), JSON.stringify(redactedResults, null, 2));
console.log(`\nScreenshots: ${OUT}`);


if (REDACTIONS.length) {
  const total = results.reduce((n, r) => n + (r.redacted ?? 0), 0);
  console.log(`Redaction: ${REDACTIONS.length} rule(s), ${total} text node(s) rewritten.`);
  if (!total) {
    console.log("  WARNING: no rule matched anything. Verify the strings before publishing.");
  }
}
const skipped = results.filter((r) => r.status === "skipped").length;
if (skipped) console.log(`${skipped} route(s) skipped — set MATRIXCTRL_TOKEN to include them.`);

// Worded to be true in a partial run. "All routes rendered cleanly" sitting above a
// list of thirteen skips is how the original defect read to anyone skimming.
const verifiedCount = results.length - skipped;
console.log(failed
  ? `\n${failed} route(s) failed.`
  : `\n${verifiedCount} of ${results.length} route(s) rendered cleanly.`);

// A skipped route is a route that was not verified, and until 2026-08-17 that fact
// reached the exit code not at all: `process.exit(failed ? 1 : 0)`. Run without a
// token, ten of eleven routes skipped and this process exited 0 — a verification tool
// reporting success for a run that verified one page (etappe 49, DESIGN §4.48).
//
// The output above was always honest. Only the status was not, and the status is what
// a Makefile, a CI job or a shell `&&` actually reads.
if (skipped && !ALLOW_SKIP) {
  console.log(
    `\nNOT VERIFIED: ${skipped} of ${results.length} route(s) never rendered.\n` +
    "  Set MATRIXCTRL_TOKEN to check them, or pass --allow-skip to accept a\n" +
    "  partial run. This is a failure by default because a check that skips\n" +
    "  almost everything and exits 0 is worse than no check at all.");
  process.exit(1);
}
if (skipped) {
  console.log(`\nPartial run accepted (--allow-skip): ${results.length - skipped} of ${results.length} route(s) verified.`);
}
process.exit(failed ? 1 : 0);
