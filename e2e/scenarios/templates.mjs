// Projects made from templates, and the pages a template leaves out.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  cardsOnBoard,
  commitIssue,
  createBoard,
  createIssue,
  createProject,
  createSprint,
  expect,
  goto,
  planSprint,
  signUp,
  startSprint,
  turnFeature,
  WAIT,
} from "../helpers.mjs";

scenario("a project made from the Scrum template shows only the running sprint", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Sprinting", "scrum");
  const committed = await createIssue(page, key, "committed to the sprint", "Task");
  const waiting = await createIssue(page, key, "still in the backlog", "Task");

  // Nothing is running, so the board says so instead of drawing empty columns.
  await goto(page, `/projects/${key}/board`);
  await page.waitForFunction(
    () => document.body.innerText.includes("No sprint is running"),
    { timeout: WAIT },
  );
  expect.contains(await bodyText(page), "Scrum", "the board says what kind it is");

  await createSprint(page, key, "Sprint 1");
  await planSprint(page, "Sprint 1", { from: "2026-03-02", to: "2026-03-13", capacity: 10 });
  await commitIssue(page, committed, "Sprint 1");

  // Committed is not running: the board is still empty until the sprint starts.
  await goto(page, `/projects/${key}/board`);
  await page.waitForFunction(
    () => document.body.innerText.includes("No sprint is running"),
    { timeout: WAIT },
  );

  await startSprint(page, key, "Sprint 1");

  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector(`[data-card="${committed}"]`, { timeout: WAIT });
  const cards = await page.$$eval("[data-card]", (els) => els.map((el) => el.getAttribute("data-card")));
  expect.equal(cards.join(","), committed, "only the sprint's work is on the board");
  expect.truthy(!cards.includes(waiting), "the backlog stays in the backlog");
  await page.waitForSelector('[data-sprint-showing="Sprint 1"]', { timeout: WAIT });
});

scenario("a kanban board added to a scrum project shows everything", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Mixed", "scrum");
  const issueKey = await createIssue(page, key, "work with no sprint", "Task");

  await createBoard(page, key, "Flow", "kanban");

  const flow = await cardsOnBoard(page, key, "Flow");
  expect.equal(flow.join(","), issueKey, "the kanban board shows work no sprint holds");

  const scrum = await cardsOnBoard(page, key, "Mixed board");
  expect.equal(scrum.join(","), "", "and the scrum board still waits for a sprint");
});

scenario("a service desk has no sprints until an administrator turns them on", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Helpdesk", "service-desk");

  // The template left the page out: the sidebar does not list it, and an old
  // link lands on a notice rather than an empty page.
  await goto(page, `/projects/${key}`);
  await page.waitForSelector("[data-sidebar]", { timeout: WAIT });
  const nav = await page.$eval("[data-sidebar]", (el) => el.innerText);
  expect.contains(nav, "Queues", "the desk's own pages are listed");
  expect.truthy(!nav.includes("Sprints"), "sprints are not");
  await goto(page, `/projects/${key}/sprints`);
  await page.waitForSelector('[data-feature-off="sprints"]', { timeout: WAIT });
  expect.contains(await bodyText(page), "does not use sprints", "the notice says why");

  await turnFeature(page, key, "sprints", true);
  await goto(page, `/projects/${key}/sprints`);
  await page.waitForFunction(() => document.body.innerText.includes("New sprint"), { timeout: WAIT });
  expect.contains(await page.$eval("[data-sidebar]", (el) => el.innerText), "Sprints", "and the sidebar lists the page now");
});

scenario("the Task tracking template gives a project a simple workflow of its own", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Chores", "task-tracking");
  await createIssue(page, key, "take the bins out", "Task");

  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector("[data-card]", { timeout: WAIT });
  const board = await bodyText(page);
  for (const state of ["TO DO", "IN PROGRESS", "DONE"]) {
    expect.contains(board, state, `the ${state} swimlane`);
  }
  expect.truthy(!board.includes("IN REVIEW"), "there is no review step");
  expect.contains(board, "Kanban", "the board is a kanban board");

  // The workflow is the project's own, and the settings page says where from.
  await goto(page, `/projects/${key}/workflows`);
  await page.waitForFunction(
    () => document.body.innerText.includes("Simple task workflow"),
    { timeout: WAIT },
  );
  expect.contains(await bodyText(page), "Task tracking workflows", "the scheme the template brought");

  await goto(page, "/projects");
  await page.waitForFunction(() => document.body.innerText.includes("Chores"), { timeout: WAIT });
  expect.contains(await bodyText(page), "Task tracking", "the project list says how it was set up");
});
