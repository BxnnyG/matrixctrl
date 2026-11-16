// Shared page check for verify-ui.mjs and e2e.mjs (etappe 121, DESIGN S12: one copy).
//
// Opens one route in a real browser and decides pass or fail: console errors, failed
// requests, an empty root, a bounce to the login page, pending API calls, and the
// sensitive-content scan before a screenshot is written. The long comments below were
// written against real defects in verify-ui.mjs and moved here unchanged.

import { mkdir } from "node:fs/promises";
import { readFileSync } from "node:fs";
import path from "node:path";

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
const IPV4_SRC = `\\b${OCTET}\\.${OCTET}\\.${OCTET}\\.${OCTET}\\b`;

// The documentation ranges reserved by RFC 5737 are what --redact-ips replaces
// real addresses *with*. They must be exempt, or the detector fires on the
// tool's own placeholder — which it did: the redaction ran, put 203.0.113.1 on
// the page, the scan called that an IP address, and the retry loop replaced a
// placeholder with a placeholder four times before giving up. A check that
// cannot tell its own output from a leak is not a check.
const DOC_RANGES = ["192.0.2.", "198.51.100.", "203.0.113."];
const realIPs = (text) =>
  [...String(text).matchAll(new RegExp(IPV4_SRC, "g"))]
    .map((m) => m[0])
    .filter((ip) => !DOC_RANGES.some((p) => ip.startsWith(p)));

SENSITIVE.push({ name: "IPv4 address", find: realIPs });

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
      const re = new RegExp(p, "g");
      SENSITIVE.push({
        name: `sensitive pattern #${SENSITIVE.length}`,
        find: (text) => [...String(text).matchAll(re)].map((m) => m[0]),
      });
    } catch {
      console.error(`  (ignoring an unparseable sensitive pattern)`);
    }
  }
}


// Console noise that is not a defect.
const IGNORE = [
  /favicon/i,
  /Download the React DevTools/i,
  /\[vite\]/i,
];

/** Text with every redaction rule applied — for anything that leaves the process. */
export const redactText = (s, redactions) =>
  redactions.reduce((acc, { from, to }) => acc.split(from).join(to), String(s));

/**
 * Check one route in an existing browser context.
 * cfg: { base, out, redactions: [{from,to}], redactIps: boolean, sensitiveFails?: boolean (default true) }
 * Returns { ...route, httpStatus, redacted, apiCalls, status: "pass"|"fail", problems }.
 */
export async function checkRoute(ctx, route, cfg) {
  const BASE = cfg.base;
  const OUT = cfg.out;
  const REDACTIONS = cfg.redactions ?? [];
  const REDACT_IPS = !!cfg.redactIps;
  await mkdir(OUT, { recursive: true });
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
    if (REDACTIONS.length || REDACT_IPS) {
      const subs = REDACTIONS;
      const redactOnce = () =>
        page.evaluate(
          ({ subs, ips, ipSrc, docRanges }) => {
            // Same regex as the detector, rebuilt here because this runs in the
            // page. RFC 5737 documentation range as the stand-in, so a reader can
            // tell at a glance that it is not a real address — and skipped on the
            // way in, so a second pass does not renumber its own placeholders.
            const ipRe = new RegExp(ipSrc, "g");
            const seen = new Map();
            const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
            const nodes = [];
            while (walker.nextNode()) nodes.push(walker.currentNode);
            let n = 0;
            for (const node of nodes) {
              let value = node.nodeValue;
              for (const { from, to } of subs) value = value.split(from).join(to);
              if (ips) {
                // Stable within a run: the same address always becomes the same
                // placeholder, so "resolves to X" and "X" still read as one fact.
                value = value.replace(ipRe, (m) => {
                  if (docRanges.some((p) => m.startsWith(p))) return m;
                  if (!seen.has(m)) seen.set(m, `203.0.113.${seen.size + 1}`);
                  return seen.get(m);
                });
              }
              if (value !== node.nodeValue) {
                node.nodeValue = value;
                n++;
              }
            }
            return n;
          },
          { subs, ips: REDACT_IPS, ipSrc: IPV4_SRC, docRanges: DOC_RANGES },
        );

      // Also asks about IP literals, so a late-rendered address gets the retry
      // loop rather than going straight to a hard failure.
      const stillVisible = () =>
        page.evaluate(
          ({ needles, ips, ipSrc, docRanges }) => {
            const text = document.body.innerText || "";
            const left = needles.filter((n) => text.includes(n));
            if (ips) {
              const real = [...text.matchAll(new RegExp(ipSrc, "g"))]
                .map((m) => m[0])
                .filter((ip) => !docRanges.some((p) => ip.startsWith(p)));
              if (real.length) left.push("an IP address");
            }
            return left;
          },
          {
            needles: REDACTIONS.map((r) => r.from),
            ips: REDACT_IPS,
            ipSrc: IPV4_SRC,
            docRanges: DOC_RANGES,
          },
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
    let withhold = false;
    const visible = await page.evaluate(() => document.body.innerText || "");
    const hits = SENSITIVE.filter((s) => s.find(visible).length > 0).map((s) => s.name);
    if (hits.length) {
      // Name the category, never the match — printing it here would put the
      // secret straight into a terminal or a CI log, one layer down.
      // e2e (cfg.sensitiveFails === false) tests a private cluster and publishes
      // nothing: the page is still checked, only the image is withheld.
      if (cfg.sensitiveFails === false) withhold = true;
      else problems.push(`sensitive content visible: ${hits.join(", ")} — no screenshot written`);
    }

    const unsafe = withhold || problems.some(
      (p) => p.startsWith("redaction failed") || p.startsWith("sensitive content visible"),
    );
    if (!unsafe) {
      await page.screenshot({ path: path.join(OUT, `${route.name}.png`), fullPage: true });
    }
  } catch (err) {
    problems.push(`navigation: ${err.message}`);
  }

  await page.close();
  return { ...route, httpStatus, redacted, apiCalls: apiSeen, status: problems.length ? "fail" : "pass", problems };
}
