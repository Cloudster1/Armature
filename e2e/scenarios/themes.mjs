// Custom themes: made, previewed, used, shared and deleted.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  confirm,
  createTheme,
  expect,
  goto,
  inviteMember,
  reload,
  signIn,
  signUp,
  textOf,
  WAIT,
  waitForApp,
} from "../helpers.mjs";

const accentOf = (page) => page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim());

scenario("a theme changes the accent for whoever uses it, and survives a reload", async ({ page }) => {
  const owner = await signUp(page);
  const stock = await accentOf(page);
  const name = `Magenta ${Date.now().toString(36)}`;

  await createTheme(page, name, { accent: "#ff0066" });
  await goto(page, "/settings/themes");
  await page.waitForSelector(`[data-theme-row="${name}"] [data-action="theme-menu"]`, { timeout: WAIT });
  await page.click(`[data-theme-row="${name}"] [data-action="theme-menu"]`);
  await page.waitForSelector('[data-action="use-theme"]', { timeout: WAIT });
  await page.click('[data-action="use-theme"]');
  await page.waitForFunction(() => getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim() === "#ff0066", { timeout: WAIT });

  await reload(page);
  await waitForApp(page);
  expect.equal(await accentOf(page), "#ff0066", "the theme is back after a reload");

  // The light, dark and auto choice still cycles under a custom theme.
  const themeButton = '[data-action="theme"]';
  await page.click(themeButton);
  expect.equal(await textOf(page, themeButton), "Light", "cycles to light");
  await page.click(themeButton);
  expect.equal(await textOf(page, themeButton), "Dark", "cycles to dark");
  await page.click(themeButton);
  expect.equal(await textOf(page, themeButton), "Auto", "and back to auto");

  // Shared, a second person can use it; deleted, they fall back.
  await goto(page, "/settings/themes");
  await page.click(`[data-theme-row="${name}"] [data-action="theme-menu"]`);
  await page.waitForSelector('[data-action="share-theme"]', { timeout: WAIT });
  await page.click('[data-action="share-theme"]');
  await page.waitForFunction((n) => document.querySelector(`[data-theme-row="${n}"]`)?.innerText.includes("Shared"), { timeout: WAIT }, name);

  const who = await inviteMember(page, "themed");
  await acceptInvite(page, who);
  await waitForApp(page);
  expect.equal(await accentOf(page), stock, "somebody else starts with the built-in theme");
  await goto(page, "/settings/themes");
  await page.click('[data-themes-view="shared"]');
  await page.waitForSelector(`[data-theme-row="${name}"] [data-action="theme-menu"]`, { timeout: WAIT });
  await page.click(`[data-theme-row="${name}"] [data-action="theme-menu"]`);
  await page.waitForSelector('[data-action="use-theme"]', { timeout: WAIT });
  await page.click('[data-action="use-theme"]');
  await page.waitForFunction(() => getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim() === "#ff0066", { timeout: WAIT });

  await signIn(page, owner.email);
  await waitForApp(page);
  await goto(page, "/settings/themes");
  await page.click(`[data-theme-row="${name}"] [data-action="theme-menu"]`);
  await page.waitForSelector('[data-action="delete-theme"]', { timeout: WAIT });
  await page.click('[data-action="delete-theme"]');
  await confirm(page);
  await page.waitForFunction((n) => !document.querySelector(`[data-theme-row="${n}"]`), { timeout: WAIT }, name);
  await page.waitForFunction((s) => getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim() === s, { timeout: WAIT }, stock);

  await signIn(page, who.email);
  await waitForApp(page);
  expect.equal(await accentOf(page), stock, "the deleted theme let the other person go too");
});

scenario("the editor previews a draft on the page and takes it off again", async ({ page }) => {
  await signUp(page);
  const stock = await accentOf(page);
  await goto(page, "/settings/themes/new");
  await page.waitForSelector("#field-token-accent", { timeout: WAIT });
  await page.click("#field-token-accent", { clickCount: 3 });
  await page.keyboard.sendCharacter("#00aa55");
  await page.click('[data-action="preview-theme"]');
  await page.waitForFunction(() => getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim() === "#00aa55", { timeout: WAIT });
  await page.click('[data-action="preview-theme"]');
  await page.waitForFunction((s) => getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim() === s, { timeout: WAIT }, stock);
  const disabled = await page.$eval('[data-action="save-theme"]', (el) => el.disabled);
  expect.truthy(disabled, "a theme without a name cannot be saved");
});
