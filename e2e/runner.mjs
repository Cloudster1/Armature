// A test runner of its own because Playwright's image is gigabytes and nothing
// is installed on the host; the alpine-chrome image is small and already here.
import { writeFileSync } from "node:fs";

const scenarios = [];
let registering = null;

// The driver names the module it is about to import, so ONLY can pick a whole
// file and the timings say where a slow scenario lives.
export function registeringFrom(file) {
  registering = file;
}

export function scenario(name, fn) {
  scenarios.push({ name, fn, file: registering });
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

// Scenarios each sign up their own organization, so they share nothing and run
// on several browsers at once; the driver supplies launch, open and load counts.
export async function run({ launch, open, artifacts, workers = DEFAULT_WORKERS }) {
  const results = [];

  // E2E_ONLY narrows the run to the scenarios whose names contain it, or to one
  // file's scenarios, so one failing case or one area can be re-run on its own.
  const only = process.env.E2E_ONLY ?? "";
  const chosen = only ? scenarios.filter((s) => chosenBy(only, s)) : scenarios;
  if (only && chosen.length === 0) {
    process.stdout.write(`  no scenario matches ${JSON.stringify(only)}\n`);
    return false;
  }

  const queue = [...chosen];
  const parallel = Math.max(1, Math.min(workers, queue.length));

  async function worker() {
    // One browser per worker for the whole run: launching one per scenario
    // cost more than most scenarios did.
    const browser = await launch();
    try {
      for (let next = queue.shift(); next; next = queue.shift()) {
        results.push(await runOne(next, browser));
      }
    } finally {
      await browser.close().catch(() => {});
    }
  }

  async function runOne({ name, fn, file }, browser) {
    const started = Date.now();
    // A fresh incognito context per scenario, so cookies and storage from one
    // never leak into the next on the same browser.
    const { context, page, loads } = await open(browser);
    try {
      await fn({ page, context });
      const ms = Date.now() - started;
      process.stdout.write(`  PASS  ${name} (${ms}ms)\n`);
      return { name, file, ok: true, ms, loads: loads() };
    } catch (error) {
      const ms = Date.now() - started;
      process.stdout.write(`  FAIL  ${name} (${ms}ms)\n        ${error.message}\n`);
      const result = { name, file, ok: false, ms, loads: loads(), error };
      if (artifacts) {
        const file = `${artifacts}/failure-${name.replace(/[^a-z0-9]+/gi, "-").toLowerCase()}.png`;
        try {
          await page.screenshot({ path: file });
          process.stdout.write(`        screenshot: ${file}\n`);
        } catch {
          // A screenshot of a broken page is a nicety, not a requirement.
        }
      }
      return result;
    } finally {
      await context.close().catch(() => {});
    }
  }

  const started = Date.now();
  await Promise.all(Array.from({ length: parallel }, worker));

  report(results);
  const failed = results.filter((r) => !r.ok);
  if (failed.length) {
    process.stdout.write("\n  failed:\n");
    for (const { name } of failed) process.stdout.write(`    ${name}\n`);
  }
  const seconds = Math.round((Date.now() - started) / 1000);
  process.stdout.write(
    `\n  ${results.length - failed.length}/${results.length} scenarios passed in ${seconds}s on ${parallel} browser${parallel === 1 ? "" : "s"}\n`,
  );
  if (artifacts) writeTimings(`${artifacts}/${TIMINGS_FILE}`, results, { seconds, workers: parallel, only });
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
function writeTimings(path, results, { seconds, workers, only }) {
  const lines = [...results]
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
    .map(({ name, file, ok, ms, loads }) => `    ${JSON.stringify({ name, file, ok, ms, loads })}`);
  const head = [`"seconds": ${seconds}`, `"workers": ${workers}`, `"only": ${JSON.stringify(only)}`, `"loads": ${totalLoads(results)}`];
  const body = `{\n${head.map((h) => `  ${h},\n`).join("")}  "scenarios": [\n${lines.join(",\n")}\n  ]\n}\n`;
  try {
    writeFileSync(path, body);
    process.stdout.write(`  timings: ${path}\n`);
  } catch (error) {
    // The run's verdict stands without its timings.
    process.stdout.write(`  timings not written: ${error.message}\n`);
  }
}
