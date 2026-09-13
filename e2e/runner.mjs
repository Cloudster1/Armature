// A test runner of its own because Playwright's image is gigabytes and nothing
// is installed on the host; the alpine-chrome image is small and already here.

const scenarios = [];

export function scenario(name, fn) {
  scenarios.push({ name, fn });
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

// Scenarios each sign up their own organization, so they share nothing and run
// on several browsers at once; the driver supplies launch and open.
export async function run({ launch, open, screenshotDir, workers = DEFAULT_WORKERS }) {
  const results = [];

  // E2E_ONLY narrows the run to the scenarios whose names contain it, so one
  // failing case can be re-run on its own.
  const only = process.env.E2E_ONLY ?? "";
  const chosen = only ? scenarios.filter((s) => s.name.includes(only)) : scenarios;
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

  async function runOne({ name, fn }, browser) {
    const started = Date.now();
    // A fresh incognito context per scenario, so cookies and storage from one
    // never leak into the next on the same browser.
    const { context, page } = await open(browser);
    try {
      await fn({ page, context });
      const ms = Date.now() - started;
      process.stdout.write(`  PASS  ${name} (${ms}ms)\n`);
      return { name, ok: true, ms };
    } catch (error) {
      const ms = Date.now() - started;
      process.stdout.write(`  FAIL  ${name} (${ms}ms)\n        ${error.message}\n`);
      if (screenshotDir) {
        const file = `${screenshotDir}/failure-${name.replace(/[^a-z0-9]+/gi, "-").toLowerCase()}.png`;
        try {
          await page.screenshot({ path: file });
          process.stdout.write(`        screenshot: ${file}\n`);
        } catch {
          // A screenshot of a broken page is a nicety, not a requirement.
        }
      }
      return { name, ok: false, ms, error };
    } finally {
      await context.close().catch(() => {});
    }
  }

  const started = Date.now();
  await Promise.all(Array.from({ length: parallel }, worker));

  const failed = results.filter((r) => !r.ok);
  if (failed.length) {
    process.stdout.write("\n  failed:\n");
    for (const { name } of failed) process.stdout.write(`    ${name}\n`);
  }
  const seconds = Math.round((Date.now() - started) / 1000);
  process.stdout.write(
    `\n  ${results.length - failed.length}/${results.length} scenarios passed in ${seconds}s on ${parallel} browser${parallel === 1 ? "" : "s"}\n`,
  );
  return failed.length === 0;
}
