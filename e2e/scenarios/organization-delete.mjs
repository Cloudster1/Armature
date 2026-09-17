// An organization deleted by its owner, and what everybody in it sees next.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  clickButton,
  expect,
  fill,
  goto,
  inviteFromMembersPage,
  joinThroughLink,
  signIn,
  signOut,
  signUp,
  unique,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

/** Deletes the signed-in owner's organization from its settings page. */
async function deleteOrganization(page) {
  await goto(page, "/settings/organization");
  await page.waitForSelector('[data-action="delete-organization"]', { timeout: WAIT });
  const slug = await page.$eval("main code", (el) => el.textContent.trim());
  await page.click('[data-action="delete-organization"]');
  await fill(page, "confirm organization", slug);
  await page.waitForFunction(() => !document.querySelector('[data-action="confirm-delete-organization"]')?.disabled, { timeout: WAIT });
  await page.click('[data-action="confirm-delete-organization"]');
}

scenario("an owner deletes the organization, and the people in it are told it is gone", async ({ page }) => {
  const owner = await signUp(page);
  const member = unique("stranded");
  const { token } = await inviteFromMembersPage(page, member.email);
  await joinThroughLink(page, token, member);

  await signIn(page, owner.email);
  await waitForApp(page);
  await deleteOrganization(page);
  await page.waitForSelector("[data-no-organization]", { timeout: WAIT });
  expect.contains(await bodyText(page), "You are not in an organization", "the owner has none left");

  await signIn(page, member.email);
  await page.waitForSelector("[data-no-organization]", { timeout: WAIT });
  expect.contains(await bodyText(page), "no longer exists", "the member is told why the app is empty");
});

scenario("an administrator who does not own the organization is not offered deleting it", async ({ page }) => {
  await signUp(page);
  const admin = unique("notowner");
  const { token } = await inviteFromMembersPage(page, admin.email, "Administrator");
  await joinThroughLink(page, token, admin);

  await goto(page, "/settings/organization");
  await page.waitForFunction(() => document.body.innerText.includes("Only an owner of the organization can delete it."), { timeout: WAIT });
  expect.equal(await page.$('[data-action="delete-organization"]'), null, "no delete button");
});

scenario("deleting one of two organizations leaves the other to choose", async ({ page }) => {
  const first = await signUp(page);
  await signOut(page);
  const second = await signUp(page, unique("survivor"));
  await signIn(page, first.email);
  await waitForApp(page);
  const { token } = await inviteFromMembersPage(page, second.email);

  // The second owner joins the first organization, then deletes their own.
  await signIn(page, second.email);
  await waitForApp(page);
  await goto(page, `/invite#${token}`);
  await clickButton(page, `Join ${first.org}`);
  await waitForPath(page, "/");
  await signIn(page, second.email);
  await page.waitForSelector("[data-org-choice-list]", { timeout: WAIT });
  await page.evaluate((name) => [...document.querySelectorAll("[data-org-choice]")].find((b) => b.textContent.includes(name))?.click(), second.org);
  await waitForApp(page);

  await deleteOrganization(page);
  await page.waitForSelector("[data-no-organization] [data-org-choice]", { timeout: WAIT });
  expect.contains(await bodyText(page), first.org, "the organization still there is offered");
  await page.click("[data-no-organization] [data-org-choice]");
  await waitForApp(page);
});
