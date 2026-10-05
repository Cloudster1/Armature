// The resource view: work per team or person per week, against the hours left.

import { scenario } from "../runner.mjs";
import { createProject, createTeam, expect, goto, joinTeam, signUp, WAIT } from "../helpers.mjs";

const DAY_MS = 86_400_000;
const DAYS_PER_WEEK = 7;
// The absence falls on the Wednesday of next week, inside the window the page opens on.
const WEDNESDAY = 2;

/** The Monday a number of weeks from this one, as the page writes days. */
function monday(weeksAhead) {
  const now = new Date();
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const back = (new Date(today).getUTCDay() + DAYS_PER_WEEK - 1) % DAYS_PER_WEEK;
  return new Date(today + (weeksAhead * DAYS_PER_WEEK - back) * DAY_MS).toISOString().slice(0, 10);
}

function plus(isoDay, days) {
  return new Date(Date.parse(isoDay) + days * DAY_MS).toISOString().slice(0, 10);
}

/** What a person's week reads as in the grid: its hours and its days away. */
async function weekOf(page, person, start) {
  const cell = `[data-resource-row="${person}"] [data-resource-week="${start}"]`;
  await page.waitForSelector(cell, { timeout: WAIT });
  return page.$eval(cell, (el) => ({ capacity: el.dataset.capacity, away: el.dataset.daysAway, title: el.getAttribute("title") }));
}

scenario("a kanban project switches to people and sees a person's week shortened by their absence", async ({ page }) => {
  const who = await signUp(page);
  const key = await createProject(page, "Resourced", "kanban");
  await createTeam(page, key, "Crew");
  await joinTeam(page, key, "Crew", who.name);

  // Recorded through the API: the absence form has scenarios of its own.
  const away = plus(monday(1), WEDNESDAY);
  await page.evaluate(async (day) => {
    const me = await (await fetch("/api/v1/auth/me", { credentials: "include" })).json();
    const made = await fetch("/api/v1/absences", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ userId: me.principal.user.id, startsOn: day, endsOn: day }),
    });
    if (!made.ok) throw new Error(`absence failed: ${made.status}`);
  }, away);

  await goto(page, `/projects/${key}/resources`);
  await page.waitForSelector('[data-resources="team"] [data-resource-row="Crew"]', { timeout: WAIT });
  await page.click('[data-action="group-by-person"]');
  await page.waitForSelector(`[data-resources="person"] [data-resource-row="${who.name}"]`, { timeout: WAIT });

  const shortened = await weekOf(page, who.name, monday(1));
  expect.equal(shortened.capacity, "32", "four of the five days are left");
  expect.equal(shortened.away, "1", "the week counts the day away");
  expect.contains(shortened.title, "0 of 32 h; 1 day away", "the cell says why");
  expect.equal((await weekOf(page, who.name, monday(2))).capacity, "40", "the week after is whole");

  // The choice is the project's: it is still people after a reload.
  await goto(page, `/projects/${key}/resources`);
  await page.waitForSelector(`[data-resources="person"] [data-resource-row="${who.name}"]`, { timeout: WAIT });
});

scenario("a scrum project offers only teams", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Sprinted", "scrum");
  await goto(page, `/projects/${key}/resources`);
  await page.waitForSelector('[data-resource-grouping="team"]', { timeout: WAIT });
  expect.equal(await page.$('[data-action="group-by-person"]'), null, "no switch to people");
  expect.contains(await page.$eval('[data-resource-grouping="team"]', (el) => el.textContent), "Planned by team", "the page says how it plans");

  await goto(page, `/projects/${key}/settings`);
  await page.waitForSelector('[data-planning-settings] [data-resource-grouping="person"]', { timeout: WAIT });
  const person = await page.$eval('[data-planning-settings] [data-resource-grouping="person"]', (el) => ({ disabled: el.disabled, text: el.textContent }));
  expect.equal(person.disabled, true, "people cannot be chosen");
  expect.contains(person.text, "Switch the project to kanban", "and the card says why");
});
