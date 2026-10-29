// Asking for access to an issue one may not see, and granting it (#279).
import { scenario } from "../runner.mjs";
import {
  WAIT,
  acceptInvite,
  createIssue,
  createProject,
  eventually,
  expect,
  goto,
  grantRole,
  inviteMember,
  revokeAll,
  signIn,
  signUp,
  textOf,
  unique,
  waitForApp,
} from "../helpers.mjs";

/** The project's administrator, an issue of it, and a member it is a wall to. */
async function aHiddenIssue(page, prefix) {
  const owner = await signUp(page, unique(prefix), { visit: false });
  const key = await createProject(page, "Asked about", { visit: false });
  const issueKey = await createIssue(page, key, "the work somebody asks to see", { type: "Task", visit: false });
  // Told as the project's own administrator, not as one of everybody's.
  await grantRole(page, { who: owner.name, role: "Project administrator", projectKey: key });
  const asker = await inviteMember(page, "asker");
  await acceptInvite(page, asker, { visit: false });
  await signIn(page, owner.email);
  await waitForApp(page);
  // Joining grants an ordinary role over every project; this one sees none.
  await revokeAll(page, asker.name);
  return { owner, asker, key, issueKey };
}

scenario("somebody who may not see an issue asks for access, and its administrator grants it", async ({ page }) => {
  const { owner, asker, issueKey } = await aHiddenIssue(page, "granter");

  await signIn(page, asker.email);
  await waitForApp(page);
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector('[data-issue-unavailable="not-found"]', { timeout: WAIT });
  expect.equal((await textOf(page, "body")).includes("the work somebody asks to see"), false, "the summary is nowhere");
  await page.type("[data-issue-unavailable] textarea", "I review this work.");
  await page.click('[data-action="ask-for-access"]');
  await page.waitForSelector("[data-access-asked]", { timeout: WAIT });

  // The administrator hears through the inbox and grants it from there.
  await signIn(page, owner.email);
  await waitForApp(page);
  await goto(page, "/notifications");
  const notice = '[data-notification="access_requested"]';
  await eventually(page, notice);
  expect.contains(await textOf(page, notice), `asks for access to ${issueKey}`, "the notice names the issue");
  await page.click(notice);
  await page.waitForSelector(`[data-access-request="${issueKey}"][data-access-request-state="open"]`, { timeout: WAIT });
  expect.contains(await textOf(page, "[data-access-request]"), "I review this work.", "the note is there");
  await page.click('[data-action="grant-access"]');
  await page.waitForSelector(`[data-access-request="${issueKey}"][data-access-request-state="granted"]`, { timeout: WAIT });

  await signIn(page, asker.email);
  await waitForApp(page);
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector(`[data-issue-page="${issueKey}"]`, { timeout: WAIT });
});

scenario("a project that says its issues exist shows them as restricted, by name", async ({ page }) => {
  const { asker, key, issueKey } = await aHiddenIssue(page, "revealer");
  await goto(page, `/projects/${key}/settings`);
  await page.waitForSelector('[data-setting="reveals-issues"]', { timeout: WAIT });
  await page.click('[data-setting="reveals-issues"]');
  await page.waitForFunction(() => document.body.innerText.includes("now show as restricted"), { timeout: WAIT });

  await signIn(page, asker.email);
  await waitForApp(page);
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector('[data-issue-unavailable="restricted"]', { timeout: WAIT });
  expect.contains(await textOf(page, "[data-issue-unavailable]"), "Asked about", "it names the project");
  // One that does not exist is still not found.
  await goto(page, `/issues/${key}-999`);
  await page.waitForSelector('[data-issue-unavailable="not-found"]', { timeout: WAIT });
});
