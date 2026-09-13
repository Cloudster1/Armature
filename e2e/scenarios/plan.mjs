// Scheduling work on the plan.

import { scenario } from "../runner.mjs";
import {
  addChild,
  barRange,
  bodyText,
  createIssue,
  createProject,
  dragBar,
  expect,
  goto,
  scheduleIssue,
  setDate,
  signUp,
  WAIT,
} from "../helpers.mjs";

scenario("an issue can be scheduled and appears on the plan", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Scheduling");
  const story = await createIssue(page, key, "Add password reset by email", "Story");

  await scheduleIssue(page, story, "2026-03-10", "2026-03-20");

  await goto(page, `/projects/${key}/plan`);
  const range = await barRange(page, story);
  expect.equal(range.start, "2026-03-10", "the bar starts where the issue does");
  expect.equal(range.due, "2026-03-20", "the bar ends where the issue does");
});

scenario("dragging a bar moves the work and keeps its length", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Dragging dates");
  const story = await createIssue(page, key, "Cache the account page", "Story");
  await scheduleIssue(page, story, "2026-03-10", "2026-03-20");

  await goto(page, `/projects/${key}/plan`);
  await dragBar(page, story, 3);
  await page.waitForFunction(
    (k) => document.querySelector(`[data-plan-bar="${k}"]`)?.dataset.planStart?.startsWith("2026-03-13"),
    { timeout: WAIT },
    story,
  );

  const range = await barRange(page, story);
  expect.equal(range.start, "2026-03-13", "the start moved three days");
  expect.equal(range.due, "2026-03-23", "and the end moved with it");
});

scenario("dragging a bar's edge changes how long the work takes", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Resizing");
  const story = await createIssue(page, key, "Compress the bundle", "Story");
  await scheduleIssue(page, story, "2026-03-10", "2026-03-20");

  await goto(page, `/projects/${key}/plan`);
  await dragBar(page, story, 4, "end");
  await page.waitForFunction(
    (k) => document.querySelector(`[data-plan-bar="${k}"]`)?.dataset.planDue?.startsWith("2026-03-24"),
    { timeout: WAIT },
    story,
  );

  const range = await barRange(page, story);
  expect.equal(range.start, "2026-03-10", "the start stayed put");
  expect.equal(range.due, "2026-03-24", "the end moved out four days");
});

scenario("an epic with no dates of its own spans its children", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Spanning");
  const epic = await createIssue(page, key, "Password and recovery", "Epic");
  const first = await addChild(page, epic, "Design the reset email", "Story");
  const second = await addChild(page, epic, "Expire reset tokens", "Story");

  await scheduleIssue(page, first, "2026-03-10", "2026-03-14");
  await scheduleIssue(page, second, "2026-03-18", "2026-03-27");

  await goto(page, `/projects/${key}/plan`);
  const range = await barRange(page, epic);
  expect.equal(range.start, "2026-03-10", "the epic starts with its earliest child");
  expect.equal(range.due, "2026-03-27", "and ends with its latest");
});

scenario("a dependency scheduled too early is reported, not refused", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Blocking");
  const blocker = await createIssue(page, key, "Build the token store", "Story");
  const blocked = await createIssue(page, key, "Send the reset email", "Story");

  await scheduleIssue(page, blocker, "2026-03-10", "2026-03-20");
  await scheduleIssue(page, blocked, "2026-03-15", "2026-03-25");

  // The link is made through the API the client uses; the plan reads it back.
  await page.evaluate(
    async (from, to) => {
      const response = await fetch(`/api/v1/issues/${from}/links`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type: "Blocks", targetKey: to }),
      });
      if (!response.ok) throw new Error(`link failed: ${response.status}`);
    },
    blocker,
    blocked,
  );

  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector('[data-testid="plan-warnings"]', { timeout: WAIT });

  const warnings = await page.$eval('[data-testid="plan-warnings"]', (el) => el.innerText);
  expect.contains(warnings, blocked, "the warning names the issue that starts too early");
  expect.contains(warnings, blocker, "and the one it is waiting on");

  // The row wears it too, in red, with the words a hover away.
  const row = await page.$eval(`[data-plan-row="${blocked}"]`, (el) => ({ trouble: el.dataset.planTrouble, title: el.title }));
  expect.equal(row.trouble, "error", "a contradiction is an error on the row");
  expect.contains(row.title, blocker, "and its title names the blocker");
  expect.equal(await page.$eval(`[data-plan-bar="${blocked}"]`, (el) => el.dataset.planTrouble), "error", "the bar too");
  expect.equal(await page.$(`[data-plan-row="${blocker}"][data-plan-trouble]`), null, "the blocker itself is fine");

  // The schedule was still saved: a plan is a draft, not a promise.
  const range = await barRange(page, blocked);
  expect.equal(range.start, "2026-03-15", "the dates were kept despite the warning");
});

scenario("a row on the plan says what is wrong with it, in two tones", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Troubled");
  const epic = await createIssue(page, key, "The whole thing", "Epic");
  const child = await addChild(page, epic, "a piece of it", "Story");
  const loose = await createIssue(page, key, "half planned", "Story");

  // A child outside its parent's dates contradicts the parent: red.
  await scheduleIssue(page, epic, "2026-04-01", "2026-04-10");
  await scheduleIssue(page, child, "2026-04-08", "2026-04-20");
  // A start with no end is unfinished, not wrong: yellow.
  await goto(page, `/issues/${loose}`);
  await setDate(page, "#issue-start", "2026-04-03");
  await page.waitForFunction(() => document.querySelector("#issue-start")?.value === "2026-04-03", { timeout: WAIT });

  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector(`[data-plan-row="${child}"]`, { timeout: WAIT });
  expect.equal(await page.$eval(`[data-plan-row="${child}"]`, (el) => el.dataset.planTrouble), "error", "outside its parent is an error");
  expect.contains(await page.$eval(`[data-plan-row="${child}"]`, (el) => el.title), epic, "the row's title names the parent");
  expect.equal(await page.$eval(`[data-plan-row="${loose}"]`, (el) => el.dataset.planTrouble), "warning", "half scheduled is a warning");
  expect.equal(await page.$(`[data-plan-row="${epic}"][data-plan-trouble]`), null, "the parent is untroubled");
  expect.contains(await page.$eval(`[data-plan-trouble-icon="error"]`, (el) => el.getAttribute("aria-label")), epic, "the icon carries the words");
});

scenario("unscheduled work keeps its row on the plan", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Unplanned");
  const story = await createIssue(page, key, "Nobody has planned this", "Story");

  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector(`[data-plan-schedule="${story}"]`, { timeout: WAIT });
  expect.contains(await bodyText(page), "not on the plan yet", "the count is shown");

  await page.click(`[data-plan-schedule="${story}"]`);
  await page.waitForSelector(`[data-plan-bar="${story}"]`, { timeout: WAIT });

  const range = await barRange(page, story);
  expect.truthy(range.start, "the issue now has a start");
  expect.truthy(range.due, "and an end");
});
