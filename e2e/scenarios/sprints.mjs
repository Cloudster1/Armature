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
  unique,
  waitForButtonToGo,
  WAIT,
} from "../helpers.mjs";

scenario("a sprint counts what is committed to it against its capacity", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Committing", { visit: false });
  const first = await createIssue(page, key, "five points of work", { type: "Story", visit: false });
  const second = await createIssue(page, key, "three points of work", { type: "Task", visit: false });
  await createIssue(page, key, "nobody has sized this", { type: "Task", visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Overcommitting", { visit: false });
  const heavy = await createIssue(page, key, "more than fits", { type: "Story", visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Unsized", { visit: false });
  const sized = await createIssue(page, key, "this one has a number", { type: "Story", visit: false });
  const unsized = await createIssue(page, key, "this one does not", { type: "Task", visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "One at a time", { visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Carrying", { visit: false });
  const unfinished = await createIssue(page, key, "this does not get done", { type: "Task", visit: false });

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
  const who = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Splitting", { visit: false });
  const mine = await createIssue(page, key, "platform work", { type: "Task", visit: false });
  const theirs = await createIssue(page, key, "portal work", { type: "Task", visit: false });
  const nobodys = await createIssue(page, key, "nobody has taken this on", { type: "Task", visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Two backlogs", { visit: false });
  const mine = await createIssue(page, key, "platform work", { type: "Task", visit: false });
  const theirs = await createIssue(page, key, "portal work", { type: "Task", visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "In parallel", { visit: false });
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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Still carrying", { visit: false });
  const task = await createIssue(page, key, "carried by the team", { type: "Task", visit: false });

  await createTeam(page, key, "Platform");
  await assignTeam(page, task, "Platform");

  await goto(page, `/projects/${key}/teams`);
  await page.evaluate(() => {
    const card = document.querySelector('[data-team="Platform"]');
    [...card.querySelectorAll("button")].find((b) => b.textContent.trim() === "Delete")?.click();
  });
  await confirm(page);
  await page.waitForFunction(
    () => document.body.innerText.includes("Platform still has 1 issue."),
    { timeout: WAIT },
  );
});

// More than the largest page the API serves, so the old work is off the newest one.
const PAST_A_PAGE = 205;

scenario("an old committed issue stays in its sprint past the newest 200", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Long history", { visit: false });
  const old = await createIssue(page, key, "committed long ago", { type: "Task", visit: false });
  const finished = await createIssue(page, key, "finished long ago", { type: "Task", visit: false });
  await goto(page, `/issues/${finished}`);
  await clickButton(page, "Close");
  await waitForButtonToGo(page, "Close");

  await createSprint(page, key, "Sprint 3");
  await commitIssue(page, old, "Sprint 3");

  // Filed through the API: two hundred trips through the dialog would test the dialog.
  const filed = await page.evaluate(async (projectKey, count) => {
    let made = 0;
    for (let i = 0; i < count; i += 1) {
      const response = await fetch(`/api/v1/projects/${projectKey}/issues`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ summary: `newer work ${i}` }),
      });
      if (response.ok) made += 1;
    }
    return made;
  }, key, PAST_A_PAGE);
  expect.equal(filed, PAST_A_PAGE, "the newer work was filed");

  await goto(page, `/projects/${key}/sprints`);
  await page.waitForFunction(
    (k) => document.querySelector(`[data-sprint="Sprint 3"] [data-issue="${k}"]`) !== null,
    { timeout: WAIT },
    old,
  );

  // The card is not empty, so it is not offered for deletion.
  const buttons = await page.$$eval('[data-sprint="Sprint 3"] button', (els) => els.map((b) => b.textContent.trim()));
  expect.notContains(buttons.join(","), "Delete", "a sprint holding work is offered for deletion");

  // The backlog is every newer issue, and nothing that is done or committed.
  await page.waitForFunction(
    (n) => document.querySelector('[data-testid="backlog"] h2')?.innerText.includes(`(${n})`),
    { timeout: WAIT },
    PAST_A_PAGE,
  );
  const backlog = await page.$$eval('[data-testid="backlog"] [data-issue]', (els) => els.map((e) => e.getAttribute("data-issue")));
  expect.truthy(!backlog.includes(finished), "the finished work is not in the backlog");
  expect.truthy(!backlog.includes(old), "nor is the committed work");
});
