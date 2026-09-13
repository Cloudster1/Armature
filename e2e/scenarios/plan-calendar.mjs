// The plan's panes and teams, the calendar, and a board per sprint.

import { scenario } from "../runner.mjs";
import {
  addOnPlan,
  assignTeam,
  barRange,
  bodyText,
  commitIssue,
  createIssue,
  createMilestone,
  createProject,
  createSprint,
  createTeam,
  day,
  dragAcross,
  dragCardTo,
  estimateIssue,
  expect,
  goto,
  openPlanFilters,
  planGeometry,
  planRows,
  planSprint,
  scheduleIssue,
  selectByLabel,
  setTeamCapacity,
  signUp,
  textOf,
  WAIT,
  zoomPlan,
} from "../helpers.mjs";

scenario("the plan's two panes line up at every zoom, bands and flags included", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Lined up");

  // Enough on the calendar to draw every band: scheduled work, a dated
  // sprint, and a milestone due well after the last issue.
  const first = await createIssue(page, key, "first stretch");
  const second = await createIssue(page, key, "second stretch");
  await createIssue(page, key, "not scheduled");
  await scheduleIssue(page, first, day(1), day(8));
  await scheduleIssue(page, second, day(5), day(20));
  const sprint = await createSprint(page, key, "Sprint A");
  await planSprint(page, sprint, { from: day(0), to: day(13), capacity: 10 });
  await createMilestone(page, key, "Far off", day(50));

  await goto(page, `/projects/${key}/plan`);
  for (const zoom of ["Fit", "Weeks", "Months", "Quarters"]) {
    await zoomPlan(page, zoom);
    const g = await planGeometry(page);

    expect.equal(g.rows.length, 3, `${zoom}: every issue has a row on both sides`);
    for (const row of g.rows) {
      expect.truthy(Math.abs(row.side - row.calendar) < 0.5, `${zoom}: ${row.key} starts on the same pixel either side (${row.side} vs ${row.calendar})`);
    }
    expect.truthy(
      Math.abs(g.sidebarHeaderHeight - g.calendarHeaderHeight) < 0.5,
      `${zoom}: the sidebar header is as tall as the calendar's (${g.sidebarHeaderHeight} vs ${g.calendarHeaderHeight})`,
    );
    // A calendar narrower than the pane scrolls nothing; a wider one scrolls
    // exactly its own width, never something hanging out past its edge.
    const widest = Math.ceil(Math.max(g.canvasWidth, g.paneWidth));
    expect.truthy(g.scrollWidth <= widest, `${zoom}: nothing hangs out past the calendar (${g.scrollWidth} vs ${widest})`);
    // The calendar always reaches the right edge of its pane: a coarse zoom of
    // a short plan is not a strip beside a blank pane.
    expect.truthy(g.canvasWidth >= g.paneWidth - 1, `${zoom}: the calendar fills the pane (${g.canvasWidth} of ${g.paneWidth})`);
    expect.truthy(g.flag, `${zoom}: the milestone is on the calendar`);
    expect.truthy(g.flag.right <= g.flag.canvasRight + 0.5 && g.flag.left >= g.flag.canvasLeft - 0.5, `${zoom}: the flag is inside the calendar`);
  }

  // Fit means fit: the calendar is exactly as wide as the pane, no scrollbar.
  await zoomPlan(page, "Fit");
  const fit = await planGeometry(page);
  expect.truthy(fit.scrollWidth <= fit.paneWidth, `fitted calendar scrolls (${fit.scrollWidth} in ${fit.paneWidth})`);
  expect.truthy(Math.abs(fit.canvasWidth - fit.paneWidth) <= 1, `fitted calendar is ${fit.canvasWidth} wide in a ${fit.paneWidth} pane`);
});

// The load is read per calendar week, so the scenario schedules into the next
// whole one and asks the plan about that week by its Monday.
const DAY_MS = 24 * 60 * 60 * 1000;
function addDaysTo(date, days) {
  return new Date(date.getTime() + days * DAY_MS);
}
function isoDate(date) {
  return date.toISOString().slice(0, 10);
}
function nextMonday() {
  const today = new Date(Date.UTC(new Date().getUTCFullYear(), new Date().getUTCMonth(), new Date().getUTCDate()));
  const ahead = ((8 - today.getUTCDay()) % 7) || 7;
  return addDaysTo(today, ahead);
}

