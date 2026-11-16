#!/usr/bin/env node
// End-to-end run against a TEST cluster (etappe 121, docs/DESIGN.md §4.130).
//
// What the operator does by hand, in a real browser, in this order:
//   1. log in with the form (not with an injected token),
//   2. open every route the router knows,
//   3. change one setting, press "Übernehmen", wait until it is applied, prove on the
//      cluster that it arrived, put the old value back and apply that too.
//
//   node scripts/e2e.mjs --base https://panel.example.com [--out DIR]
//
// Environment:
//   MATRIXCTRL_ADMIN_PASSWORD  the bootstrap admin's password (user "admin"). `make e2e`
//                              reads it from the local test cluster's secret if unset.
//   E2E_READONLY=1             steps 1 and 2 only; nothing is written to the cluster.
//   E2E_ELEMENT_URL            URL of the test cluster Element Web (not a public name that may be another cluster). When set, its config.json must
//                              show the test value, as extra proof next to the Helm
//                              revision.
//
// Never meant for production: step 3 changes the managed Element Web and redeploys it.
// It refuses to start while changes are already pending (someone else's work must not be
// applied by a test) and always puts the original value back, also after a failure.
//
// Exit: 0 all green · 1 a check failed · 2 usage / refused to start.

import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { checkRoute, redactText } from "./lib/ui-check.mjs";
import { EXCLUDE, nameOf, routerPaths as routerPaths_ } from "./lib/routes.mjs";

const args = process.argv.slice(2);
const argOf = (name, fallback) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback;
};
const BASE = (argOf("--base", process.env.MATRIXCTRL_BASE) || "").replace(/\/$/, "");
const OUT = argOf("--out", "/tmp/matrixctrl-e2e");
const PASSWORD = process.env.MATRIXCTRL_ADMIN_PASSWORD || "";
const READONLY = process.env.E2E_READONLY === "1";
const ELEMENT_URL = (process.env.E2E_ELEMENT_URL || "").replace(/\/$/, "");
const TEST_VALUE = `E2E-${Date.now().toString(36)}`;

if (!BASE) {
  console.error("usage: e2e.mjs --base <url>   (password in MATRIXCTRL_ADMIN_PASSWORD)");
  process.exit(2);
}
if (!PASSWORD) {
  console.error("MATRIXCTRL_ADMIN_PASSWORD is not set. Use `make e2e`, which reads it from the test cluster.");
  process.exit(2);
}

// The host of the panel and the password never reach the output.
const safe = (s) => redactText(s, [{ from: BASE, to: "<panel>" }, { from: PASSWORD, to: "<password>" }, ...(ELEMENT_URL ? [{ from: ELEMENT_URL, to: "<element>" }] : [])]);
const failures = [];
const fail = (what) => { failures.push(what); console.log(`  FAIL  ${safe(what)}`); };
const ok = (what) => console.log(`  PASS  ${safe(what)}`);
const step = (what) => console.log(`\n${what}`);

// --- Routes: from the router, not from a list (lib/routes.mjs) -----------------------
//
// A route with a parameter needs a resolver (how to find a real id) or an exclusion with a
// reason; a route with neither fails the run — so a new screen cannot slip past unopened.
const routerPaths = await routerPaths_();

// A resolver returns { path } or { skip: reason } (nothing to open on this cluster). An
// API error is not "nothing": it throws, and the route fails.
const RESOLVE = {
  "/config/$slice": async (api) => {
    const slices = await api("/api/v1/config/slices");
    return slices.length ? { path: `/config/${encodeURIComponent(slices[0].name)}` } : { skip: "no config sections" };
  },
  "/hooks/$id": async (api) => {
    const hooks = await api("/api/v1/hooks");
    return hooks.length ? { path: `/hooks/${encodeURIComponent(hooks[0].id)}` } : { skip: "no hooks" };
  },
  "/rooms/$id": async (api) => {
    // The bootstrap login has no Matrix account behind it, so the room list is closed to
    // it by design. That is a state of the session, not an error to hide.
    const st = await api("/api/v1/rooms/state");
    if (!st.connected) return { skip: "bootstrap session has no Matrix access (room list closed by design)" };
    const page = await api("/api/v1/rooms?limit=1");
    const first = (page.rooms ?? page.items ?? [])[0];
    return first ? { path: `/rooms/${encodeURIComponent(first.room_id ?? first.id)}` } : { skip: "no rooms" };
  },
};


// --- Browser ----------------------------------------------------------------------------
await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();
const newCtx = () => browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: "dark", ignoreHTTPSErrors: true });
const ctx = await newCtx();
const cfg = { base: BASE, out: OUT, redactions: [], redactIps: true, sensitiveFails: false };

