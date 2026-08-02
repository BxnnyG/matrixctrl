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

import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { readFileSync } from "node:fs";
import path from "node:path";

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

// --- Sensitive-content scan -------------------------------------------------
//
// `--redact` only removes the strings someone remembered to pass. On the run
// that shipped 0.1.20 the rules covered the node name and the admin hostname,
// and /rtc rendered the *RTC* hostname next to its resolved public IP — neither
// was a rule, so nothing was replaced and the route was reported PASS. The tool
// whose job is producing publishable screenshots produced one containing the
// operator's public IP address and called it clean.
//
// `scripts/check-sensitive.sh` skips binaries with a comment saying screenshots
// "are covered by verify-ui.mjs --redact instead". They were not. So the same
// pattern source now guards the pixels too, and a route fails on a *category* of
// secret rather than on a remembered string.
const SENSITIVE = [];

// Always on, needs no secret, works in a fork: an IP literal in a screenshot is
// never intentional. Octet-range checked so version numbers cannot match.
const OCTET = "(25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)";
SENSITIVE.push({
  name: "IPv4 address",
  re: new RegExp(`\\b${OCTET}\\.${OCTET}\\.${OCTET}\\.${OCTET}\\b`),
});

// Same contract as check-sensitive.sh: env wins, then an untracked file found by
// walking up from the working directory. No source is not an error — a fork has
// no access to the secret, and failing its build over a check it cannot satisfy
// only gets the check deleted.
{
  let raw = process.env.SENSITIVE_PATTERNS || "";
  if (!raw) {
    const name = process.env.SENSITIVE_PATTERNS_FILE || ".sensitive-patterns";
    for (let dir = process.cwd(); ; dir = path.dirname(dir)) {
      try {
        raw = readFileSync(path.join(dir, name), "utf8");
        break;
      } catch {
        if (dir === path.dirname(dir)) break;
      }
    }
  }
  for (const line of raw.split("\n")) {
    const p = line.trim();
    if (!p || p.startsWith("#")) continue;
    try {
      // Named by index, never by content: the pattern is itself the secret.
      SENSITIVE.push({ name: `sensitive pattern #${SENSITIVE.length}`, re: new RegExp(p) });
    } catch {
      console.error(`  (ignoring an unparseable sensitive pattern)`);
    }
  }
}

// Routes worth proving. `auth: false` must render without a token.
const ROUTES = [
  { path: "/auth/login", name: "login", auth: false },
  { path: "/", name: "dashboard", auth: true },
  { path: "/config", name: "config-settings", auth: true },
  { path: "/config/history", name: "config-history", auth: true },
  { path: "/helm", name: "updates", auth: true },
  { path: "/helm/history", name: "helm-history", auth: true },
  { path: "/hooks", name: "hooks", auth: true },
  { path: "/setup", name: "setup", auth: true },
  { path: "/system", name: "system", auth: true },
  { path: "/audit", name: "audit", auth: true },
  { path: "/rtc", name: "rtc", auth: true },
];

