// A test runner of its own because Playwright's image is gigabytes and nothing
// is installed on the host; the alpine-chrome image is small and already here.
import { writeFileSync } from "node:fs";

const scenarios = [];
let registering = null;

/** Which part of the run is under way: setup, the parallel pool, or the serial lane. */
let lane = "setup";

// The driver names the module it is about to import, so ONLY can pick a whole
// file and the timings say where a slow scenario lives.
export function registeringFrom(file) {
  registering = file;
}

/** A scenario that shares nothing with the others, run in the parallel pool. */
export function scenario(name, fn) {
  scenarios.push({ name, fn, file: registering, serial: false });
}

/**
 * A scenario that changes something every other scenario lives with, run
 * after the pool on one browser. It puts back what it changed before it ends.
 */
export function serialScenario(name, fn) {
  scenarios.push({ name, fn, file: registering, serial: true });
}

export class AssertionError extends Error {}

export const expect = {
  equal(actual, want, what) {
    if (actual !== want) {
      throw new AssertionError(`${what}: got ${JSON.stringify(actual)}, want ${JSON.stringify(want)}`);
    }
  },
  contains(haystack, needle, what) {
    if (!String(haystack).includes(needle)) {
      throw new AssertionError(`${what}: ${JSON.stringify(String(haystack).slice(0, 200))} does not contain ${JSON.stringify(needle)}`);
    }
  },
  // innerText is rendered text, so a heading styled with text-transform comes
  // back upper case; assertions about content should not care.
  containsInsensitive(haystack, needle, what) {
    if (!String(haystack).toLowerCase().includes(String(needle).toLowerCase())) {
      throw new AssertionError(`${what}: ${JSON.stringify(String(haystack).slice(0, 200))} does not contain ${JSON.stringify(needle)} (case-insensitively)`);
    }
  },
  notContains(haystack, needle, what) {
    if (String(haystack).includes(needle)) {
      throw new AssertionError(`${what}: unexpectedly contains ${JSON.stringify(needle)}`);
    }
  },
  truthy(value, what) {
    if (!value) throw new AssertionError(`${what}: expected a truthy value, got ${JSON.stringify(value)}`);
  },
};

/** How many browsers run scenarios side by side unless E2E_WORKERS says otherwise. */
export const DEFAULT_WORKERS = 4;

/** How many of the slowest scenarios the end of a run lists. */
const SLOWEST_SHOWN = 10;

/** Where in the artifacts directory each run leaves its per-scenario timings. */
const TIMINGS_FILE = "timings.json";

/** Whether ONLY asks for a scenario: by a piece of its name, or by its whole file name. */
function chosenBy(only, { name, file }) {
  return name.includes(only) || file === only || file === `${only}.mjs`;
}

/**
 * The requests that change what every scenario lives with, as method, path and
 * words: reviewed by hand, since a URL cannot say whether what it changes is shared.
 */
