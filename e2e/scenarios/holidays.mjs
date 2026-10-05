// Calendars of days off, and the working week each person keeps.

import { scenario } from "../runner.mjs";
import {
  clickButton,
  createLocalUser,
  expect,
  goto,
  PASSWORD,
  replaceValue,
  selectByLabel,
  signIn,
  signUp,
  textOf,
  unique,
  WAIT,
  waitForApp,
} from "../helpers.mjs";

// The .ics kept under e2e/fixtures is what a calendar application exports: a
// folded name and an event over three days, each of which becomes a day off.
scenario("an administrator imports an .ics and sees its days on the default calendar", async ({ page }) => {
  await signUp(page);
  await goto(page, "/settings/holidays");
  await page.waitForSelector('[data-calendar-row="Holidays"] [data-default-calendar]', { timeout: WAIT });
  await page.waitForSelector("[data-holiday-file]", { timeout: WAIT });
  const input = await page.$("[data-holiday-file]");
  await input.uploadFile(new URL("../fixtures/holidays.ics", import.meta.url).pathname);

  for (const day of ["2026-12-24", "2026-12-25", "2026-12-26", "2027-01-01"]) {
    await page.waitForSelector(`[data-holiday-day="${day}"]`, { timeout: WAIT });
  }
  const name = await page.$eval('[data-holiday-day="2026-12-25"] input[aria-label="Name"]', (el) => el.value);
  expect.equal(name, "Christmas break", "a folded name is read whole");
  await page.waitForFunction(
    () => document.querySelector('[data-calendar-row="Holidays"] td:nth-child(2)')?.textContent.trim() === "4",
    { timeout: WAIT },
  );
});

scenario("an administrator sets somebody's working week and they read it on their profile", async ({ page }) => {
  await signUp(page);
  const who = unique("parttime");
  await createLocalUser(page, { email: who.email, name: who.name });

  await page.click(`[data-user="${who.email}"] [data-action="user-menu"]`);
  await page.waitForSelector('[data-action="working-week"]', { timeout: WAIT });
  await page.click('[data-action="working-week"]');
  await page.waitForSelector('[data-week-day="fri"]', { timeout: WAIT });
  await replaceValue(page, '[data-week-day="fri"]', "4");
  await selectByLabel(page, "#field-holiday-calendar", "Holidays");
  await clickButton(page, "Save week");
  await page.waitForFunction(() => !document.querySelector("[data-working-week-dialog]"), { timeout: WAIT });

  await signIn(page, who.email, PASSWORD);
  await waitForApp(page);
  await goto(page, "/settings/profile");
  await page.waitForSelector('[data-week-hours="fri"]', { timeout: WAIT });
  expect.equal(await textOf(page, '[data-week-hours="fri"]'), "4 h", "the profile shows the hours set");
  expect.equal(await textOf(page, '[data-week-hours="mon"]'), "8 h", "the other days keep the standard week");
  expect.equal(await textOf(page, "[data-week-calendar]"), "Holidays", "the profile names the calendar");
});