// Console noise that is not a defect.
const IGNORE = [
  /favicon/i,
  /Download the React DevTools/i,
  /\[vite\]/i,
];

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

  const page = await ctx.newPage();
  const problems = [];

  page.on("console", (msg) => {
    if (msg.type() !== "error") return;
    const text = msg.text();
    if (IGNORE.some((re) => re.test(text))) return;
    problems.push(`console: ${text}`);
  });
  page.on("pageerror", (err) => problems.push(`pageerror: ${err.message}`));

  // Counting the page's own API calls is the only precise answer to "is it
  // finished loading?" — every indirect signal tried before this one lied.
  let apiPending = 0;
  let apiSeen = 0;
  let lastAPIActivity = 0; // a request starting *or* ending both count as activity
  const isAPI = (url) => url.includes("/api/v1/");
  page.on("request", (r) => {
    if (isAPI(r.url())) {
      apiPending++;
      apiSeen++;
      lastAPIActivity = Date.now();
    }
  });
  page.on("requestfinished", (r) => {
    if (isAPI(r.url())) {
      apiPending--;
      lastAPIActivity = Date.now();
    }
  });
  page.on("requestfailed", (req) => {
    const url = req.url();
    if (isAPI(url)) {
      apiPending--;
      lastAPIActivity = Date.now();
    }
    if (IGNORE.some((re) => re.test(url))) return;
    problems.push(`request failed: ${url} (${req.failure()?.errorText ?? "?"})`);
  });

  let httpStatus = 0;
  let redacted = 0;
  try {
    const resp = await page.goto(BASE + route.path, { waitUntil: "networkidle", timeout: 30_000 });
    httpStatus = resp?.status() ?? 0;
    if (httpStatus >= 400) problems.push(`HTTP ${httpStatus}`);

    // A blank root means the bundle loaded but React never mounted — exactly the
    // failure mode a status-code check misses.
    //
    // Poll rather than sampling once: "networkidle" only means 500 ms without
    // requests, which on a code-split route can land *before* React mounts —
    // the chunk arrives, the network goes quiet, and the data query has not
    // started yet. Sampling at that instant reported /hooks as empty while the
    // page was perfectly fine on every manual check.
    //
    // innerText is also layout-dependent and reads empty until first paint, so
    // the deadline covers rendering as well as mounting. A genuinely dead route
    // still fails — it just gets 10 seconds to prove itself first.
    await page
      .waitForFunction(
        () => (document.getElementById("root")?.innerText ?? "").trim().length >= 10,
        { timeout: 10_000 },
      )
      .catch(() => {});

    const rendered = await page.evaluate(() => {
      const root = document.getElementById("root");
      return (root?.innerText ?? "").trim().length;
    });
    if (rendered < 10) problems.push("root rendered empty (React did not mount?)");

    // Mounted is not loaded. The check above is satisfied by the sidebar and a
    // skeleton placeholder, so a page whose data had not arrived passed happily —
    // and the screenshot of that pass was an empty dashboard with four grey
    // boxes. `/status` costs ~5 s on a cold release cache, which is exactly the
    // window this used to photograph.
    //
    // Three wrong fixes were tried before this one, and each failed in a way
    // worth naming:
    //
    //   1. waitForLoadState("networkidle") — resolves immediately once the page
    //      has reached that state once, which goto() already did. It never
    //      waited for anything.
    //   2. "wait until the content stops changing" — a skeleton is perfectly
    //      stable, so this exited *earliest* exactly when the page was still
    //      loading. A heuristic that fails hardest in the case it exists for is
    //      worse than none.
    //   3. A fixed delay — either too short for a cold `/status` (~4.7 s) or
    //      wasted on every fast route.
    //
    // What actually answers the question: are this page's API requests done?
    // Counted directly from the request/response events above, so it is precise
    // rather than inferred.
    // Not every page has data to wait for: /auth/login calls nothing. Waiting for
    // apiSeen > 0 on those can never succeed, so the loop below used to run its
    // full 25 s on the login route of every single run, and on *all eleven* routes
    // whenever the token was rejected — which is how a five-minute check turned
    // into a stalled one. "No request has started for a while" is the signal that
    // a page is done rather than slow.
    // One condition covers both: nothing in flight, and nothing has happened for
    // QUIET_MS. A page that asks for nothing is quiet from the start; a page whose
    // first response triggers a second wave is not quiet until that wave lands.
    const settleStart = Date.now();
    const settleDeadline = settleStart + 25_000;
    const QUIET_MS = 2_000;
    while (Date.now() < settleDeadline) {
      const quietSince = Math.max(lastAPIActivity, settleStart);
      if (apiPending === 0 && Date.now() - quietSince > QUIET_MS) break;
      await page.waitForTimeout(250);
    }
    if (apiPending > 0) {
      problems.push(`${apiPending} API request(s) still pending after 25 s`);
    }

    // If an authenticated route bounced us back to login, the token is bad —
    // report it rather than screenshotting a login page under the wrong name.
    if (route.auth && page.url().includes("/auth/login")) {
      problems.push("redirected to /auth/login — token rejected");
    }

    // Redact, then **verify**, then screenshot.
    //
    // The first version of this did one pass and claimed "nothing can re-render
    // in between". That was wrong and it leaked: on /rtc the redaction ran
    // before the API response arrived, React then re-rendered with the real
    // value, and the screenshot captured it. The tool built to prevent leaks had
    // exactly the flaw it exists to prevent, and only looking at the image
    // caught it.
    //
    // So the guarantee is not "we replaced it" but "it is not there any more" —
    // checked, with retries for a late render, and a hard failure if it survives.
    // A redaction that silently did not apply is worse than none.
    if (REDACTIONS.length) {
      const subs = REDACTIONS;
      const redactOnce = () =>
        page.evaluate((subs) => {
          const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
          const nodes = [];
          while (walker.nextNode()) nodes.push(walker.currentNode);
          let n = 0;
          for (const node of nodes) {
            let value = node.nodeValue;
            for (const { from, to } of subs) value = value.split(from).join(to);
            if (value !== node.nodeValue) {
              node.nodeValue = value;
              n++;
            }
          }
          return n;
        }, subs);

      const stillVisible = () =>
        page.evaluate(
          (needles) => {
            const text = document.body.innerText || "";
            return needles.filter((n) => text.includes(n));
          },
          REDACTIONS.map((r) => r.from),
        );

      let leaked = [];
      for (let attempt = 0; attempt < 4; attempt++) {
        redacted += await redactOnce();
        leaked = await stillVisible();
        if (leaked.length === 0) break;
        // A late render put it back. Give the page a moment and redo it.
        await page.waitForTimeout(600);
      }

      if (leaked.length > 0) {
        // Fail the route rather than write the screenshot. The whole point is
        // that the image is safe to publish; an unsafe one must not exist.
        problems.push(`redaction failed — still visible after 4 passes: ${leaked.join(", ")}`);
      }
    }

    // The category check, after every redaction has run. This is the one that
    // does not depend on anyone having anticipated the right string.
    const visible = await page.evaluate(() => document.body.innerText || "");
    const hits = SENSITIVE.filter((s) => s.re.test(visible)).map((s) => s.name);
    if (hits.length) {
      // Name the category, never the match — printing it here would put the
      // secret straight into a terminal or a CI log, one layer down.
      problems.push(`sensitive content visible: ${hits.join(", ")} — no screenshot written`);
    }

    const unsafe = problems.some(
      (p) => p.startsWith("redaction failed") || p.startsWith("sensitive content visible"),
    );
    if (!unsafe) {
      await page.screenshot({ path: path.join(OUT, `${route.name}.png`), fullPage: true });
    }
  } catch (err) {
    problems.push(`navigation: ${err.message}`);
  }

  await page.close();
  results.push({ ...route, httpStatus, redacted, apiCalls: apiSeen, status: problems.length ? "fail" : "pass", problems });
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

const skipped = results.filter((r) => r.status === "skipped").length;
if (skipped) console.log(`${skipped} route(s) skipped — set MATRIXCTRL_TOKEN to include them.`);

if (REDACTIONS.length) {
  const total = results.reduce((n, r) => n + (r.redacted ?? 0), 0);
  console.log(`Redaction: ${REDACTIONS.length} rule(s), ${total} text node(s) rewritten.`);
  if (!total) {
    console.log("  WARNING: no rule matched anything. Verify the strings before publishing.");
  }
}
console.log(failed ? `\n${failed} route(s) failed.` : "\nAll checked routes rendered cleanly.");
process.exit(failed ? 1 : 0);
