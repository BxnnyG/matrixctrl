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

import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

const args = process.argv.slice(2);
const argOf = (name, fallback) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback;
};

const BASE = (argOf("--base", process.env.MATRIXCTRL_BASE) || "").replace(/\/$/, "");
const OUT = argOf("--out", "/tmp/matrixctrl-verify");
const TOKEN = process.env.MATRIXCTRL_TOKEN || "";

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
  page.on("requestfailed", (req) => {
    const url = req.url();
    if (IGNORE.some((re) => re.test(url))) return;
    problems.push(`request failed: ${url} (${req.failure()?.errorText ?? "?"})`);
  });

  let httpStatus = 0;
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

    // If an authenticated route bounced us back to login, the token is bad —
    // report it rather than screenshotting a login page under the wrong name.
    if (route.auth && page.url().includes("/auth/login")) {
      problems.push("redirected to /auth/login — token rejected");
    }

    await page.screenshot({ path: path.join(OUT, `${route.name}.png`), fullPage: true });
  } catch (err) {
    problems.push(`navigation: ${err.message}`);
  }

  await page.close();
  results.push({ ...route, httpStatus, status: problems.length ? "fail" : "pass", problems });
}

await browser.close();

// Report
let failed = 0;
console.log(`\nUI verification — ${BASE}\n`);
for (const r of results) {
  if (r.status === "pass") {
    console.log(`  PASS  ${r.path.padEnd(18)} ${r.name}.png`);
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
console.log(failed ? `\n${failed} route(s) failed.` : "\nAll checked routes rendered cleanly.");
process.exit(failed ? 1 : 0);
