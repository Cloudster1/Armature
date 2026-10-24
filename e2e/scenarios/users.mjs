// The organization's own accounts: made with a password, reset, switched off.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  clickButton,
  confirm,
  createLocalUser,
  expect,
  fill,
  findUser,
  goto,
  makeAccount,
  PASSWORD,
  press,
  signIn,
  signInThroughForm,
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
  await findUser(page, who.email);
  await press(page, `[data-user="${who.email}"] [data-action="user-menu"]`);
  await press(page, '[data-action="set-password"]');
  const reset = "a replacement adequate password";
  await fill(page, "New password", reset);
  await clickButton(page, "Set password");
  await page.waitForFunction((e) => !document.querySelector(`[data-set-password="${e}"]`), { timeout: WAIT }, who.email);

  await press(page, `[data-user="${who.email}"] [data-action="user-menu"]`);
  await press(page, '[data-action="deactivate-user"]');
  await confirm(page);
  await page.waitForFunction((e) => document.querySelector(`[data-user="${e}"]`)?.getAttribute("data-user-active") === "false", { timeout: WAIT }, who.email);

  await signInThroughForm(page, who.email, reset);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains(await textOf(page, "[role=alert]"), "Your account has been switched off. Ask an administrator.", "a switched off account is told so");
});

scenario("an administrator's own row is not theirs to change here", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  await findUser(page, owner.email);
  await press(page, `[data-user="${owner.email}"] [data-action="user-menu"]`);
  await page.waitForSelector('[data-action="rename-user"]', { timeout: WAIT });
  const disabled = await page.$eval('[data-action="rename-user"]', (el) => el.disabled || el.getAttribute("aria-disabled") === "true");
  expect.truthy(disabled, "the owner's own row offers nothing");
});

// Somebody the identity provider vouched for who is not a member is sent back
// to the sign-in page with a reason. Said there: who can let them in, and
// where. The page that lists the waiting takes no room while nobody waits.
scenario("somebody the provider knows is told who can let them in", async ({ page }) => {
  await goto(page, "/login?sso=not_a_member");
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  const told = await textOf(page, "[role=alert]");
  expect.contains(told, "let you in", "the refusal says what happens next");
  expect.contains(told, "Users page", "the refusal says where");

  await signUp(page, unique("user"), { visit: false });
  await goto(page, "/settings/users");
  await page.waitForSelector("[data-new-user]", { timeout: WAIT });
  const waiting = await page.$("[data-join-requests]");
  expect.truthy(waiting === null, "an empty waiting list is drawn");
});

// The installation the suite shares holds thousands of accounts: the page shows
// one page of them, and somebody is found by typing part of their name.
scenario("the users page finds somebody by part of their name, a page at a time", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const who = await makeAccount(unique("findable"));

  await goto(page, "/settings/users");
  await page.waitForSelector("[data-user]", { timeout: WAIT });
  expect.truthy(await page.$('nav[aria-label="User pages"]'), "a list longer than a page comes with a pager");
  const shown = await page.$$eval("[data-user]", (rows) => rows.length);
  expect.truthy(shown <= 50, `one page of accounts is drawn, not ${shown}`);

  const tail = who.name.split(" ").at(-1);
  await page.click("[data-people-search]");
  await page.keyboard.sendCharacter(tail);
  await page.waitForFunction(
    (email) => document.querySelectorAll("[data-user]").length === 1 && document.querySelector(`[data-user="${email}"]`),
    { timeout: WAIT },
    who.email,
  );
  expect.contains(page.url(), `q=${tail}`, "the search is in the address");
});
