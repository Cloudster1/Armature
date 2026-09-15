// The organization's own accounts: made with a password, reset, switched off.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  clickButton,
  confirm,
  createLocalUser,
  expect,
  fill,
  goto,
  PASSWORD,
  signIn,
  signUp,
  textOf,
  unique,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("an administrator makes a local user who can sign in, then resets and switches them off", async ({ page }) => {
  const owner = await signUp(page);
  const who = unique("local");

  await createLocalUser(page, { email: who.email, name: who.name, role: "Member", password: PASSWORD });
  expect.contains(await textOf(page, `[data-user="${who.email}"]`), "Password", "the account signs in with a password");

  await signIn(page, who.email, PASSWORD);
  await waitForPath(page, "/");
  await waitForApp(page);
  expect.contains(await bodyText(page), owner.org, "the new person lands in the organization");

  // Back as the owner: a new password ends their session, and switching them off refuses the door.
  await signIn(page, owner.email);
  await waitForApp(page);
  await goto(page, "/settings/users");
  await page.waitForSelector(`[data-user="${who.email}"] [data-action="user-menu"]`, { timeout: WAIT });
  await page.click(`[data-user="${who.email}"] [data-action="user-menu"]`);
  await page.waitForSelector('[data-action="set-password"]', { timeout: WAIT });
  await page.click('[data-action="set-password"]');
  const reset = "a replacement adequate password";
  await fill(page, "New password", reset);
  await clickButton(page, "Set password");
  await page.waitForFunction((e) => !document.querySelector(`[data-set-password="${e}"]`), { timeout: WAIT }, who.email);

  await page.click(`[data-user="${who.email}"] [data-action="user-menu"]`);
  await page.waitForSelector('[data-action="deactivate-user"]', { timeout: WAIT });
  await page.click('[data-action="deactivate-user"]');
  await confirm(page);
  await page.waitForFunction(
    (e) => document.querySelector(`[data-user="${e}"]`)?.getAttribute("data-user-active") === "false",
    { timeout: WAIT },
    who.email,
  );

  await signIn(page, who.email, reset);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains((await textOf(page, "[role=alert]")).toLowerCase(), "deactivated", "a switched off account is told so");
});

scenario("an administrator's own row is not theirs to change here", async ({ page }) => {
  const owner = await signUp(page);
  await goto(page, "/settings/users");
  await page.waitForSelector(`[data-user="${owner.email}"]`, { timeout: WAIT });
  await page.click(`[data-user="${owner.email}"] [data-action="user-menu"]`);
  await page.waitForSelector('[data-action="rename-user"]', { timeout: WAIT });
  const disabled = await page.$eval('[data-action="rename-user"]', (el) => el.disabled || el.getAttribute("aria-disabled") === "true");
  expect.truthy(disabled, "the owner's own row offers nothing");
});
