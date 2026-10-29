// Projects made from templates, and the pages a template leaves out.

import { scenario, serialScenario } from "../runner.mjs";
import {
  bodyText,
  callAPI,
  clickButton,
  confirm,
  cardsOnBoard,
  commitIssue,
  createBoard,
  createIssue,
  createIssueThroughForm,
  createProject,
  createProjectThroughForm,
  createSprint,
  expect,
  goto,
  openProjectRow,
  planSprint,
  press,
  replaceValue,
  signUp,
  startSprint,
  textOf,
  turnFeature,
  unique,
  uniqueName,
  WAIT,
} from "../helpers.mjs";

scenario("a project made from the Scrum template shows only the running sprint", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProjectThroughForm(page, "Sprinting", "scrum");
  const committed = await createIssueThroughForm(page, key, "committed to the sprint", "Task");
  const waiting = await createIssue(page, key, "still in the backlog", { type: "Task", visit: false });

  // Nothing is running, so the board says so instead of drawing empty columns.
  await goto(page, `/projects/${key}/board`);
  await page.waitForFunction(() => document.body.innerText.includes("No sprint is running"), { timeout: WAIT });
  expect.contains(await bodyText(page), "Scrum", "the board says what kind it is");

  await createSprint(page, key, "Sprint 1");
  await planSprint(page, "Sprint 1", { from: "2026-03-02", to: "2026-03-13", capacity: 10 });
  await commitIssue(page, committed, "Sprint 1");

  // Committed is not running: the board is still empty until the sprint starts.
  await goto(page, `/projects/${key}/board`);
  await page.waitForFunction(() => document.body.innerText.includes("No sprint is running"), { timeout: WAIT });

  await startSprint(page, key, "Sprint 1");

  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector(`[data-card="${committed}"]`, { timeout: WAIT });
  const cards = await page.$$eval("[data-card]", (els) => els.map((el) => el.getAttribute("data-card")));
  expect.equal(cards.join(","), committed, "only the sprint's work is on the board");
  expect.truthy(!cards.includes(waiting), "the backlog stays in the backlog");
  await page.waitForSelector('[data-sprint-showing="Sprint 1"]', { timeout: WAIT });
});

scenario("a kanban board added to a scrum project shows everything", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Mixed", { template: "scrum", visit: false });
  const issueKey = await createIssue(page, key, "work with no sprint", { type: "Task", visit: false });

  await createBoard(page, key, "Flow", "kanban");

  const flow = await cardsOnBoard(page, key, "Flow");
  expect.equal(flow.join(","), issueKey, "the kanban board shows work no sprint holds");

  const scrum = await cardsOnBoard(page, key, "Mixed board");
  expect.equal(scrum.join(","), "", "and the scrum board still waits for a sprint");
});

scenario("a service desk has no sprints until an administrator turns them on", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Helpdesk", { template: "service-desk", visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Chores", { template: "task-tracking", visit: false });
  await createIssue(page, key, "take the bins out", { type: "Task", visit: false });

  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector("[data-card]", { timeout: WAIT });
  const board = await bodyText(page);
  for (const state of ["TO DO", "IN PROGRESS", "DONE"]) {
    expect.contains(board, state, `the ${state} swimlane`);
  }
  expect.truthy(!board.includes("IN REVIEW"), "there is no review step");
  expect.contains(board, "Kanban", "the board is a kanban board");

  // The workflow is the project's own, and the settings page says where from.
  await goto(page, `/projects/${key}/settings/workflows`);
  await page.waitForFunction(() => document.body.innerText.includes("Simple task workflow"), { timeout: WAIT });
  expect.contains(await bodyText(page), "Task tracking workflows", "the scheme the template brought");

  await openProjectRow(page, key);
  expect.contains(await textOf(page, `[data-project="${key}"]`), "Task tracking", "the project list says how it was set up");
});

const card = (name) => `[data-template-card="${name}"]`;

/** Archives the templates an unfinished run left, so the shared installation offers none of them. */
async function archiveTemplates(page, names) {
  const { templates } = await callAPI(page, "GET", "/project-templates?archived=true");
  for (const template of templates.filter((each) => !each.builtin && !each.archived && names.includes(each.name))) {
    await callAPI(page, "POST", `/project-templates/${template.key}/archive`);
  }
}

// Serial: editing and archiving a template changes what everybody's New project form offers.
serialScenario("a duplicate of Scrum, narrowed to two types, makes projects that offer only those until it is archived", async ({ page }) => {
  await signUp(page, unique("templater"), { visit: false });
  const name = uniqueName("Narrow Scrum");
  try {
    await goto(page, "/admin/project-templates");
    await press(page, `${card("Scrum")} [data-action="duplicate-template"]`);
    // The copy opens in the editor, to be named and narrowed.
    await page.waitForSelector("[data-template-editor]", { timeout: WAIT });
    await replaceValue(page, "#template-name", name);
    const ticked = await page.$$eval("[data-template-type][data-ticked]", (rows) => rows.map((row) => row.dataset.templateType));
    for (const each of ticked.filter((type) => type !== "Task" && type !== "Bug")) {
      await press(page, `[data-template-type="${each}"] input[type="checkbox"]`);
    }
    await clickButton(page, "Save template");
    await page.waitForSelector(`${card(name)} [data-action="edit-template"]`, { timeout: WAIT });
    expect.equal(await page.$(`${card(name)} [data-builtin-template]`), null, "the copy is the administrators' own");

    const { templates } = await callAPI(page, "GET", "/project-templates");
    const id = templates.find((each) => each.name === name).key;
    expect.equal(
      templates
        .find((each) => each.key === id)
        .issueTypes.map((type) => type.name)
        .sort()
        .join(","),
      "Bug,Task",
      "the template keeps the two types left ticked",
    );

    await goto(page, "/projects");
    await clickButton(page, "New project");
    await page.waitForSelector(`[data-template-group="made"] [data-template="${id}"]`, { timeout: WAIT });
    const summary = await textOf(page, `[data-template="${id}"]`);
    expect.contains(summary, "Bug", "the card names the types");
    expect.contains(summary, "Task", "the card names both of them");
    await page.keyboard.press("Escape");

    const key = await createProjectThroughForm(page, "Narrow made", id);
    await clickButton(page, "New issue");
    await page.waitForSelector("#issue-type", { timeout: WAIT });
    const offered = await page.$$eval("#issue-type option", (options) => options.map((option) => option.textContent.trim()));
    expect.equal(offered.sort().join(","), "Bug,Task", `${key} offers exactly the template's types`);
    await page.keyboard.press("Escape");

    await goto(page, "/admin/project-templates");
    await press(page, `${card(name)} [data-action="archive-template"]`);
    await confirm(page);
    await page.waitForFunction((selector) => !document.querySelector(selector), { timeout: WAIT }, card(name));

    await goto(page, "/projects");
    await clickButton(page, "New project");
    await page.waitForSelector('[data-template="scrum"]', { timeout: WAIT });
    expect.equal(await page.$(`[data-template="${id}"]`), null, "an archived template leaves the chooser");
  } finally {
    await archiveTemplates(page, [name, "Scrum copy"]);
  }
});