const INSTALLATION_WIDE = [
  ["PUT", /^\/settings\/second-factor$/, "who must sign in with a second factor"],
  ["DELETE", /^\/organization$/, "the organization itself"],
  ["PUT", /^\/installation$/, "the installation's name"],
  ["PUT", /^\/themes\/default$/, "the default theme"],
  ["PUT", /^\/oidc-provider$/, "the single sign-on provider"],
  ["POST|PATCH|DELETE", /^\/roles(\/|$)/, "the roles and what they grant"],
  ["PUT", /^\/workflow-schemes\/[^/]+\/default$/, "the default workflow scheme"],
  ["PUT", /^\/issue-arrangement$/, "the arrangement every project follows"],
  ["POST", /^\/fields(\/[^/]+\/promote)?$/, "a field on every project"],
  ["POST|PUT|PATCH|DELETE", /^\/holiday-calendars(\/|$)/, "the holiday calendars"],
  ["POST", /^\/automation\/rules$/, "a rule over every project"],
  ["POST|PATCH|DELETE", /^\/webhooks(\/|$)/, "a webhook that hears every project"],
  ["PATCH|DELETE", /^\/labels\//, "a label everybody tags with"],
];

/** Where the API is served, which every guarded path is under. */
const API_PREFIX = "/api/v1";

/**
 * Adds a request to the guarded ones, for what only exists once the run has
 * looked, such as the workflows the installation started with.
 */
export function guardAlso(methods, path, what) {
  INSTALLATION_WIDE.push([methods, path, what]);
}

/**
 * Why a request may not be made from where the run is, or null when it may: a
 * scenario in the parallel pool that changes something installation wide.
 */
export function refusal(method, url) {
  if (lane !== "parallel") return null;
  const { pathname } = new URL(url, "http://e2e.invalid");
  if (!pathname.startsWith(`${API_PREFIX}/`)) return null;
  const path = pathname.slice(API_PREFIX.length);
  const hit = INSTALLATION_WIDE.find(([methods, pattern]) => methods.split("|").includes(method) && pattern.test(path));
  if (!hit) return null;
  return `${method} ${path} changes ${hit[2]} while other scenarios run beside it; register this scenario with serialScenario and put back what it changed before it ends.`;
}

/** A shuffle that a seed repeats, so an order that failed can be run again. */
function shuffled(list, seed) {
  let state = seed >>> 0;
  const next = () => {
    // mulberry32: small, and the same sequence for the same seed everywhere.
    state = (state + 0x6d2b79f5) >>> 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const out = [...list];
  for (let i = out.length - 1; i > 0; i -= 1) {
    const j = Math.floor(next() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

/** The largest seed E2E_SHUFFLE is drawn from when it is only switched on. */
const SEED_RANGE = 1_000_000;

/** The seed E2E_SHUFFLE asks for: its own number, a fresh one for "1", or none. */
function shuffleSeed() {
  const asked = process.env.E2E_SHUFFLE ?? "";
  if (!asked || asked === "0") return null;
  const seed = Number(asked);
  return Number.isInteger(seed) && seed > 1 ? seed : 2 + Math.floor(Math.random() * SEED_RANGE);
}

// The pool's scenarios keep apart by unique names and keys, so several browsers
// share one installation; the serial lane follows on one. The driver supplies the rest.
export async function run({ launch, open, prepare, artifacts, workers = DEFAULT_WORKERS }) {
  const results = [];

  // E2E_ONLY narrows the run to the scenarios whose names contain it, or to one
  // file's scenarios, so one failing case or one area can be re-run on its own.
  const only = process.env.E2E_ONLY ?? "";
  const chosen = only ? scenarios.filter((s) => chosenBy(only, s)) : scenarios;
  if (only && chosen.length === 0) {
    process.stdout.write(`  no scenario matches ${JSON.stringify(only)}\n`);
    return false;
  }

  const seed = shuffleSeed();
  const pooled = chosen.filter((s) => !s.serial);
  const queue = seed === null ? pooled : shuffled(pooled, seed);
  const serial = chosen.filter((s) => s.serial);
  const parallel = Math.max(1, Math.min(workers, queue.length));
  if (seed !== null) process.stdout.write(`  the pool is shuffled; E2E_SHUFFLE=${seed} repeats this order\n\n`);

  const started = Date.now();
  try {
    // A browser too, for a setup an empty installation asks of its sign-up page.
    await prepare?.({ launch, open });
  } catch (error) {
    process.stdout.write(`  the run could not be set up: ${error.message}\n`);
    return false;
  }

  async function worker(from) {
    // One browser per worker for the whole run: launching one per scenario
    // cost more than most scenarios did.
    const browser = await launch();
    try {
      for (let next = from.shift(); next; next = from.shift()) {
        results.push(await runOne(next, browser));
      }
    } finally {
      await browser.close().catch(() => {});
    }
  }

  async function runOne({ name, fn, file, serial: alone }, browser) {
    const began = Date.now();
    // A fresh incognito context per scenario, so cookies and storage from one
    // never leak into the next on the same browser.
    const { context, page, loads, check, refused } = await open(browser);
    const mark = alone ? "serial " : "";
    // A refused request is the reason, even when the scenario then failed on something else.
    const guarded = () => {
      const seen = refused?.() ?? [];
      return seen.length ? new AssertionError(seen.join(" ")) : null;
    };
    try {
      await fn({ page, context });
      const blocked = guarded();
      if (blocked) throw blocked;
      await check?.();
      const ms = Date.now() - began;
      process.stdout.write(`  PASS  ${mark}${name} (${ms}ms)\n`);
      return { name, file, ok: true, ms, loads: loads(), serial: alone };
    } catch (error) {
      const ms = Date.now() - began;
      const reason = guarded() ?? error;
      process.stdout.write(`  FAIL  ${mark}${name} (${ms}ms)\n        ${reason.message}\n`);
      const result = { name, file, ok: false, ms, loads: loads(), error: reason, serial: alone };
      if (artifacts) {
        const shot = `${artifacts}/failure-${name.replace(/[^a-z0-9]+/gi, "-").toLowerCase()}.png`;
        try {
          await page.screenshot({ path: shot });
          process.stdout.write(`        screenshot: ${shot}\n`);
        } catch {
          // A screenshot of a broken page is a nicety, not a requirement.
        }
      }
      return result;
    } finally {
      await context.close().catch(() => {});
    }
  }

  lane = "parallel";
  await Promise.all(Array.from({ length: parallel }, () => worker(queue)));
  // Nothing else runs while a serial scenario changes what everybody shares.
  lane = "serial";
  if (serial.length) await worker([...serial]);
  lane = "done";

  report(results);
  const failed = results.filter((r) => !r.ok);
  if (failed.length) {
    process.stdout.write("\n  failed:\n");
    for (const { name } of failed) process.stdout.write(`    ${name}\n`);
  }
  const seconds = Math.round((Date.now() - started) / 1000);
  const pool = results.filter((r) => !r.serial).length;
  process.stdout.write(
    `\n  ${results.length - failed.length}/${results.length} scenarios passed in ${seconds}s: ` +
      `${pool} in parallel on ${parallel} browser${parallel === 1 ? "" : "s"}, then ${results.length - pool} serial on one\n`,
  );
  if (artifacts) writeTimings(`${artifacts}/${TIMINGS_FILE}`, results, { seconds, workers: parallel, only, seed });
  return failed.length === 0;
}

function totalLoads(results) {
  return results.reduce((sum, r) => sum + r.loads, 0);
}

// The slowest scenarios and the page loads are where a run's time goes, so
// they are what to look at before making the suite faster.
function report(results) {
  const slowest = [...results].sort((a, b) => b.ms - a.ms).slice(0, SLOWEST_SHOWN);
  process.stdout.write("\n  slowest:\n");
  for (const { name, file, ms, loads } of slowest) {
    process.stdout.write(`    ${String(ms).padStart(6)}ms ${String(loads).padStart(3)} loads  ${name} (${file})\n`);
  }
  const loads = totalLoads(results);
  const each = results.length ? (loads / results.length).toFixed(1) : "0";
  process.stdout.write(`\n  ${loads} page loads, ${each} per scenario\n`);
}

// One scenario per line and sorted by name, so two runs' files compare line by line.
function writeTimings(path, results, { seconds, workers, only, seed }) {
  const lines = [...results]
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
    .map(({ name, file, ok, ms, loads, serial }) => `    ${JSON.stringify({ name, file, ok, ms, loads, serial })}`);
  const head = [
    `"seconds": ${seconds}`,
    `"workers": ${workers}`,
    `"only": ${JSON.stringify(only)}`,
    `"shuffle": ${JSON.stringify(seed)}`,
    `"loads": ${totalLoads(results)}`,
  ];
  const body = `{\n${head.map((h) => `  ${h},\n`).join("")}  "scenarios": [\n${lines.join(",\n")}\n  ]\n}\n`;
  try {
    writeFileSync(path, body);
    process.stdout.write(`  timings: ${path}\n`);
  } catch (error) {
    // The run's verdict stands without its timings.
    process.stdout.write(`  timings not written: ${error.message}\n`);
  }
}