let token = "";
const api = async (p, init = {}) => {
  const res = await ctx.request.fetch(BASE + p, {
    method: init.method ?? "GET",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    data: init.body,
  });
  if (!res.ok()) throw new Error(`${init.method ?? "GET"} ${p} -> HTTP ${res.status()}`);
  return res.json();
};
const revision = async () => (await api("/api/v1/helm/releases/ess")).revision;
const brandNow = async () => (await api("/api/v1/config/tasks")).values["web.brand"];

let original = null;
let wroteSomething = false;

try {
  // 0. The login page itself, before anyone is logged in.
  step("Login");
  const loginCheck = await checkRoute(await newCtx(), { path: "/auth/login", name: "login", auth: false }, cfg);
  loginCheck.status === "pass" ? ok("/auth/login renders") : fail(`/auth/login: ${loginCheck.problems.join("; ")}`);

  // 1. The form.
  const page = await ctx.newPage();
  await page.goto(BASE + "/auth/login", { waitUntil: "networkidle" });
  // The form only exists when Matrix login is off (bootstrap mode). With it on there is
  // nothing to type into, and saying so beats a timeout.
  const userField = page.locator('input[autocomplete="username"]');
  try {
    await userField.waitFor({ state: "visible", timeout: 15_000 });
  } catch {
    fail("login form not shown (is Matrix login enabled? this run needs bootstrap login)");
    throw new Error("abort");
  }
  await userField.fill("admin");
  await page.locator('input[type="password"]').fill(PASSWORD);
  await page.getByRole("button", { name: "Anmelden" }).click();
  try {
    await page.waitForFunction(() => !!localStorage.getItem("matrixctrl_token") && !location.pathname.startsWith("/auth"), null, { timeout: 20_000 });
  } catch {
    const msg = (await page.locator("form").locator("xpath=..").innerText().catch(() => "")).replace(/\s+/g, " ").slice(0, 160);
    fail(`login with the form did not succeed — page says: ${msg}`);
    throw new Error("abort");
  }
  token = await page.evaluate(() => localStorage.getItem("matrixctrl_token"));
  ok("logged in through the form");
  await page.close();

  // 2. Every route.
  step("Routes");
  const todo = [];
  for (const p of routerPaths) {
    if (p === "/auth/login") continue; // done above, unauthenticated
    if (EXCLUDE[p]) { console.log(`  EXCL  ${p}  — ${EXCLUDE[p]}`); continue; }
    if (p.includes("$")) {
      if (!RESOLVE[p]) { fail(`route ${p} has a parameter and neither a resolver nor an exclusion in e2e.mjs`); continue; }
      try {
        const r = await RESOLVE[p](api);
        if (r.skip) { console.log(`  SKIP  ${p}  — ${r.skip}`); continue; }
        todo.push(r.path);
      } catch (e) { fail(`resolver for ${p}: ${e.message}`); }
      continue;
    }
    todo.push(p);
  }
  for (const p of todo) {
    const r = await checkRoute(ctx, { path: p, name: nameOf(p), auth: true }, cfg);
    r.status === "pass" ? ok(`${p}  (${r.apiCalls} API call(s))`) : fail(`${p}: ${r.problems.join("; ")}`);
  }

  // 3. A change all the way to the cluster.
  if (READONLY) {
    step("Write step\n  SKIP  E2E_READONLY=1");
  } else {
    step("Write step: Name der App");
    const diff = (await api("/api/v1/config/diff")).diff ?? "";
    if (diff.trim()) {
      console.log("  REFUSED  changes are already pending on this cluster; applying them is not this test's business.");
      console.log("           Apply or discard them, then run again (or set E2E_READONLY=1).");
      failures.push("pending changes before start");
      throw new Error("abort");
    }
    original = await brandNow();
    console.log(`  original value: ${original.is_default ? "(default)" : JSON.stringify(original.value)}`);
    const revBefore = await revision();

    wroteSomething = true;
    await changeBrandInUI(TEST_VALUE);
    await applyInUI();
    const revAfter = await revision();
    revAfter === revBefore + 1 ? ok(`Helm revision ${revBefore} -> ${revAfter}`) : fail(`Helm revision ${revBefore} -> ${revAfter}, expected +1`);
    const now = await brandNow();
    now.value === TEST_VALUE ? ok("settings read the test value back") : fail(`settings show ${JSON.stringify(now.value)}, expected the test value`);
    if (ELEMENT_URL) await expectElementBrand(TEST_VALUE);
  }
} catch (e) {
  if (e.message !== "abort") fail(`unexpected: ${e.message}`);
} finally {
  // Always back to where it was — also after a failure half way through.
  if (wroteSomething && original) {
    step("Restore");
    try {
      await changeBrandInUI(original.is_default ? null : original.value);
      await applyInUI();
      const back = await brandNow();
      const same = back.is_default === original.is_default && (original.is_default || back.value === original.value);
      same ? ok("original value restored and applied") : fail("restore did not reach the settings");
      if (ELEMENT_URL && !original.is_default) await expectElementBrand(original.value);
    } catch (e) {
      fail(`restore through the UI failed (${e.message}); trying the API`);
      try {
        await api("/api/v1/config/tasks", { method: "POST", body: { changes: { "web.brand": original.is_default ? null : original.value } } });
        const { upgrade_id } = await api("/api/v1/helm/releases/ess/apply-config", { method: "POST", body: { message: "config: e2e restore" } });
        console.log(`  restore started through the API (${upgrade_id}); check the Updates page`);
      } catch (e2) {
        const left = await brandNow().then((b) => (b.is_default ? "(default)" : JSON.stringify(b.value)), () => `unknown (probably ${JSON.stringify(TEST_VALUE)})`);
        const pend = await api("/api/v1/config/diff").then((d) => (d.diff ?? "").trim() ? "unapplied changes are pending" : "nothing pending", () => "pending state unknown");
        fail(`RESTORE FAILED — the cluster is left with "Name der App" = ${left} (${pend}); original was ${original.is_default ? "(default)" : JSON.stringify(original.value)}. Set it back by hand: ${e2.message}`);
      }
    }
  }
  await browser.close();
}

