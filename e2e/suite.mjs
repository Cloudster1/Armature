import puppeteer from "puppeteer";
import { DEFAULT_WORKERS, registeringFrom, run } from "./runner.mjs";
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
  return { context, page, loads: () => loads };
}

const workers = Number(process.env.E2E_WORKERS) || DEFAULT_WORKERS;
process.stdout.write(`\nBrowser end-to-end suite against ${BASE_URL}\n\n`);
const ok = await run({ launch, open, workers, artifacts: process.env.E2E_ARTIFACTS ?? null });
process.exit(ok ? 0 : 1);
