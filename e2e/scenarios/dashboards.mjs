// Dashboards, their widgets, templates, shares and the PDF.

import { scenario } from "../runner.mjs";
import {
  addWidget,
  assignMilestone,
  bodyText,
  cardsIn,
  chartSettings,
  clickButton,
  coinStatus,
  confirm,
  createDashboard,
  createIssue,
  createMilestone,
  createProject,
  dashboardFixture,
  day,
  dragCardTo,
  dragConnect,
  expect,
  expectCounted,
  goto,
  milestoneDashboard,
  milestoneProgress,
  openWorkflowDesign,
  reload,
  replaceValue,
  revokeShare,
  saveTemplate,
  selectByLabel,
  shareDashboard,
  signIn,
  signOut,
  signUp,
  textOf,
  WAIT,
  waitForApp,
  waitForButtonToGo,
} from "../helpers.mjs";

scenario("a status coined in the designer is drawn at once and the board grows a lane for it", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Reviewed");
  const issueKey = await createIssue(page, key, "something to review", "Task");

  // The organization's own workflow gains a status that did not exist a
  // moment ago, from the same panel that adds existing ones.
  await openWorkflowDesign(page, "Default software workflow");
  await coinStatus(page, { name: "Reviewing", category: "In progress" });
  await dragConnect(page, "To Do", "Reviewing");
  await replaceValue(page, "#field-transition-name", "Review");
  await clickButton(page, "Save workflow");
  await waitForButtonToGo(page, "Save workflow");

  // The project's board followed: a lane for the new status, and a card can
  // be moved into it.
  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector('[data-swimlane="Reviewing"]', { timeout: WAIT });
  const lanes = await page.$$eval("[data-swimlane]", (els) => els.map((el) => el.getAttribute("data-swimlane")));
  expect.equal(lanes[lanes.length - 1], "Reviewing", `the new lane is last: ${lanes.join(", ")}`);
  await dragCardTo(page, issueKey, "Reviewing");
  await page.waitForFunction(
    (k) => document.querySelector(`[data-swimlane="Reviewing"] [data-card="${k}"]`) !== null,
    { timeout: WAIT },
    issueKey,
  );
  const inReview = await cardsIn(page, "Reviewing");
  expect.truthy(inReview.includes(issueKey), `the card sits in the new lane: ${inReview.join(", ")}`);

  // And every other workflow can be built from it from now on.
  await goto(page, "/settings/workflows");
  await clickButton(page, "New workflow");
  await page.waitForSelector("#field-add-status", { timeout: WAIT });
  const offered = await page.$$eval("#field-add-status option", (els) => els.map((el) => el.textContent.trim()));
  expect.truthy(offered.includes("Reviewing"), `the picker offers the coined status: ${offered.join(", ")}`);
});

scenario("a project's dashboard counts its work and can be rearranged", async ({ page }) => {
  const who = await signUp(page);
  const key = await createProject(page, "Measured");
  const started = await createIssue(page, key, "the one under way", "Task");
  await createIssue(page, key, "the one still waiting", "Task");

  await goto(page, `/issues/${started}`);
  await clickButton(page, "Start progress");
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });

  // The dashboard the project was born with counts both.
  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForSelector('[data-widget="status_breakdown"] [data-bar="In Progress"]', { timeout: WAIT });
  const status = await page.$eval('[data-widget="status_breakdown"]', (el) => el.innerText);
  expect.contains(status, "2 in all", "the status widget counts both issues");
  const bars = await page.$$eval('[data-widget="status_breakdown"] [data-bar]', (els) =>
    els.map((el) => `${el.getAttribute("data-bar")}=${el.innerText.trim().split("\n").pop()}`),
  );
  expect.truthy(bars.includes("To Do=1") && bars.includes("In Progress=1"), `one each: ${bars.join(", ")}`);

  await page.waitForSelector('[data-widget="workload"] [data-load]', { timeout: WAIT });
  const load = await page.$eval(`[data-widget="workload"] [data-load="${who.name}"]`, (el) => el.innerText);
  expect.contains(load, "1", "starting progress put the issue on the person who started it");

  // Arranging: a widget is added, and one is taken away.
  await clickButton(page, "Arrange");
  await page.waitForSelector('[data-add-widget="cycle_time"]', { timeout: WAIT });
  await page.click('[data-add-widget="cycle_time"]');
  await page.waitForSelector('[data-widget="cycle_time"]', { timeout: WAIT });
  await page.evaluate(() => {
    const widget = document.querySelector('[data-widget="velocity"]');
    [...widget.querySelectorAll("button")].find((b) => b.textContent.trim() === "Remove")?.click();
  });
  await page.waitForFunction(() => document.querySelector('[data-widget="velocity"]') === null, { timeout: WAIT });
  await clickButton(page, "Done arranging");

  // And the arrangement is what the project keeps.
  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForSelector('[data-widget="cycle_time"]', { timeout: WAIT });
  expect.truthy(!(await bodyText(page)).includes("Velocity"), "the removed widget stays removed");
});