console.log(failures.length ? `\nE2E FAILED: ${failures.length} problem(s).` : "\nE2E green.");
await writeFile(path.join(OUT, "e2e-result.json"), JSON.stringify({ failures: failures.map(safe) }, null, 2));
process.exit(failures.length ? 1 : 0);

// --- Helpers that drive the UI ------------------------------------------------------------

async function changeBrandInUI(value) {
  const page = await ctx.newPage();
  try {
    await page.goto(BASE + "/config", { waitUntil: "domcontentloaded", timeout: 90_000 });
    // The input has no label of its own; the row is the nearest block holding both the
    // label text and an input.
    const field = page.locator('xpath=//span[normalize-space()="Name der App"]/ancestor::div[.//input][1]//input');
    await field.waitFor({ state: "visible", timeout: 20_000 });
    if (value === null) {
      // Scoped to this row: the page has a "Zurück auf Standard" for every changed setting,
      // and `.first()` once reset somebody else's TURN switch instead.
      await page.locator('xpath=//span[normalize-space()="Name der App"]/ancestor::div[.//input][1]//button[normalize-space()="Zurück auf Standard"]').click();
    } else {
      await field.fill(value);
      await field.press("Enter");
    }
    await page.getByText("Änderungen ausstehend").waitFor({ timeout: 20_000 });
    await page.screenshot({ path: path.join(OUT, "write-pending.png") }).catch(() => {});
  } finally {
    await page.close();
  }
}

async function applyInUI() {
  const page = await ctx.newPage();
  try {
    await page.goto(BASE + "/config", { waitUntil: "domcontentloaded", timeout: 90_000 });
    const apply = page.getByRole("button", { name: "Übernehmen", exact: true });
    await apply.waitFor({ timeout: 180_000 }); // the preview ("Prüfe…") is slow while pods still settle
    await page.waitForFunction(() => {
      const b = [...document.querySelectorAll("button")].find((x) => x.textContent?.trim() === "Übernehmen");
      return b && !b.disabled;
    }, null, { timeout: 60_000 });
    await apply.click();
    const done = page.getByText("Übernommen — alle Dienste laufen mit der neuen Konfiguration.");
    const bad = page.getByText(/Übernehmen ist fehlgeschlagen|Übernehmen wurde abgebrochen|Es wurde nichts gespeichert|hook danach ist fehlgeschlagen/);
    // The loser of the race is still waiting when the page closes; its rejection is
    // expected and must not take the process down.
    const won = done.waitFor({ timeout: 600_000 }).then(() => "done");
    const lost = bad.first().waitFor({ timeout: 600_000 }).then(() => "bad");
    won.catch(() => {});
    lost.catch(() => {});
    const which = await Promise.race([won, lost]);
    await page.screenshot({ path: path.join(OUT, "write-applied.png") }).catch(() => {});
    if (which !== "done") throw new Error("the panel reported that applying failed");
  } finally {
    await page.close();
  }
}

async function expectElementBrand(expected) {
  // Element Web restarts after the apply; allow it a moment to serve the new file.
  let last = "";
  for (let i = 0; i < 30; i++) {
    try {
      const res = await ctx.request.get(ELEMENT_URL + "/config.json", { headers: { "Cache-Control": "no-cache" } });
      last = (await res.json()).brand;
      if (last === expected) { ok("Element Web config.json shows the value"); return; }
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 2000));
  }
  fail(`Element Web config.json brand is ${JSON.stringify(last)}, expected the value just set`);
}
