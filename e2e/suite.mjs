import puppeteer from "puppeteer";
import { AssertionError, DEFAULT_WORKERS, run } from "./runner.mjs";
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
import "./scenarios/invitations.mjs";
import "./scenarios/organization-delete.mjs";
import "./scenarios/users.mjs";
import "./scenarios/holidays.mjs";
import "./scenarios/absences.mjs";
import "./scenarios/resources.mjs";
import "./scenarios/themes.mjs";
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

/** Marks the console line a refusal under the Content Security Policy is reported on. */
const CSP_MARK = "e2e: refused by the CSP:";

async function open(browser) {
  const context = await browser.createBrowserContext();
  const page = await context.newPage();
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
  return { context, page, check };
}

const workers = Number(process.env.E2E_WORKERS) || DEFAULT_WORKERS;
process.stdout.write(`\nBrowser end-to-end suite against ${BASE_URL}\n\n`);
const ok = await run({ launch, open, workers, screenshotDir: process.env.E2E_ARTIFACTS ?? null });
process.exit(ok ? 0 : 1);
