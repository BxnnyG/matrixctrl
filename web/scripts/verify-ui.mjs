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
  const isAPI = (url) => url.includes("/api/v1/");
  page.on("request", (r) => {
    if (isAPI(r.url())) {
      apiPending++;
      apiSeen++;
    }
  });
  page.on("requestfinished", (r) => {
    if (isAPI(r.url())) apiPending--;
  });
  page.on("requestfailed", (req) => {
    const url = req.url();
    if (isAPI(url)) apiPending--;
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
    const settleDeadline = Date.now() + 25_000;
    while (Date.now() < settleDeadline) {
      if (apiPending === 0 && apiSeen > 0) break;
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

    if (!problems.some((p) => p.startsWith("redaction failed"))) {
      await page.screenshot({ path: path.join(OUT, `${route.name}.png`), fullPage: true });
    }
  } catch (err) {
    problems.push(`navigation: ${err.message}`);
  }

  await page.close();
  results.push({ ...route, httpStatus, redacted, status: problems.length ? "fail" : "pass", problems });
}

await browser.close();

// Report
let failed = 0;
console.log(`\nUI verification — ${BASE}\n`);
for (const r of results) {
  if (r.status === "pass") {
    const red = r.redacted ? `  (${r.redacted} text node(s) redacted)` : "";
    console.log(`  PASS  ${r.path.padEnd(18)} ${r.name}.png${red}`);
  } else if (r.status === "skipped") {
    console.log(`  SKIP  ${r.path.padEnd(18)} ${r.reason}`);
  } else {
    failed++;
    console.log(`  FAIL  ${r.path.padEnd(18)} HTTP ${r.httpStatus}`);
    for (const p of r.problems) console.log(`          ${p}`);
  }
}

await writeFile(path.join(OUT, "results.json"), JSON.stringify(results, null, 2));
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
