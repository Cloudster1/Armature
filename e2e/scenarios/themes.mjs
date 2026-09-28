// Custom themes: made, previewed, used, shared and deleted.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  clickButton,
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
  await goto(page, "/");
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

// The product ships a theme to start from; picking it fills the draft, the
// preview paints the page in its colours, and saving keeps it as one's own.
scenario("a new theme starts from the shipped Deep-Tech example", async ({ page }) => {
  await signUp(page);
  await goto(page, "/settings/themes/new");
  await page.waitForSelector('[data-theme-example="deep-tech"]', { timeout: WAIT });
  await page.click('[data-theme-example="deep-tech"]');
  await page.waitForFunction(() => document.querySelector('[data-theme-example="deep-tech"]')?.getAttribute("aria-checked") === "true", { timeout: WAIT });
  const named = await page.$eval("#field-theme-name", (el) => el.value);
  expect.equal(named, "Deep-Tech", "the example names the draft");

  await page.click('[data-action="preview-theme"]');
  await page.waitForFunction(() => {
    const accent = getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim().toLowerCase();
    return accent === "#2f6fd6" || accent === "#5cc8ff";
  }, { timeout: WAIT });

  await clickButton(page, "Save theme");
  await page.waitForFunction(() => /\/settings\/themes\/[0-9a-f-]{36}$/.test(location.pathname), { timeout: WAIT });
  await goto(page, "/settings/themes");
  await page.waitForSelector('[data-theme-row="Deep-Tech"]', { timeout: WAIT });
});

// Constellation's network is drawn live rather than printed, so the shell
// mounts a canvas for it, while the theme is previewed and once it is used.
scenario("Constellation draws its network live behind the page", async ({ page }) => {
  await signUp(page);
  const before = await page.$("[data-backdrop-effect]");
  expect.truthy(before === null, "the built-in theme draws nothing behind the page");

  await goto(page, "/settings/themes/new");
  await page.waitForSelector('[data-theme-example="constellation"]', { timeout: WAIT });
  await page.click('[data-theme-example="constellation"]');
  await page.click('[data-action="preview-theme"]');
  await page.waitForSelector('[data-backdrop-effect="constellation"] canvas', { timeout: WAIT });
  await page.click('[data-action="preview-theme"]');
  await page.waitForFunction(() => !document.querySelector("[data-backdrop-effect]"), { timeout: WAIT });

  await clickButton(page, "Save theme");
  await page.waitForFunction(() => /\/settings\/themes\/[0-9a-f-]{36}$/.test(location.pathname), { timeout: WAIT });
  await clickButton(page, "Use this theme");
  await page.waitForSelector('[data-backdrop-effect="constellation"] canvas', { timeout: WAIT });
  const drawn = await page.$eval('[data-backdrop-effect="constellation"] canvas', (c) => {
    const data = c.getContext("2d").getImageData(0, 0, c.width, c.height).data;
    let lit = 0;
    for (let i = 3; i < data.length; i += 4) if (data[i] > 0) lit++;
    return { w: c.width, h: c.height, lit };
  });
  expect.truthy(drawn.w > 0 && drawn.h > 0 && drawn.lit > 0, `the canvas covers the page and something is drawn on it: ${JSON.stringify(drawn)}`);
});
