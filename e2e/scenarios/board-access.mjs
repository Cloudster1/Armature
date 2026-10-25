// Who may set up a board, decided by the roles held in the board's project.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  cardsIn,
  clickButton,
  createIssue,
  createProject,
  dragCardTo,
  expect,
  goto,
  grantRole,
  inviteMember,
  revokeAll,
  signIn,
  signUp,
  unique,
  WAIT,
  waitForApp,
} from "../helpers.mjs";

/** Whether a button with exactly this label is on the page. */
async function hasButton(page, label) {
  return page.evaluate((words) => [...document.querySelectorAll("button")].some((b) => b.textContent.trim() === words), label);
}

/** Makes a member of the installation who holds only this role, in this project alone. */
async function memberWith(page, owner, prefix, role, projectKey) {
  const who = await inviteMember(page, prefix);
  await acceptInvite(page, who, { visit: false });
  await signIn(page, owner.email);
  await waitForApp(page);
  await revokeAll(page, who.name);
  await grantRole(page, { who: who.name, role, projectKey });
  return who;
}

scenario("a board is set up by whoever its project lets, whatever their role across all projects", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Board rights", { visit: false });
  const issueKey = await createIssue(page, key, "dragged by a user", { visit: false });

  const user = await memberWith(page, owner, "boarduser", "User", key);
  const admin = await memberWith(page, owner, "boardadmin", "Project administrator", key);

  // A user works the board but does not set it up.
  await signIn(page, user.email);
  await waitForApp(page);
  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector(`[data-card="${issueKey}"]`, { timeout: WAIT });
  for (const label of ["Configure swimlanes", "New board", "Rename", "Change type", "Delete"]) {
    expect.equal(await hasButton(page, label), false, `a user is not offered ${label}`);
  }
  await dragCardTo(page, issueKey, "In Progress");
  await page.waitForFunction((k) => document.querySelector(`[data-swimlane="In Progress"] [data-card="${k}"]`) !== null, { timeout: WAIT }, issueKey);
  await page.waitForFunction(() => document.body.innerText.includes("Start progress"), { timeout: WAIT });
  expect.equal((await cardsIn(page, "To Do")).join(","), "", "the user's drag moved the card");

  // A project administrator who is a plain member of the installation sets it up.
  await signIn(page, admin.email);
  await waitForApp(page);
  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector(`[data-card="${issueKey}"]`, { timeout: WAIT });
  for (const label of ["Configure swimlanes", "New board", "Rename", "Change type"]) {
    expect.equal(await hasButton(page, label), true, `a project administrator is offered ${label}`);
  }
  await clickButton(page, "Configure swimlanes");
  await page.waitForFunction(() => document.body.innerText.includes("Done configuring"), { timeout: WAIT });
});
