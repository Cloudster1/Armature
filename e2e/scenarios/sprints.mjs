// Sprints, capacity and teams.

import { scenario } from "../runner.mjs";
import {
  assignTeam,
  bodyText,
  capacityOf,
  cardsOnBoard,
  clickButton,
  commitIssue,
  confirm,
  createIssue,
  createProject,
  createSprint,
  createTeam,
  estimateIssue,
  expect,
  goto,
  joinTeam,
  planSprint,
  selectByLabel,
  signUp,
  WAIT,
} from "../helpers.mjs";

scenario("a sprint counts what is committed to it against its capacity", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Committing");
  const first = await createIssue(page, key, "five points of work", "Story");
  const second = await createIssue(page, key, "three points of work", "Task");
  await createIssue(page, key, "nobody has sized this", "Task");

  await createSprint(page, key, "Sprint 1");
  await planSprint(page, "Sprint 1", { from: "2026-03-02", to: "2026-03-13", capacity: 10 });

  await estimateIssue(page, first, 5);
  await estimateIssue(page, second, 3);
  await commitIssue(page, first, "Sprint 1");
  await commitIssue(page, second, "Sprint 1");

  const capacity = await capacityOf(page, key, "Sprint 1");
  expect.contains(capacity, "8 of 10 pts", "the committed work is counted against the capacity");
});

scenario("committing past the capacity is reported, not refused", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Overcommitting");
  const heavy = await createIssue(page, key, "more than fits", "Story");

  await createSprint(page, key, "Sprint 1");
  await planSprint(page, "Sprint 1", { from: "2026-03-02", to: "2026-03-13", capacity: 5 });
  await estimateIssue(page, heavy, 13);
  await commitIssue(page, heavy, "Sprint 1");

  // The commitment stands, and the plan says what it costs.
  const capacity = await capacityOf(page, key, "Sprint 1");
  expect.contains(capacity, "13 of 5 pts", "the work was committed anyway");

  const warnings = await page.$eval('[data-testid="plan-warnings"]', (el) => el.innerText);
  expect.contains(warnings, "Sprint 1", "the warning names the sprint");
  expect.contains(warnings, "13 points", "and how much is committed");
});

scenario("an unsized issue is counted, not guessed at", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Unsized");
  const sized = await createIssue(page, key, "this one has a number", "Story");
  const unsized = await createIssue(page, key, "this one does not", "Task");

  await createSprint(page, key, "Sprint 1");
  await planSprint(page, "Sprint 1", { from: "2026-03-02", to: "2026-03-13", capacity: 10 });
  await estimateIssue(page, sized, 5);
  await commitIssue(page, sized, "Sprint 1");
  await commitIssue(page, unsized, "Sprint 1");

  const capacity = await capacityOf(page, key, "Sprint 1");
  expect.contains(capacity, "5 of 10 pts", "only the sized work is counted");
  expect.contains(capacity, "1 unestimated", "and the rest is reported rather than assumed");
});

scenario("a team runs one sprint at a time", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "One at a time");

  await createSprint(page, key, "Sprint 1");
  await planSprint(page, "Sprint 1", { from: "2026-03-02", to: "2026-03-13", capacity: 10 });
  await createSprint(page, key, "Sprint 2");
  await planSprint(page, "Sprint 2", { from: "2026-03-16", to: "2026-03-27", capacity: 10 });

  await goto(page, `/projects/${key}/sprints`);
  await page.click('[data-sprint="Sprint 1"] button');
  await page.waitForFunction(
    () => document.body.innerText.includes("Running"),
    { timeout: WAIT },
  );

  // Starting the second is refused, and the refusal names the one in the way.
  await page.evaluate(() => {
    const card = document.querySelector('[data-sprint="Sprint 2"]');
    [...card.querySelectorAll("button")].find((b) => b.textContent.trim() === "Start sprint")?.click();
  });
  await page.waitForFunction(
    () => document.body.innerText.includes("Sprint 1 is already running"),
    { timeout: WAIT },
  );
});

scenario("completing a sprint carries the unfinished work onward", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Carrying");
  const unfinished = await createIssue(page, key, "this does not get done", "Task");

  await createSprint(page, key, "Sprint 1");
  await planSprint(page, "Sprint 1", { from: "2026-03-02", to: "2026-03-13", capacity: 10 });
  await createSprint(page, key, "Sprint 2");
  await planSprint(page, "Sprint 2", { from: "2026-03-16", to: "2026-03-27", capacity: 10 });

  await estimateIssue(page, unfinished, 5);
  await commitIssue(page, unfinished, "Sprint 1");

  await goto(page, `/projects/${key}/sprints`);
  await page.click('[data-sprint="Sprint 1"] button');
  await page.waitForFunction(() => document.body.innerText.includes("Running"), { timeout: WAIT });

  await selectByLabel(page, '[data-sprint="Sprint 1"] select', "Carry the rest to Sprint 2");
  await clickButton(page, "Complete sprint");
  await page.waitForFunction(
    () => document.body.innerText.includes("Completed"),
    { timeout: WAIT },
  );

  // The issue moved, and its changelog says so.
  await goto(page, `/issues/${unfinished}`);
  await page.waitForFunction(
    () => document.body.innerText.includes("Sprint 2"),
    { timeout: WAIT },
  );
  expect.contains(await bodyText(page), "Sprint 1", "the changelog records where it came from");
});

