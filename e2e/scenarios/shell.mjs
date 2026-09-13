// What the application does before anybody is anywhere in particular.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  expect,
  fill,
  goto,
  PASSWORD,
  reload,
  signUp,
  submit,
  textOf,
  unique,
  WAIT,
  waitForPath,
} from "../helpers.mjs";

scenario("an anonymous visitor is sent to sign in, not shown the app", async ({ page }) => {
  await goto(page, "/");
  await waitForPath(page, "/login");

  await goto(page, "/settings/tokens");
  await waitForPath(page, "/login");
});

scenario("an unknown path shows the not-found page rather than a blank screen", async ({ page }) => {
  await goto(page, "/no-such-page");
  expect.contains(await bodyText(page), "Page not found", "not-found page");
});

scenario("form validation is reported against the field, not swallowed", async ({ page }) => {
  const who = unique("dupe");
  await signUp(page, who);
  await page.click('[data-action="sign-out"]');
  await waitForPath(page, "/login");

  // The same address a second time must be reported, not silently accepted.
  await goto(page, "/signup");
  await fill(page, "Your name", "Someone Else");
  await fill(page, "Work email", who.email);
  await fill(page, "Password", PASSWORD);
  await fill(page, "Organization name", `Another ${Date.now()}`);
  await submit(page);

  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains((await textOf(page, "[role=alert]")).toLowerCase(), "already exists", "duplicate email message");
});

scenario("the theme choice persists across a reload", async ({ page }) => {
  await signUp(page);

  const themeButton = '[data-action="theme"]';
  expect.equal(await textOf(page, themeButton), "Auto", "starts following the system");

  await page.click(themeButton);
  expect.equal(await textOf(page, themeButton), "Light", "cycles to light");
  await page.click(themeButton);
  expect.equal(await textOf(page, themeButton), "Dark", "cycles to dark");
  expect.equal(
    await page.evaluate(() => document.documentElement.getAttribute("data-theme")),
    "dark",
    "the document is stamped with the theme",
  );

  await reload(page);
  await page.waitForSelector(themeButton, { timeout: 10_000 });
  expect.equal(await textOf(page, themeButton), "Dark", "the choice survived the reload");
});

scenario("nothing in the page reports a script error", async ({ page }) => {
  const problems = [];
  page.on("console", (message) => {
    if (message.type() === "error") problems.push(message.text());
  });
  page.on("pageerror", (error) => problems.push(error.message));

  await signUp(page);
  await page.click('a[href="/settings/tokens"]');
  await waitForPath(page, "/settings/tokens");
  await goto(page, "/");

  // A 401 during the anonymous phase of loading is expected and logged by the
  // browser itself, so only genuine script failures count.
  const real = problems.filter((p) => !p.includes("401") && !p.includes("Failed to load resource"));
  expect.equal(real.length, 0, `console errors: ${real.join(" | ")}`);
});
