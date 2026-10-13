// An organization deleted by its owner, and what everybody in it sees next.

import { scenario, serialScenario } from "../runner.mjs";
import {
  bodyText,
  clickButton,
  expect,
  fill,
  goto,
  inviteFromMembersPage,
  joinThroughLink,
  signIn,
  signInThroughForm,
  signOut,
  signUp,
  signUpOwnOrganization,
  textOf,
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

// Serial, like every deletion of an organization: this one is the scenario's own.
serialScenario("an owner deletes the organization, and the people in it are switched off", async ({ page }) => {
  const owner = await signUpOwnOrganization(page);
  const member = unique("stranded");
  const { token } = await inviteFromMembersPage(page, member.email);
  await joinThroughLink(page, token, member);

  await signIn(page, owner.email);
  await waitForApp(page);
  await deleteOrganization(page);
  // Nobody is signed in without an organization, so the session ends.
  await waitForPath(page, "/login");

  await signInThroughForm(page, member.email);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains(await textOf(page, "[role=alert]"), "Your account has been switched off. Ask an administrator.", "the member is told what to do");
});

scenario("an administrator who does not own the organization is not offered deleting it", async ({ page }) => {
  // The run's people are administrators; the run's administrator owns it.
  await signUp(page, unique("notowner"), { visit: false });

  await goto(page, "/settings/organization");
  await page.waitForFunction(() => document.body.innerText.includes("Only an owner of the organization can delete it."), { timeout: WAIT });
  expect.equal(await page.$('[data-action="delete-organization"]'), null, "no delete button");
});

serialScenario("deleting one of two organizations leaves its owner the other", async ({ page }) => {
  const first = await signUpOwnOrganization(page, unique("user"), { visit: false });
  await signOut(page, { visit: false });
  const second = await signUpOwnOrganization(page, unique("survivor"), { visit: false });
  await signIn(page, first.email);
  await waitForApp(page);
  const { token } = await inviteFromMembersPage(page, second.email);

  // The second owner joins the first organization, then deletes their own.
  await signIn(page, second.email);
  await waitForApp(page);
  await goto(page, `/invite#${token}`);
  await clickButton(page, `Join ${first.org}`);
  await waitForPath(page, "/");
  await signInThroughForm(page, second.email);
  await page.waitForSelector("[data-org-choice-list]", { timeout: WAIT });
  await page.evaluate((name) => [...document.querySelectorAll("[data-org-choice]")].find((b) => b.textContent.includes(name))?.click(), second.org);
  await waitForApp(page);

  await deleteOrganization(page);
  await waitForPath(page, "/login");
  await signInThroughForm(page, second.email);
  await waitForApp(page);
  expect.contains(await bodyText(page), first.org, "signing in again lands in the organization still there");
});
