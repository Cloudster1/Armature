// Which fields an issue shows, where, and in what order, decided per issue
// type by the project, and followed by the issue page and the create form.

import { scenario } from "../runner.mjs";
import {
  arrangeIssue,
  clickButton,
  createIssue,
  createProject,
  expect,
  goto,
  groupRows,
  selectByLabel,
  settled,
  signUp,
  WAIT,
} from "../helpers.mjs";

scenario("a project arranges an issue type and the page and the form follow", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Arranged");
  const task = await createIssue(page, key, "the arranged task", "Task");
  const story = await createIssue(page, key, "the untouched story", "Story");

  // A new project draws an issue the way this tracker does, with no row of its
  // own in the database: Sprint under Planning, Priority under Tracking.
  await goto(page, `/issues/${task}`);
  expect.contains((await groupRows(page, "Planning")).join(" "), "Sprint", "a new project draws Sprint where this tracker puts it");
  expect.contains((await groupRows(page, "Tracking")).join(" "), "Priority", "and Priority where this tracker puts it");

  await arrangeIssue(page, key, "Task", {
    moves: [
      { place: "sprint", area: "Not shown" },
      { place: "priority", area: "People" },
    ],
  });

  await goto(page, `/issues/${task}`);
  await settled(page);
  expect.truthy(!(await page.$("#issue-sprint")), "a hidden field is not drawn at all");
  expect.contains((await groupRows(page, "People")).join(" "), "Priority", "a field moved is drawn where it was put");
  expect.notContains((await groupRows(page, "Tracking")).join(" "), "Priority", "and not where it used to be");

  // The form that files an issue asks for what the page shows and no more, so
  // a field nobody shows is not a field anybody is asked for.
  await goto(page, `/projects/${key}`);
  await clickButton(page, "New issue");
  await selectByLabel(page, "#issue-type", "Task");
  await page.waitForSelector("[data-create-issue] #field-assignee", { timeout: WAIT });
  expect.truthy(!(await page.$("[data-create-issue] #field-sprint")), "the create form does not ask for a hidden field");

  // The same dialog, the other issue type: the arrangement is per type.
  await selectByLabel(page, "#issue-type", "Story");
  await page.waitForSelector("[data-create-issue] #field-sprint", { timeout: WAIT });

  await goto(page, `/issues/${story}`);
  expect.contains((await groupRows(page, "Planning")).join(" "), "Sprint", "another issue type in the same project keeps what it had");

  // Handed back, the project stops having an opinion and the issue is drawn
  // the way it was before anybody arranged it.
  await arrangeIssue(page, key, "Task", { follow: true });
  await goto(page, `/issues/${task}`);
  await settled(page);
  await page.waitForSelector("#issue-sprint", { timeout: WAIT });
  expect.contains((await groupRows(page, "Tracking")).join(" "), "Priority", "handing the type back brings the built-in arrangement back");
});
