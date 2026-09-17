// People brought in through the page: invited, mailed, joining, withdrawn.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  clickButton,
  confirm,
  expect,
  fill,
  goto,
  inviteFromMembersPage,
  joinThroughLink,
  mailFor,
  PASSWORD,
  signIn,
  signOut,
  signUp,
  submit,
  unique,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("an administrator invites somebody from the members page and they join through the link", async ({ page }) => {
  const owner = await signUp(page);
  const who = unique("joiner");
  const since = Date.now();
  const { link, token } = await inviteFromMembersPage(page, who.email);
  expect.contains(link, "/invite#", "the link opens the invitation page");

  // The stack has mail, so the same link also went out.
  const mail = await mailFor(who.email, "invited you", { since });
  expect.contains(mail.text, link, "the mail carries the link the page showed");

  await page.waitForSelector(`[data-pending-invite="${who.email}"]`, { timeout: WAIT });

  await joinThroughLink(page, token, who);
  expect.contains(await bodyText(page), owner.org, "they land in the organization that invited them");

  await signIn(page, owner.email);
  await waitForApp(page);
  await goto(page, "/settings/access");
  await page.click('[data-access-tab="Members"]');
  await page.waitForSelector(`[data-member="${who.name}"]`, { timeout: WAIT });
  expect.equal(await page.$(`[data-pending-invite="${who.email}"]`), null, "the invitation no longer waits");
});

scenario("the people a new member invites can sign in with what they chose", async ({ page }) => {
  await signUp(page);
  const who = unique("chosen");
  const { token } = await inviteFromMembersPage(page, who.email, "Administrator");
  await joinThroughLink(page, token, who);

  await signOut(page);
  await signIn(page, who.email, PASSWORD);
  await waitForPath(page, "/");
  await waitForApp(page);
  expect.contains(await bodyText(page), "Admin", "they hold the role they were invited as");
});

scenario("a withdrawn invitation cannot be used", async ({ page }) => {
  await signUp(page);
  const who = unique("withdrawn");
  const { token } = await inviteFromMembersPage(page, who.email);

  // Pressed through the DOM: the link card above it has just appeared and
  // moved the row, and a click by position can land where it used to be.
  const withdraw = `[data-pending-invite="${who.email}"] button`;
  await page.waitForSelector(withdraw, { timeout: WAIT });
  await page.evaluate((sel) => document.querySelector(sel)?.click(), withdraw);
  await confirm(page);
  await page.waitForFunction((sel) => !document.querySelector(sel), { timeout: WAIT }, `[data-pending-invite="${who.email}"]`);

  await signOut(page);
  await goto(page, `/invite#${token}`);
  await page.waitForSelector("[data-invite-invalid]", { timeout: WAIT });
  expect.contains(await bodyText(page), "Ask whoever invited you for a new one", "the page says what to do");
});

scenario("somebody who already has an account signs in from the invitation and belongs to both", async ({ page }) => {
  const owner = await signUp(page);
  await signOut(page);
  const other = await signUp(page, unique("twofold"));
  await signIn(page, owner.email);
  await waitForApp(page);
  const { token } = await inviteFromMembersPage(page, other.email);

  // Opened signed out: the address has an account, so it asks for a sign-in.
  await signOut(page);
  await goto(page, `/invite#${token}`);
  await fill(page, "Your name", "Somebody Else");
  await fill(page, "Password", PASSWORD);
  await fill(page, "Repeat password", PASSWORD);
  await page.click("[data-invite-accept] button[type=submit]");
  await page.waitForSelector("[data-invite-sign-in]", { timeout: WAIT });
  await clickButton(page, "Sign in to accept");

  await waitForPath(page, "/login");
  await fill(page, "Email", other.email);
  await fill(page, "Password", PASSWORD);
  await submit(page);

  // Back on the invitation, signed in as the right person.
  await waitForPath(page, "/invite");
  await clickButton(page, `Join ${owner.org}`);
  await waitForPath(page, "/");
  await waitForApp(page);
  const both = await bodyText(page);
  expect.contains(both, owner.org, "now working in the organization that invited them");
});

scenario("somebody in two organizations chooses which one when signing in", async ({ page }) => {
  const owner = await signUp(page);
  await signOut(page);
  const other = await signUp(page, unique("chooser"));
  await signIn(page, owner.email);
  await waitForApp(page);
  const { token } = await inviteFromMembersPage(page, other.email);

  await signIn(page, other.email);
  await waitForApp(page);
  await goto(page, `/invite#${token}`);
  await clickButton(page, `Join ${owner.org}`);
  await waitForPath(page, "/");

  await signIn(page, other.email);
  await page.waitForSelector("[data-org-choice-list]", { timeout: WAIT });
  const offered = await bodyText(page);
  expect.contains(offered, owner.org, "the organization they joined is offered");
  expect.contains(offered, other.org, "and their own");

  await page.evaluate((name) => [...document.querySelectorAll("[data-org-choice]")].find((b) => b.textContent.includes(name))?.click(), owner.org);
  await waitForPath(page, "/");
  await waitForApp(page);
  await page.waitForFunction((name) => document.querySelector("[data-sidebar]")?.innerText.includes(name), { timeout: WAIT }, owner.org);
});
