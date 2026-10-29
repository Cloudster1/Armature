// The library of issue types (#464): an administrator adds a type, edits it,
// retires it and deletes it, while the built-in ones stay as they are. Each
// project then allows its own types (#466), and its pickers offer only those.

import { scenario, serialScenario } from "../runner.mjs";
import {
  clickButton,
  confirm,
  createIssue,
  createProject,
  expect,
  fill,
  goto,
  issueTypeNames,
  openSetupTab,
  pickProject,
  press,
  removeIssueType,
  replaceValue,
  signUp,
  unique,
  uniqueName,
  WAIT,
} from "../helpers.mjs";

const row = (name) => `[data-issue-type-row="${name}"]`;

async function choose(page, name, action) {
  await press(page, `${row(name)} [data-action="issue-type-menu"]`);
  await press(page, `[role="menu"] [data-action="${action}"]`);
}

// Serial: a type is offered to every project, so editing or retiring one changes everybody's pickers.
serialScenario("an administrator adds an issue type, edits, retires and deletes it", async ({ page }) => {
  await signUp(page, unique("typekeeper"));
  const made = uniqueName("Change request");
  const renamed = uniqueName("Change");
  try {
    await goto(page, "/admin/issue-types");
    await page.waitForSelector(`${row("Task")} [data-builtin-type]`, { timeout: WAIT });

    await fill(page, "New issue type", made);
    await press(page, '[data-new-issue-type] [data-icon="change"]');
    await clickButton(page, "Add type");
    await page.waitForSelector(row(made), { timeout: WAIT });
    expect.equal(await page.$(`${row(made)} [data-builtin-type]`), null, "a type of one's own is not built in");

    await choose(page, made, "edit-issue-type");
    await replaceValue(page, "#issue-type-name", renamed);
    await press(page, '[data-edit-issue-type] [data-icon="improvement"]');
    await clickButton(page, "Save type");
    await page.waitForSelector(row(renamed), { timeout: WAIT });

    await choose(page, renamed, "retire-issue-type");
    await page.waitForSelector(`${row(renamed)} [data-retired-type]`, { timeout: WAIT });
    expect.equal((await issueTypeNames(page)).includes(renamed), false, "a retired type is not offered for new work");
    expect.equal((await issueTypeNames(page, { retired: true })).includes(renamed), true, "but it is still in the library");

    await choose(page, renamed, "delete-issue-type");
    await confirm(page);
    await page.waitForFunction((selector) => !document.querySelector(selector), { timeout: WAIT }, row(renamed));
    expect.equal((await issueTypeNames(page, { retired: true })).includes(renamed), false, "the deleted type is gone");
  } finally {
    await removeIssueType(page, made);
    await removeIssueType(page, renamed);
  }
});

const optionsOf = (page, selector) => page.$$eval(selector, (options) => options.map((option) => option.textContent.trim()));

scenario("a project narrowed to Task and Bug offers only those and refuses an Epic moved in", async ({ page }) => {
  await signUp(page, unique("typist"), { visit: false });
  const narrow = await createProject(page, "Narrowed", { visit: false });
  const other = await createProject(page, "Wide", { visit: false });

  await openSetupTab(page, narrow, "issue-types");
  await page.waitForSelector('[data-project-type="Task"][data-ticked]', { timeout: WAIT });
  const ticked = await page.$$eval("[data-project-type][data-ticked]", (rows) => rows.map((row) => row.dataset.projectType));
  for (const name of ticked.filter((each) => each !== "Task" && each !== "Bug")) await press(page, `[data-project-type="${name}"] input[type="checkbox"]`);
  await clickButton(page, "Save types");
  await page.waitForFunction(() => document.body.innerText.includes("Issue types saved"), { timeout: WAIT });
  expect.equal(
    (await page.$$eval("[data-project-type][data-ticked]", (rows) => rows.map((row) => row.dataset.projectType))).sort().join(","),
    "Bug,Task",
    "the tab keeps only the two that were left ticked",
  );

  await goto(page, `/projects/${narrow}`);
  await clickButton(page, "New issue");
  await page.waitForSelector("#issue-type", { timeout: WAIT });
  expect.equal((await optionsOf(page, "#issue-type option")).sort().join(","), "Bug,Task", "creating offers only the project's types");
  await page.keyboard.press("Escape");

  const key = await createIssue(page, narrow, "Something to retype", { type: "Task", visit: false });
  await goto(page, `/issues/${key}`);
  await page.waitForSelector('[data-action="change-type"]', { timeout: WAIT });
  await page.click('[data-action="change-type"]');
  await page.waitForSelector('[role="menu"] [data-type-option]', { timeout: WAIT });
  const offered = await page.$$eval('[role="menu"] [data-type-option]', (items) => items.map((item) => item.dataset.typeOption));
  expect.equal(offered.join(","), "Bug", "changing the type offers only the other of the two");
  await page.click('[role="menu"] [data-type-option="Bug"]');
  await page.waitForSelector('[data-action="change-type"][aria-label="Issue type: Bug"]', { timeout: WAIT });

  const epic = await createIssue(page, other, "An epic from elsewhere", { type: "Epic", visit: false });
  await goto(page, `/issues/${epic}`);
  await page.waitForSelector('[data-action="issue-more"]', { timeout: WAIT });
  await page.click('[data-action="issue-more"]');
  await page.waitForSelector('[data-action="move"]', { timeout: WAIT });
  await page.click('[data-action="move"]');
  await page.waitForSelector("[data-move-dialog]", { timeout: WAIT });
  await pickProject(page, "#field-move-project", narrow);
  await page.click('[data-action="move-go"]');
  await page.waitForSelector("[data-move-dialog] [role='alert']", { timeout: WAIT });
  expect.contains(
    await page.$eval("[data-move-dialog] [role='alert']", (el) => el.innerText),
    `${narrow} does not use Epic; ask its administrator to allow it or change the type first.`,
    "the move is refused in the server's own sentence",
  );
});