scenario("a dashboard's filter tile narrows every widget that counts issues", async ({ page }) => {
  const { key } = await dashboardFixture(page, "Narrowed");

  // The tile narrows the widgets that count issues, and says so on the ones it cannot.
  await addWidget(page, "filter");
  await page.click('[data-filter-type="Bug"]');
  await expectCounted(page, 1);
  expect.contains(page.url(), "types=Bug", "the filter is in the address");
  expect.truthy(!page.url().includes("%22"), "and reads as plain words, not quoted values");
  await page.waitForSelector('[data-widget="velocity"] [data-not-narrowed]', { timeout: WAIT });

  // The address is the filter: a reload keeps it.
  await reload(page);
  await expectCounted(page, 1);
  expect.equal(await page.$eval('[data-filter-type="Bug"]', (el) => el.getAttribute("aria-pressed")), "true", "the chip remembers");

  // A query the language cannot read is refused with a caret, not a blank widget.
  await page.type("#field-narrow-with-a-query", "priority = ");
  await page.keyboard.press("Enter");
  await page.waitForSelector("[data-filter-tile] [data-query-caret]", { timeout: WAIT });

  await clickButton(page, "Clear filter");
  await expectCounted(page, 3);

  // The defaults belong to the tile: saved while arranging, they hold on a clean address.
  await clickButton(page, "Arrange");
  await page.waitForSelector('[data-action="save-filter"]', { timeout: WAIT });
  await page.click('[data-filter-type="Task"]');
  await expectCounted(page, 2);
  await page.click('[data-action="save-filter"]');
  await page.waitForFunction(() => document.querySelector('[data-action="save-filter"]')?.disabled, { timeout: WAIT });
  await goto(page, `/projects/${key}/dashboard`);
  await expectCounted(page, 2);
});

scenario("a chart widget groups the project's issues by type and draws them", async ({ page }) => {
  const { key } = await dashboardFixture(page, "Charted");
  await addWidget(page, "chart");

  // Bars by type: the figure rides each bar and the table says the same.
  await chartSettings(page, { groupBy: "Issue type" });
  await page.waitForSelector('[data-widget="chart"] [data-chart="bar"] [data-chart-group="Bug"][data-value="1"]', { timeout: WAIT });
  await page.waitForSelector('[data-widget="chart"] [data-chart-group="Task"][data-value="2"]', { timeout: WAIT });
  expect.contains(await page.$eval('[data-widget="chart"]', (el) => el.innerText), "3 issues in all", "the total is written out");

  // A stack splits each bar by a second field; a donut draws shares; a line draws time.
  await chartSettings(page, { shape: "Stack", splitBy: "Status category" });
  await page.waitForSelector('[data-widget="chart"] [data-chart="stacked"] [data-chart-part]', { timeout: WAIT });
  await chartSettings(page, { shape: "Donut" });
  await page.waitForSelector('[data-widget="chart"] [data-chart="donut"] path[data-chart-group="Task"]', { timeout: WAIT });
  await chartSettings(page, { shape: "Line", series: "Created" });
  await page.waitForSelector('[data-widget="chart"] [data-testid="line-chart"] [data-series]', { timeout: WAIT });

  // The dashboard's filter narrows a chart like anything else.
  await chartSettings(page, { shape: "Bars" });
  await page.waitForSelector('[data-widget="chart"] [data-chart="bar"]', { timeout: WAIT });
  await addWidget(page, "filter");
  await page.click('[data-filter-type="Bug"]');
  await page.waitForFunction(
    () => document.querySelector('[data-widget="chart"] [data-chart-group="Bug"]') && !document.querySelector('[data-widget="chart"] [data-chart-group="Task"]'),
    { timeout: WAIT },
  );
});

