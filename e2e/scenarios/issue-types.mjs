// The library of issue types (#464): an administrator adds a type, edits it,
// retires it and deletes it, while the built-in ones stay as they are.

import { serialScenario } from "../runner.mjs";
import {
  clickButton,
  confirm,
  expect,
  fill,
  goto,
  issueTypeNames,
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
