import puppeteer from "puppeteer";
import { AssertionError, DEFAULT_WORKERS, registeringFrom, run } from "./runner.mjs";
import {
  BASE_URL,
  WAIT,
} from "./helpers.mjs";

// Each module registers its scenarios as it loads, in the order below, which is
// the order they are queued in.
const SCENARIO_FILES = [
  "accounts",
  "work",
  "shell",
  "projects",
  "templates",
  "git",
  "desk",
  "desk-mail",
  "dashboards",
  "issue-details",
  "organization",
  "board",
  "hierarchy",
  "plan",
  "workflows",
  "plan-calendar",
  "search",
  "plan-links",
  "sprints",
  "access",
  "invitations",
  "organization-delete",
  "users",
  "holidays",
  "absences",
  "resources",
  "themes",
  "arrange",
];

for (const name of SCENARIO_FILES) {
  registeringFrom(`${name}.mjs`);
  await import(`./scenarios/${name}.mjs`);
}

// ----------------------------------------------------------------- driver ---

async function launch() {
  return puppeteer.launch({
    // Not CHROME_PATH: the image sets that to a directory, which puppeteer
    // then tries to execute.
    executablePath: process.env.E2E_CHROME ?? "/usr/bin/chromium-browser",
    args: [
      "--no-sandbox",
      "--disable-dev-shm-usage",
      "--disable-crash-reporter",
      "--disable-crashpad",
      `--user-data-dir=/tmp/chrome-${Math.random().toString(36).slice(2)}`,
    ],
  });
}

/** Marks the console line a refusal under the Content Security Policy is reported on. */
const CSP_MARK = "e2e: refused by the CSP:";

async function open(browser) {
  const context = await browser.createBrowserContext();
  // Every full load in the context counts, on a page the scenario opens too. A
  // route change inside the app has no DOMContentLoaded; a goto never misses one.
  let loads = 0;
  const counted = new WeakSet();
  const count = (page) => {
    if (!page || counted.has(page)) return;
    counted.add(page);
    page.on("domcontentloaded", () => {
      loads += 1;
    });
  };
  context.on("targetcreated", async (target) => {
    if (target.type() === "page") count(await target.page().catch(() => null));
  });
  const page = await context.newPage();
  count(page);
  await page.setViewport({ width: 1280, height: 860 });
  page.setDefaultTimeout(WAIT);
  // The production nginx sends a CSP, and what it refuses is a bug in what
  // ships even when the scenario's own checks still pass.
  const refused = [];
  await page.evaluateOnNewDocument((mark) => {
    document.addEventListener("securitypolicyviolation", (e) => console.error(`${mark} ${e.violatedDirective} ${e.blockedURI}`));
  }, CSP_MARK);
  page.on("console", (message) => {
    if (message.text().startsWith(CSP_MARK)) refused.push(`${message.text().slice(CSP_MARK.length + 1)} on ${page.url()}`);
  });
  const check = async () => {
    // A refusal is reported in a task of its own: one more round trip lets the last one arrive.
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 0))).catch(() => {});
    if (refused.length) throw new AssertionError(`the Content Security Policy refused ${refused.join("; ")}`);
  };
  return { context, page, loads: () => loads, check };
}

const workers = Number(process.env.E2E_WORKERS) || DEFAULT_WORKERS;
process.stdout.write(`\nBrowser end-to-end suite against ${BASE_URL}\n\n`);
const ok = await run({ launch, open, workers, artifacts: process.env.E2E_ARTIFACTS ?? null });
process.exit(ok ? 0 : 1);