scenario("a milestones widget says what the milestone card says", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Tracked");
  await createMilestone(page, key, "Release 1", day(30));
  const first = await createIssue(page, key, "the first step", "Task");
  const second = await createIssue(page, key, "the bug on the way", "Bug");
  await assignMilestone(page, first, "Release 1");
  await assignMilestone(page, second, "Release 1");
  await goto(page, `/issues/${first}`);
  await clickButton(page, "Close");
  await page.waitForFunction(() => document.body.innerText.includes("Done"), { timeout: WAIT });
  const card = await milestoneProgress(page, key, "Release 1");

  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForSelector('[data-widget="status_breakdown"]', { timeout: WAIT });
  await addWidget(page, "milestones");
  await page.waitForSelector('[data-milestone-widget="Release 1"]', { timeout: WAIT });
  const widget = await page.$eval('[data-milestone-widget="Release 1"]', (el) => el.innerText);
  expect.contains(widget, card, "the widget and the card agree");
  expect.contains(widget, "days left", "and the widget says when it is due");

  // One milestone in full, chosen in the widget's settings.
  await selectByLabel(page, 'select[aria-label="Milestone for Milestones"]', "Release 1");
  await page.waitForSelector('[data-widget="milestones"] [data-milestone-single]', { timeout: WAIT });
  expect.contains(await page.$eval('[data-widget="milestones"]', (el) => el.innerText), "50%", "half of it is done");

  // The filter tile narrows the rest of the dashboard to the milestone.
  await addWidget(page, "filter");
  await selectByLabel(page, "#field-milestone", "Release 1");
  await expectCounted(page, 2);
  expect.contains(page.url(), "milestone=Release", "the milestone is in the address");
});

scenario("a dashboard is saved as a template and another project starts from it", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Origin");
  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForSelector('[data-widget="status_breakdown"]', { timeout: WAIT });
  await addWidget(page, "chart");

  // Saved while arranging, under a name of its own.
  await saveTemplate(page, "Mondays");

  // Another project starts from it and gets the same widgets.
  const other = await createProject(page, "Elsewhere");
  await createDashboard(page, other, "From Mondays", "Mondays");
  await page.waitForSelector('[data-widget="chart"]', { timeout: WAIT });
  expect.equal((await page.$$('[data-widget="status_breakdown"]')).length, 1, "the status widget came along");

  // Removing the template takes it from the chooser and nothing from the dashboard.
  await clickButton(page, "New dashboard");
  await page.waitForSelector('[data-action="remove-template"]', { timeout: WAIT });
  await page.click('[data-action="remove-template"]');
  await confirm(page);
  await page.waitForFunction(
    () => ![...document.querySelectorAll("[data-dashboard-template]")].some((el) => el.innerText.includes("Mondays")),
    { timeout: WAIT },
  );
  await clickButton(page, "Cancel");
  expect.truthy(await page.$('[data-widget="chart"]'), "the dashboard made from it is untouched");
});

