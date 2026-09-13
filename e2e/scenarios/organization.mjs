// The organization's audit log, calendar, status updates and fields.

import { scenario } from "../runner.mjs";
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
  postStatus,
  scheduleIssue,
  selectByLabel,
  signIn,
  signOut,
  signUp,
  textOf,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("the audit log says who granted a role", async ({ page }) => {
  const admin = await signUp(page);
  const key = await createProject(page, "Audited");
  const who = await inviteMember(page, "newcomer");
  await acceptInvite(page, who);
  await signOut(page);
  await signIn(page, admin.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await grantRole(page, { who: who.name, role: "Project administrator", projectKey: key });
  await createToken(page, "Nightly build");

  // The grant reached the log through the worker, naming who granted it; the
  // token was written by the act itself.
  await goto(page, "/settings/audit");
  const grants = await eventually(page, '[data-audit-row="role.granted"]');
  expect.truthy(grants, "the grant is in the log");
  const rows = await auditRowsFor(page, "role.granted");
  expect.truthy(rows.every((r) => r.action === "role.granted"), "narrowing to an action shows only it");
  expect.contains(rows[0].text, admin.name, "and names who granted");
  expect.contains(rows[0].text, "project_administrator", "and what");
  const tokens = await auditRowsFor(page, "token.created");
  expect.contains(tokens[0].text, "Nightly build", "the token is in the log by name");
  expect.truthy(await page.$('[data-action="export-audit"]'), "and the log can be exported");
});

scenario("a due date is dragged to another day on the calendar", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Dated");
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

  await goto(page, `/issues/${issueKey}`);
  await page.waitForFunction((want) => document.querySelector("#issue-due")?.value === want, { timeout: WAIT }, at(17));
  expect.equal(await page.$eval("#issue-start", (el) => el.value), at(15), "the issue itself carries the new dates");
});

scenario("a project posts a status update and the projects table shows it", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Reported");
  await goto(page, "/projects");
  await page.waitForSelector(`[data-project="${key}"]`, { timeout: WAIT });
  expect.contains(await textOf(page, `[data-project="${key}"]`), "Not said", "a fresh project has said nothing");

  await postStatus(page, key, "at_risk", "The vendor is late; the first half ships on time.");
  expect.contains(await textOf(page, "[data-status-strip]"), "the first half ships on time", "the strip carries the note");
  await postStatus(page, key, "on_track", "The vendor delivered.");

  await goto(page, "/projects");
  await page.waitForSelector(`[data-project="${key}"] [data-project-status="on_track"]`, { timeout: WAIT });
  await goto(page, `/projects/${key}`);
  await page.waitForSelector('[data-action="status-history"]', { timeout: WAIT });
  await page.click('[data-action="status-history"]');
  await page.waitForSelector('[data-status-history] [data-status-update="at_risk"]', { timeout: WAIT });
  const history = await page.$$eval("[data-status-history] [data-status-update]", (els) => els.map((el) => el.getAttribute("data-status-update")));
  expect.equal(history.join(","), "on_track,at_risk", "the history has both, newest first");
});

scenario("a field defined for the organization is on every project", async ({ page }) => {
  await signUp(page);
  const first = await createProject(page, "First");
  const second = await createProject(page, "Second");

  await goto(page, "/settings/fields");
  await clickButton(page, "New field");
  await fill(page, "Field name", "Cost centre");
  await selectByLabel(page, "#field-kind", "Text");
  await clickButton(page, "Create field");
  await page.waitForSelector('[data-field="Cost centre"][data-field-scope="org"]', { timeout: WAIT });

  for (const key of [first, second]) {
    await goto(page, `/projects/${key}/fields`);
    await page.waitForSelector('[data-field="Cost centre"][data-field-scope="org"]', { timeout: WAIT });
  }

  // A project's own field is promoted from its page and joins them.
  await clickButton(page, "New field");
  await fill(page, "Field name", "Region");
  await selectByLabel(page, "#field-kind", "Text");
  await clickButton(page, "Create field");
  await page.waitForSelector('[data-field="Region"][data-field-scope="project"]', { timeout: WAIT });
  await page.click('[data-field="Region"] [data-action="promote-field"]');
  await confirm(page);
  await page.waitForSelector('[data-field="Region"][data-field-scope="org"]', { timeout: WAIT });
  await goto(page, `/projects/${first}/fields`);
  await page.waitForSelector('[data-field="Region"][data-field-scope="org"]', { timeout: WAIT });
});