scenario("a project splits into teams and each gets a board of its own", async ({ page }) => {
  const who = await signUp(page);
  const key = await createProject(page, "Splitting");
  const mine = await createIssue(page, key, "platform work", "Task");
  const theirs = await createIssue(page, key, "portal work", "Task");
  const nobodys = await createIssue(page, key, "nobody has taken this on", "Task");

  await createTeam(page, key, "Platform");
  await createTeam(page, key, "Portal");
  await joinTeam(page, key, "Platform", who.name);

  await assignTeam(page, mine, "Platform");
  await assignTeam(page, theirs, "Portal");

  // Forming a team offers it a board, because a team without one has a backlog
  // nobody can look at.
  await goto(page, `/projects/${key}/board`);
  await clickButton(page, "Give Platform a board");
  await page.waitForSelector('[data-board="Platform board"]', { timeout: WAIT });
  await clickButton(page, "Give Portal a board");
  await page.waitForSelector('[data-board="Portal board"]', { timeout: WAIT });

  const platform = await cardsOnBoard(page, key, "Platform board");
  expect.equal(platform.join(","), mine, "the platform board shows only its own work");

  const portal = await cardsOnBoard(page, key, "Portal board");
  expect.equal(portal.join(","), theirs, "and the portal board only shows its own");

  // The board over the whole project is what a project without teams has, and
  // it does not stop working when teams appear.
  const whole = await cardsOnBoard(page, key, "Splitting board");
  expect.truthy(whole.includes(nobodys), "the project's own board still sees unassigned work");
  expect.equal(whole.length, 3, "and everything else");
});

scenario("each team plans out of its own backlog", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Two backlogs");
  const mine = await createIssue(page, key, "platform work", "Task");
  const theirs = await createIssue(page, key, "portal work", "Task");

  await createTeam(page, key, "Platform");
  await createTeam(page, key, "Portal");
  await assignTeam(page, mine, "Platform");
  await assignTeam(page, theirs, "Portal");

  await goto(page, `/projects/${key}/sprints`);
  await page.waitForSelector('[data-backlog-of="Platform"]', { timeout: WAIT });

  await page.click('[data-backlog-of="Platform"]');
  await page.waitForFunction(
    (k) => document.querySelector(`[data-issue="${k}"]`) !== null,
    { timeout: WAIT },
    mine,
  );
  let shown = await page.$$eval("[data-issue]", (els) => els.map((e) => e.getAttribute("data-issue")));
  expect.equal(shown.join(","), mine, "the platform backlog holds only platform work");

  await page.click('[data-backlog-of="Portal"]');
  await page.waitForFunction(
    (k) => document.querySelector(`[data-issue="${k}"]`) !== null,
    { timeout: WAIT },
    theirs,
  );
  shown = await page.$$eval("[data-issue]", (els) => els.map((e) => e.getAttribute("data-issue")));
  expect.equal(shown.join(","), theirs, "and the portal backlog only portal work");
});

scenario("two teams run sprints at the same time", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "In parallel");
  await createTeam(page, key, "Platform");
  await createTeam(page, key, "Portal");

  for (const [team, name] of [["Platform", "Platform 1"], ["Portal", "Portal 1"]]) {
    await goto(page, `/projects/${key}/sprints`);
    await page.click(`[data-backlog-of="${team}"]`);
    await page.waitForSelector("#field-new-sprint", { timeout: WAIT });
    await page.type("#field-new-sprint", name);
    await clickButton(page, "Add sprint");
    await page.waitForFunction(
      (n) => document.querySelector(`[data-sprint="${n}"]`) !== null,
      { timeout: WAIT },
      name,
    );
    await planSprint(page, name, { from: "2026-03-02", to: "2026-03-13", capacity: 10 });
    await page.click(`[data-sprint="${name}"] button`);
    await page.waitForFunction(
      (n) => document.querySelector(`[data-sprint="${n}"]`)?.innerText.includes("Running"),
      { timeout: WAIT },
      name,
    );
  }

  // Both are running, which is the whole point of separate backlogs.
  await goto(page, `/projects/${key}/sprints`);
  await page.click('[data-backlog-of="Platform"]');
  await page.waitForFunction(
    () => document.querySelector('[data-sprint="Platform 1"]')?.innerText.includes("Running"),
    { timeout: WAIT },
  );
});

scenario("a team carrying work cannot just be deleted", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Still carrying");
  const task = await createIssue(page, key, "carried by the team", "Task");

  await createTeam(page, key, "Platform");
  await assignTeam(page, task, "Platform");

  await goto(page, `/projects/${key}/teams`);
  await page.evaluate(() => {
    const card = document.querySelector('[data-team="Platform"]');
    [...card.querySelectorAll("button")].find((b) => b.textContent.trim() === "Delete")?.click();
  });
  await confirm(page);
  await page.waitForFunction(
    () => document.body.innerText.includes("still assigned to Platform"),
    { timeout: WAIT },
  );
});