scenario("a milestone gets a dashboard of its own", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Aimed");
  await createMilestone(page, key, "Release 1", day(20));
  const counted = await createIssue(page, key, "counts towards the release", "Task");
  await createIssue(page, key, "counts towards nothing", "Task");
  await assignMilestone(page, counted, "Release 1");

  await milestoneDashboard(page, key, "Release 1");
  expect.contains(page.url(), "/dashboard?", "the page went to the new dashboard");
  expect.contains(await page.$eval('[data-widget="filter"]', (el) => el.innerText), "Release 1", "the filter is pinned to the milestone");
  await page.waitForSelector('[data-milestone-widget="Release 1"]', { timeout: WAIT });
  await page.waitForFunction(() => document.querySelector('[data-widget="chart"]')?.innerText.includes("1 issues in all"), { timeout: WAIT });
  await page.waitForSelector('[data-dashboard="Release 1"][aria-pressed="true"]', { timeout: WAIT });

  // The address names the dashboard, so a reload lands on the same one.
  await reload(page);
  await page.waitForSelector('[data-dashboard="Release 1"][aria-pressed="true"]', { timeout: WAIT });
});

scenario("a dashboard is shared by a link that needs no sign-in, until it is revoked", async ({ page }) => {
  const { who: owner, key } = await dashboardFixture(page, "Lobby");
  await addWidget(page, "filter");
  await page.click('[data-filter-type="Bug"]');
  await expectCounted(page, 1);
  const url = await shareDashboard(page, "Lobby screen");
  const path = new URL(url).pathname;
  expect.contains(path, "/shared/", "the link opens the shared page");

  // Signed out, the link still opens the dashboard, narrowed as it was shared.
  await signOut(page);
  await goto(page, path);
  await page.waitForSelector('[data-shared-dashboard="Overview"] [data-widget="status_breakdown"]', { timeout: WAIT });
  await expectCounted(page, 1);
  expect.contains(await textOf(page, "[data-shared-query]"), "type = ", "the page says what the link froze");
  expect.equal(await page.$('[data-widget="filter"]'), null, "the filter tile is not offered to a visitor");
  expect.truthy(await page.$('[data-widget="velocity"] [data-not-narrowed]'), "and the sprint widget says it is not narrowed");
  expect.equal(await page.$('a[href="/settings/tokens"]'), null, "no shell around it");

  // Revoked, the same link says so.
  await signIn(page, owner.email);
  await waitForApp(page);
  await goto(page, `/projects/${key}/dashboard`);
  await revokeShare(page, "Lobby screen");
  await signOut(page);
  await goto(page, path);
  await page.waitForSelector("[data-share-gone]", { timeout: WAIT });
});

scenario("a dashboard is exported as a PDF, signed in and from a shared link", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Printed");
  await createIssue(page, key, "something to count", "Task");
  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForSelector('[data-action="export-pdf"]', { timeout: WAIT });

  // The export link answers with a file the browser would save.
  const fetched = await page.evaluate(async () => {
    const href = document.querySelector('[data-action="export-pdf"]').getAttribute("href");
    const response = await fetch(href, { credentials: "include" });
    const bytes = new Uint8Array(await response.arrayBuffer());
    return {
      status: response.status,
      type: response.headers.get("content-type"),
      disposition: response.headers.get("content-disposition"),
      head: String.fromCharCode(...bytes.slice(0, 5)),
      size: bytes.length,
    };
  });
  expect.equal(fetched.status, 200, `the export answered ${fetched.status}`);
  expect.truthy((fetched.type ?? "").startsWith("application/pdf"), `content type is ${fetched.type}`);
  expect.equal(fetched.head, "%PDF-", "the file is a PDF");
  expect.contains(fetched.disposition ?? "", "attachment", "and it downloads rather than opens");
  expect.truthy(fetched.size > 1000, "and it has pages in it");

  // A shared link prints too, for whoever holds it.
  const url = await shareDashboard(page, "Wall");
  await signOut(page);
  await goto(page, new URL(url).pathname);
  await page.waitForSelector('[data-action="download-pdf"]', { timeout: WAIT });
  const shared = await page.evaluate(async () => {
    const href = document.querySelector('[data-action="download-pdf"]').getAttribute("href");
    const response = await fetch(href);
    const bytes = new Uint8Array(await response.arrayBuffer());
    return { status: response.status, head: String.fromCharCode(...bytes.slice(0, 5)) };
  });
  expect.equal(shared.status, 200, `the shared export answered ${shared.status}`);
  expect.equal(shared.head, "%PDF-", "the shared file is a PDF");
});
