import puppeteer from "puppeteer";
import { DEFAULT_WORKERS, run } from "./runner.mjs";
import {
  BASE_URL,
  WAIT,
} from "./helpers.mjs";

// Each module registers its scenarios as it loads, in the order below.
import "./scenarios/accounts.mjs";
import "./scenarios/work.mjs";
import "./scenarios/shell.mjs";
import "./scenarios/projects.mjs";
import "./scenarios/templates.mjs";
import "./scenarios/git.mjs";
import "./scenarios/desk.mjs";
import "./scenarios/desk-mail.mjs";
import "./scenarios/dashboards.mjs";
import "./scenarios/issue-details.mjs";
import "./scenarios/organization.mjs";
import "./scenarios/board.mjs";
import "./scenarios/hierarchy.mjs";
import "./scenarios/plan.mjs";
import "./scenarios/workflows.mjs";
import "./scenarios/plan-calendar.mjs";
import "./scenarios/search.mjs";
import "./scenarios/plan-links.mjs";
import "./scenarios/sprints.mjs";
import "./scenarios/access.mjs";
import "./scenarios/arrange.mjs";

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
  const page = await context.newPage();
  await page.setViewport({ width: 1280, height: 860 });
  page.setDefaultTimeout(WAIT);
  return { context, page };
}

const workers = Number(process.env.E2E_WORKERS) || DEFAULT_WORKERS;
process.stdout.write(`\nBrowser end-to-end suite against ${BASE_URL}\n\n`);
const ok = await run({ launch, open, workers, screenshotDir: process.env.E2E_ARTIFACTS ?? null });
process.exit(ok ? 0 : 1);
