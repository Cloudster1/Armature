// The organization's audit log, calendar, status updates and fields.

import { scenario, serialScenario } from "../runner.mjs";
import {
  acceptInvite,
  auditRowsFor,
  calendarDaysOf,
  clickButton,
  confirm,
  createIssue,
  createProject,
  createToken,
  day,
  dragCalendarItem,
  eventually,
  expect,
  fill,
  goto,
  grantRole,
  inviteMember,
  openProjectRow,
  postStatus,
  removeOrgFields,
  scheduleIssue,
  selectByLabel,
  signIn,
  signOut,
  signUp,
  textOf,
  unique,
  uniqueName,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("the audit log says who granted a role", async ({ page }) => {
  const admin = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Audited", { visit: false });
  const who = await inviteMember(page, "newcomer");
  await acceptInvite(page, who, { visit: false });
  await signOut(page, { visit: false });
  await signIn(page, admin.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await grantRole(page, { who: who.name, role: "Project administrator", projectKey: key });
  const nightly = uniqueName("Nightly build");
  await createToken(page, nightly);

  // The grant reached the log through the worker, naming who granted it; the
  // token was written by the act itself. Everybody's acts share the log, so
  // the rows are found by who did them.
  await goto(page, "/admin/audit");
  await eventually(page, '[data-audit-row="role.granted"]');
  let grants = [];
  for (const until = Date.now() + WAIT; Date.now() < until; ) {
    const rows = await auditRowsFor(page, "role.granted");
    expect.truthy(
      rows.every((r) => r.action === "role.granted"),
      "narrowing to an action shows only it",
    );
    grants = rows.filter((r) => r.text.includes(admin.name));
    if (grants.length) break;
  }
  expect.truthy(grants.length > 0, "the grant is in the log, naming who granted");
  expect.contains(grants[0].text, "project_administrator", "and what");
  const tokens = await auditRowsFor(page, "token.created");
  expect.truthy(
    tokens.some((r) => r.text.includes(nightly)),
    "the token is in the log by name",
  );
  expect.truthy(await page.$('[data-action="export-audit"]'), "and the log can be exported");
});

scenario("a due date is dragged to another day on the calendar", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Dated", { visit: false });
  const issueKey = await createIssue(page, key, "Paint the fence", "Task");
  // Inside this month, whatever day it is: the second week to the third.
  const now = new Date();
  const at = (d) => `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
  await scheduleIssue(page, issueKey, at(8), at(10));

  await goto(page, `/projects/${key}/calendar`);
  await page.waitForSelector(`[data-day="${at(8)}"] [data-calendar-item="${issueKey}"]`, { timeout: WAIT });
  expect.equal((await calendarDaysOf(page, issueKey)).join(","), [at(8), at(9), at(10)].join(","), "the bar lies on its three days");

  await dragCalendarItem(page, issueKey, at(9), at(16));
  await page.waitForSelector(`[data-day="${at(15)}"] [data-calendar-item="${issueKey}"]`, { timeout: WAIT });
  expect.equal((await calendarDaysOf(page, issueKey)).join(","), [at(15), at(16), at(17)].join(","), "both ends moved by the days the pointer travelled");

  expect.equal(await page.$("[data-issue-panel]"), null, "the drag opened nothing");

  await goto(page, `/issues/${issueKey}`);
  await page.waitForFunction((want) => document.querySelector("#issue-due")?.value === want, { timeout: WAIT }, at(17));
  expect.equal(await page.$eval("#issue-start", (el) => el.value), at(15), "the issue itself carries the new dates");
});

// Reported as entries that wear a pointer and do nothing when clicked.
scenario("an issue on the calendar opens beside it on a click", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Clicked", { visit: false });
  const first = await createIssue(page, key, "Paint the fence", "Task");
  const second = await createIssue(page, key, "Mow the lawn", "Task");
  const now = new Date();
  const at = (d) => `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
  await scheduleIssue(page, first, at(8), at(8));
  await scheduleIssue(page, second, at(12), at(12));

  await goto(page, `/projects/${key}/calendar`);
  await page.waitForSelector(`[data-calendar-item="${first}"] a`, { timeout: WAIT });
  await page.click(`[data-calendar-item="${first}"] a`);
  await page.waitForSelector(`[data-issue-panel="${first}"]`, { timeout: WAIT });
  expect.equal(new URL(page.url()).pathname, `/projects/${key}/calendar`, "the calendar stays where it was");
  expect.equal(await calendarDaysOf(page, first).then((days) => days.join(",")), at(8), "and the click moved nothing");

  // The drawer walks the month's issues in the order the calendar has them.
  await page.click('[data-action="panel-next"]');
  await page.waitForSelector(`[data-issue-panel="${second}"]`, { timeout: WAIT });
});

scenario("a project posts a status update and the projects table shows it", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Reported", { visit: false });
  await openProjectRow(page, key);
  expect.contains(await textOf(page, `[data-project="${key}"]`), "Not said", "a fresh project has said nothing");

  await postStatus(page, key, "at_risk", "The vendor is late; the first half ships on time.");
  expect.contains(await textOf(page, "[data-status-strip]"), "the first half ships on time", "the strip carries the note");
  await postStatus(page, key, "on_track", "The vendor delivered.");

  await openProjectRow(page, key);
  await page.waitForSelector(`[data-project="${key}"] [data-project-status="on_track"]`, { timeout: WAIT });
  await goto(page, `/projects/${key}`);
  await page.waitForSelector('[data-action="status-history"]', { timeout: WAIT });
  await page.click('[data-action="status-history"]');
  await page.waitForSelector('[data-status-history] [data-status-update="at_risk"]', { timeout: WAIT });
  const history = await page.$$eval("[data-status-history] [data-status-update]", (els) => els.map((el) => el.getAttribute("data-status-update")));
  expect.equal(history.join(","), "on_track,at_risk", "the history has both, newest first");
});

// Serial: a field of the organization's is on every project there is.
serialScenario("a field defined for all projects is on every project", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const first = await createProject(page, "First", { visit: false });
  const second = await createProject(page, "Second", { visit: false });
  const costCentre = uniqueName("Cost centre");
  const region = uniqueName("Region");

  try {
    await goto(page, "/admin/fields");
    await clickButton(page, "New field");
    await fill(page, "Field name", costCentre);
    await selectByLabel(page, "#field-kind", "Text");
    await clickButton(page, "Create field");
    await page.waitForSelector(`[data-field="${costCentre}"][data-field-scope="org"]`, { timeout: WAIT });

    for (const key of [first, second]) {
      await goto(page, `/projects/${key}/settings/fields`);
      await page.waitForSelector(`[data-field="${costCentre}"][data-field-scope="org"]`, { timeout: WAIT });
    }

    // A project's own field is promoted from its page and joins them.
    await clickButton(page, "New field");
    await fill(page, "Field name", region);
    await selectByLabel(page, "#field-kind", "Text");
    await clickButton(page, "Create field");
    await page.waitForSelector(`[data-field="${region}"][data-field-scope="project"]`, { timeout: WAIT });
    await page.click(`[data-field="${region}"] [data-action="promote-field"]`);
    await confirm(page);
    await page.waitForSelector(`[data-field="${region}"][data-field-scope="org"]`, { timeout: WAIT });
    await goto(page, `/projects/${first}/settings/fields`);
    await page.waitForSelector(`[data-field="${region}"][data-field-scope="org"]`, { timeout: WAIT });
  } finally {
    await removeOrgFields([costCentre, region]);
  }
});
