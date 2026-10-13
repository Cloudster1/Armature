// People brought in through the page: invited, mailed, joining, withdrawn.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  confirm,
  expect,
  goto,
  inviteFromMembersPage,
  joinThroughLink,
  mailFor,
  PASSWORD,
  signIn,
  signOut,
  signUp,
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

  await signOut(page, { visit: false });
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

  await signOut(page, { visit: false });
  await goto(page, `/invite#${token}`);
  await page.waitForSelector("[data-invite-invalid]", { timeout: WAIT });
  expect.contains(await bodyText(page), "Ask whoever invited you for a new one", "the page says what to do");
});
