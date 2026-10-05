// The days somebody is away: recorded by the person, or by whoever runs their team.

import { scenario } from "../runner.mjs";
import {
  createLocalUser,
  createProject,
  createTeam,
  day,
  goto,
  joinTeam,
  openMonth,
  PASSWORD,
  setDate,
  signIn,
  signUp,
  textOf,
  unique,
  expect,
  WAIT,
  waitForApp,
} from "../helpers.mjs";

// How many days from now the absences fall, far enough ahead to be listed as coming.
const AWAY_FIRST_DAY = 10;
const AWAY_LAST_DAY = 12;
const TEAM_AWAY_DAY = 20;

/** Records an absence in the form inside scope and waits for its row to be listed. */
async function recordAbsence(page, scope, from, to) {
  await setDate(page, `${scope} [data-absence-from]`, from);
  await setDate(page, `${scope} [data-absence-to]`, to);
  await page.waitForFunction((sel) => !document.querySelector(`${sel} [data-action="record-absence"]`)?.disabled, { timeout: WAIT }, scope);
  await page.click(`${scope} [data-action="record-absence"]`);
  await page.waitForSelector(`${scope} [data-absence="${from}"]`, { timeout: WAIT });
}

scenario("a person records an absence on their profile and sees it listed", async ({ page }) => {
  await signUp(page);
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-absence-form]", { timeout: WAIT });
  await recordAbsence(page, "[data-absences]", day(AWAY_FIRST_DAY), day(AWAY_LAST_DAY));
  const listed = await textOf(page, `[data-absence="${day(AWAY_FIRST_DAY)}"]`);
  expect.contains(listed, " to ", "the absence reads as a first and a last day");
});

scenario("a scrum master records an absence for somebody on their team", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Away crew", "scrum");
  const master = unique("master");
  const member = unique("member");
  await createLocalUser(page, { email: master.email, name: "Sam Master" });
  await createLocalUser(page, { email: member.email, name: "Tea Member" });
  await createTeam(page, key, "Crew");
  await joinTeam(page, key, "Crew", "Tea Member");

  // Granted through the API: the roles page has scenarios of its own.
  await page.evaluate(
    async (projectKey, email) => {
      const { members } = await (await fetch("/api/v1/members", { credentials: "include" })).json();
      const userId = members.find((m) => m.email === email).id;
      const granted = await fetch("/api/v1/role-assignments", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: "scrum_master", projectKey, userId }),
      });
      if (!granted.ok) throw new Error(`grant failed: ${granted.status}`);
    },
    key,
    master.email,
  );

  await signIn(page, master.email, PASSWORD);
  await waitForApp(page);
  await goto(page, `/projects/${key}/teams`);
  await page.waitForSelector('[data-team-member="Tea Member"] [data-action="record-member-absence"]', { timeout: WAIT });
  await page.click('[data-team-member="Tea Member"] [data-action="record-member-absence"]');
  await page.waitForSelector("[data-absence-dialog] [data-absence-form]", { timeout: WAIT });
  await recordAbsence(page, "[data-absence-dialog]", day(TEAM_AWAY_DAY), day(TEAM_AWAY_DAY));

  await signIn(page, member.email, PASSWORD);
  await waitForApp(page);
  await goto(page, "/settings/profile");
  await page.waitForSelector(`[data-absences] [data-absence="${day(TEAM_AWAY_DAY)}"]`, { timeout: WAIT });
});

scenario("a person records an absence and the project calendar shows them away", async ({ page }) => {
  const who = await signUp(page);
  const key = await createProject(page, "Away on the calendar");
  await createTeam(page, key, "Crew");
  await joinTeam(page, key, "Crew", who.name);
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-absence-form]", { timeout: WAIT });
  const first = day(AWAY_FIRST_DAY);
  await recordAbsence(page, "[data-absences]", first, day(AWAY_LAST_DAY));

  await openMonth(page, key, first.slice(0, 7));
  const chip = `[data-day="${first}"] [data-calendar-kind="absence"]`;
  await page.waitForSelector(chip, { timeout: WAIT });
  expect.equal(await textOf(page, chip), `${who.name} away`, "the chip says who, and that they are away");

  // The toggle hides them, and shows them again.
  await page.click('[data-action="calendar-days-off"]');
  await page.waitForFunction((sel) => !document.querySelector(sel), { timeout: WAIT }, chip);
  await page.click('[data-action="calendar-days-off"]');
  await page.waitForSelector(chip, { timeout: WAIT });
});
