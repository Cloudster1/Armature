// Takes the pictures in docs/ from the demo organization. Run through
// `make screenshots` against a stack that has been seeded.
import puppeteer from "puppeteer";
import { mkdirSync } from "node:fs";

const BASE = process.env.E2E_BASE_URL ?? "http://web:5173";
const OUT = `${process.env.E2E_ARTIFACTS ?? "/tmp"}/shots`;
const EMAIL = process.env.DEMO_EMAIL ?? "ada@armature.test";
const PASSWORD = process.env.DEMO_PASSWORD ?? "demo password please";
/** A moment for fonts and the last frame before the picture is taken. */
const SETTLE_MS = 600;

/** Each picture: its file name, the address, and what to wait for. */
const SHOTS = [
  ["ui-home", "/", "main"],
  ["ui-project", "/projects/CP", "[data-issue-row]"],
  ["ui-panel", "/projects/CP", "[data-issue-row]", { viewport: 1600, beside: true }],
  ["ui-board", "/projects/CP/board", "[data-card]"],
  // The demo's only scrum board with a sprint running belongs to a team, so
  // one picture answers both "what does scrum look like" and "a board per team".
  ["board-scrum", "/projects/CP/board", "[data-card]", { pick: "Platform board" }],
  ["plan", "/projects/CP/plan", "[data-plan-row]"],
  ["plan-quarters", "/projects/CP/plan?view=timeline", "[data-plan-row]", { zoom: "Quarters" }],
  ["calendar", "/projects/CP/calendar", "[data-calendar-item]"],
  ["audit-log", "/settings/audit", "[data-audit-row]"],
  ["hierarchy", "/projects/CP/hierarchy", "main li"],
  ["dashboard-software", "/projects/CP/dashboard", "[data-widget]"],
  ["dashboard-desk", "/projects/HELP/dashboard", "[data-widget]"],
  ["dashboard-shared", "/projects/CP/dashboard", "[data-widget]", { shared: true }],
  ["desk-queue", "/projects/HELP/queues", "main table"],
  // The catalog, not the list: Ada is an agent, so her own list of raised
  // requests is empty and photographs as an empty state.
  ["desk-portal", "/portal/new", "main"],
  ["teams", "/projects/CP/teams", "main"],
  ["git-repositories", "/projects/CP/repositories", "main"],
  ["project-workflows", "/projects/CP/workflows", "main"],
  ["workflow-settings", "/settings/workflows", "main h1"],
  ["workflow-schemes", "/settings/workflows/schemes", "[data-scheme-card]"],
  ["access-roles", "/settings/access", "[data-access-tab]"],
  ["project-templates", "/projects", "main table", { click: "New project" }],
  ["settings-profile", "/settings/profile", "#field-name"],
];

mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({
  executablePath: process.env.E2E_CHROME ?? "/usr/bin/chromium-browser",
  args: ["--no-sandbox", "--disable-dev-shm-usage", "--disable-crash-reporter", "--disable-crashpad", `--user-data-dir=/tmp/chrome-${Date.now()}`],
});
const page = await browser.newPage();
await page.setViewport({ width: 1280, height: 860, deviceScaleFactor: 1 });
page.setDefaultTimeout(20_000);

await page.goto(`${BASE}/login`, { waitUntil: "networkidle0" });
await page.type("#field-email", EMAIL);
await page.type("#field-password", PASSWORD);
await page.click('button[type="submit"]');
await page.waitForSelector('a[href="/settings/tokens"]');

// The demo's first issue with children and one with a repository, found rather than assumed.
async function firstKey(path, selector) {
  await page.goto(`${BASE}${path}`, { waitUntil: "networkidle0" });
  await page.waitForSelector(selector);
  return page.$eval(selector, (el) => el.getAttribute("data-issue-row") ?? el.textContent.trim());
}
const epicKey = await firstKey("/search?q=type%20%3D%20Epic%20ORDER%20BY%20created%20ASC", "[data-issue-row]");
const storyKey = await firstKey("/search?q=type%20%3D%20Story%20ORDER%20BY%20created%20ASC", "[data-issue-row]");
const deskKey = await firstKey("/projects/HELP", "[data-issue-row]");
// Three pictures of an issue, each waiting for the thing it is named after
// rather than for the page, so a name can never outlive what it shows.
SHOTS.push(
  ["ui-issue", `/issues/${storyKey}`, "[data-description]"],
  ["issue-hierarchy", `/issues/${epicKey}`, '[data-section="children"] ul li', { scrollTo: '[data-section="children"]' }],
  ["git-issue", `/issues/${storyKey}`, '[data-testid="development"] [data-pull-request]', { scrollTo: '[data-testid="development"]' }],
  ["desk-issue", `/issues/${deskKey}`, "[data-description]"],
);

// A skipped picture leaves the last run's file in place under the same name,
// which is how a picture starts showing something its name does not promise.
let skipped = 0;
for (const [name, path, waitFor, extra = {}] of SHOTS) {
  try {
    await page.setViewport({ width: extra.viewport ?? 1280, height: 860, deviceScaleFactor: 1 });
    await page.goto(`${BASE}${path}`, { waitUntil: "networkidle0" });
    await page.waitForSelector(waitFor);
    if (extra.beside) {
      // The first row's key cell opens the issue beside the list.
      await page.click("[data-issue-row] td:nth-child(2)");
      await page.waitForSelector("[data-issue-panel] [data-description]");
    }
    if (extra.click) {
      await page.evaluate((text) => [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === text)?.click(), extra.click);
    }
    if (extra.shared) {
      // A link is made through the API and the picture is of what it opens.
      const shared = await page.evaluate(async () => {
        const boards = await (await fetch("/api/v1/projects/CP/dashboards", { credentials: "include" })).json();
        const [board] = boards.dashboards ?? [];
        if (!board) throw new Error("the demo project CP has no dashboard to share");
        const made = await fetch(`/api/v1/dashboards/${board.id}/shares`, {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name: `Screenshot ${Date.now()}` }),
        });
        return (await made.json()).url;
      });
      // The link names the browser's address; in here the web container has its own.
      await page.goto(`${BASE}${new URL(shared).pathname}`, { waitUntil: "networkidle0" });
      await page.waitForSelector("[data-shared-dashboard] [data-widget]");
    }
    if (extra.pick) {
      await page.evaluate((text) => [...document.querySelectorAll('[role="group"][aria-label="Board"] button')].find((b) => b.textContent.trim().startsWith(text))?.click(), extra.pick);
    }
    if (extra.zoom) {
      await page.evaluate((text) => [...document.querySelectorAll('[role="group"][aria-label="Zoom"] button')].find((b) => b.textContent.trim() === text)?.click(), extra.zoom);
    }
    if (extra.scrollTo) {
      // Centred: the sticky header covers a section put at the top, and a
      // short page cannot scroll that far without ending in empty space.
      await page.$eval(extra.scrollTo, (el) => el.scrollIntoView({ block: "center" }));
    }
    await new Promise((r) => setTimeout(r, SETTLE_MS));
    await page.screenshot({ path: `${OUT}/${name}.png` });
    process.stdout.write(`  ${name}.png\n`);
  } catch (error) {
    skipped += 1;
    process.stdout.write(`  ${name}.png skipped: ${error.message.split("\n")[0]}\n`);
  }
}
await browser.close();
if (skipped > 0) {
  process.stdout.write(`${skipped} of ${SHOTS.length} pictures were not taken. Delete the stale files or fix the page before committing.\n`);
  process.exit(1);
}