scenario("a team's weeks are measured against what it said it can take", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Loaded");
  await createTeam(page, key, "Alpha");
  await setTeamCapacity(page, key, "Alpha", 10);

  // Two issues for Alpha in one week add up to more than the week; a third is
  // nobody's, and counts beside the teams rather than against any of them.
  const big = await createIssue(page, key, "eight points for Alpha", "Task");
  const small = await createIssue(page, key, "six points for Alpha", "Task");
  const loose = await createIssue(page, key, "three points for nobody", "Task");
  const monday = nextMonday();
  for (const [issueKey, points, team] of [[big, 8, "Alpha"], [small, 6, "Alpha"], [loose, 3, null]]) {
    await estimateIssue(page, issueKey, points);
    if (team) await assignTeam(page, issueKey, team);
    await scheduleIssue(page, issueKey, isoDate(monday), isoDate(addDaysTo(monday, 4)));
  }

  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector('[data-plan-load-row="Alpha"]', { timeout: WAIT });
  expect.contains(await textOf(page, '[data-plan-load-row="Alpha"]'), "10 pts/week", "the team's capacity is on its row");
  expect.truthy(await page.$('[data-plan-load-row="Unassigned"]'), "the work nobody carries has a row");
  expect.truthy(await page.$('[data-plan-load-row="Total"]'), "and so does the whole project");

  const week = isoDate(monday);
  const alpha = await page.$eval(
    `[data-plan-load-cells="Alpha"] [data-plan-load-week="${week}"]`,
    (el) => ({ load: el.dataset.load, capacity: el.dataset.capacity, over: el.dataset.over }),
  );
  expect.equal(alpha.load, "14", "the two issues land in the week in full");
  expect.equal(alpha.capacity, "10", "against the capacity the team gave");
  expect.equal(alpha.over, "true", "which makes the week over");
  const total = await page.$eval(
    `[data-plan-load-cells="Total"] [data-plan-load-week="${week}"]`,
    (el) => el.dataset.load,
  );
  expect.equal(total, "17", "the total adds the loose work");
  expect.contains(await textOf(page, '[data-testid="plan-warnings"]'), "Alpha is loaded with 14 points", "and the plan says so, as a warning");

  // Narrowed to the team, the plan shows its work and its row and no other.
  await openPlanFilters(page);
  await selectByLabel(page, 'select[aria-label="Filter by team"]', "Alpha");
  await page.waitForFunction(() => document.querySelector('[data-plan-load-row="Total"]') === null, { timeout: WAIT });
  const rows = await planRows(page);
  expect.equal(rows.filter((r) => !r.startsWith("[")).length, 2, `Alpha's two issues remain: ${rows.join(", ")}`);
  expect.truthy(await page.$('[data-plan-load-row="Alpha"]'), "and Alpha's load row");

  // The two panes still line up with the load rows in place.
  await openPlanFilters(page);
  await selectByLabel(page, 'select[aria-label="Filter by team"]', "Any team");
  for (const zoom of ["Fit", "Quarters"]) {
    await zoomPlan(page, zoom);
    const geometry = await planGeometry(page);
    expect.equal(
      Math.round(geometry.sidebarHeaderHeight),
      Math.round(geometry.calendarHeaderHeight),
      `${zoom}: the headers agree with load rows below`,
    );
    for (const row of geometry.rows) {
      expect.equal(Math.round(row.side), Math.round(row.calendar), `${zoom}: ${row.key} lines up`);
    }
  }
});

scenario("an issue is added to a sprint from the plan's own row", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Filed");
  const existing = await createIssue(page, key, "already here", "Task");
  await createSprint(page, key, "Sprint A");
  await planSprint(page, "Sprint A", { from: "2026-09-07", to: "2026-09-18", capacity: 10 });
  await commitIssue(page, existing, "Sprint A");

  // The sprint's own add row files the ticket into the sprint, in one step.
  await goto(page, `/projects/${key}/plan?view=sprints`);
  await page.waitForSelector('[data-plan-group="Sprint A"]', { timeout: WAIT });
  await addOnPlan(page, "Sprint A", "made in the sprint");
  await page.waitForFunction(
    () => document.querySelectorAll('[data-testid="plan-sidebar"] [data-plan-row]').length === 2,
    { timeout: WAIT },
  );
  const rows = await planRows(page);
  expect.equal(rows[0], "[Sprint A]", "the sprint group comes first");
  expect.equal(rows.indexOf("[Backlog]"), 3, `the new issue sits under Sprint A, not the backlog: ${rows.join(", ")}`);
  const made = rows.slice(1, 3).find((k) => k !== existing);
  await goto(page, `/issues/${made}`);
  await page.waitForFunction(
    () => document.querySelector("#issue-sprint")?.selectedOptions[0]?.textContent.includes("Sprint A"),
    { timeout: WAIT },
  );
  expect.contains(await bodyText(page), "made in the sprint", "the ticket is real");

  // The management view's row files a ticket that no sprint has taken on.
  await goto(page, `/projects/${key}/plan`);
  await addOnPlan(page, "plan", "made on the plan");
  await page.waitForFunction(
    () => document.querySelectorAll('[data-testid="plan-sidebar"] [data-plan-row]').length === 3,
    { timeout: WAIT },
  );
  await goto(page, `/projects/${key}/plan?view=sprints`);
  await page.waitForSelector('[data-plan-group="Backlog"]', { timeout: WAIT });
  const bySprint = await planRows(page);
  expect.equal(bySprint.indexOf("[Backlog]"), 3, `the management view's ticket landed in the backlog: ${bySprint.join(", ")}`);
});

scenario("dragging across empty calendar makes a ticket with those dates", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Drawn");
  const unscheduled = await createIssue(page, key, "waiting for dates", "Task");

  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector('[data-plan-add-row="plan"]', { timeout: WAIT });

  // Escape lets a drag go without making anything.
  await dragAcross(page, '[data-plan-add-row="plan"]', 2, 6);
  await page.waitForSelector("[data-plan-draft-summary]", { timeout: WAIT });
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => document.querySelector("[data-plan-draft-summary]") === null, { timeout: WAIT });
  expect.equal((await planRows(page)).length, 1, "nothing was made");

  // Enter makes the ticket with the days dragged.
  await dragAcross(page, '[data-plan-add-row="plan"]', 2, 6);
  await page.waitForSelector("[data-plan-draft-summary]", { timeout: WAIT });
  await page.type("[data-plan-draft-summary]", "drawn on the plan");
  await page.keyboard.press("Enter");
  await page.waitForFunction(
    () => document.querySelectorAll('[data-testid="plan-sidebar"] [data-plan-row]').length === 2,
    { timeout: WAIT },
  );
  const drawn = (await planRows(page)).find((k) => k !== unscheduled);
  const range = await barRange(page, drawn);
  const today = new Date(Date.UTC(new Date().getUTCFullYear(), new Date().getUTCMonth(), new Date().getUTCDate()));
  expect.equal(range.start, isoDate(addDaysTo(today, 2)), "it starts where the drag began");
  expect.equal(range.due, isoDate(addDaysTo(today, 6)), "and ends where it let go");
  expect.contains(await bodyText(page), "drawn on the plan", "with the summary typed");

  // A drag on an unscheduled issue's own row schedules that issue, no box asked.
  await dragAcross(page, `[data-plan-bar-row="${unscheduled}"]`, 1, 3);
  await page.waitForSelector(`[data-plan-bar="${unscheduled}"]`, { timeout: WAIT });
  const scheduled = await barRange(page, unscheduled);
  expect.equal(scheduled.start, isoDate(addDaysTo(today, 1)), "the issue starts where the drag began");
  expect.equal(scheduled.due, isoDate(addDaysTo(today, 3)), "and ends where it let go");
});

scenario("every sprint has a board of its own, running or not", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Tracked");
  const committed = await createIssue(page, key, "committed to the next sprint");
  await createIssue(page, key, "still in the backlog");
  const sprint = await createSprint(page, key, "Sprint N");
  await commitIssue(page, committed, "Sprint N");

  // From the sprint's card, before it has started.
  await goto(page, `/projects/${key}/sprints`);
  await page.waitForSelector('[data-sprint-board="Sprint N"]', { timeout: WAIT });
  await page.click('[data-sprint-board="Sprint N"]');
  await page.waitForFunction(() => location.pathname.endsWith("/board") && document.querySelector("[data-card]"), { timeout: WAIT });
  const cards = await page.$$eval("[data-card]", (els) => els.map((el) => el.getAttribute("data-card")));
  expect.equal(cards.join(","), committed, "the board shows the sprint's work and nothing else");
  expect.contains(await bodyText(page), sprint, "and is headed by the sprint");

  // A card on it moves through the workflow like any other.
  await dragCardTo(page, committed, "In Progress");
  await goto(page, `/issues/${committed}`);
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });

  // The plan's sprint view links to the same board.
  await goto(page, `/projects/${key}/plan?view=sprints`);
  await page.waitForSelector('[data-plan-group="Sprint N"] [data-sprint-board="Sprint N"]', { timeout: WAIT });
});
