// The days somebody is away: recorded by the person, or by whoever runs their team.

import { scenario } from "../runner.mjs";
import {
  confirm,
  createLocalUser,
  createProject,
  createTeam,
  day,
  eventually,
  goto,
  joinTeam,
  mailFor,
  openMonth,
  PASSWORD,
  reload,
  setDate,
  signIn,
  signUp,
  textOf,
  unique,
  expect,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

// How many days from now the absences fall, far enough ahead to be listed as coming.
const AWAY_FIRST_DAY = 10;
const AWAY_LAST_DAY = 12;
const TEAM_AWAY_DAY = 20;
// Fourteen months ahead: past the year the list once stopped at.
const FAR_AHEAD_DAY = 426;

/** Records an absence in the form inside scope and waits for its row to be listed. */
async function recordAbsence(page, scope, from, to) {
  await setDate(page, `${scope} [data-absence-from]`, from);
  await setDate(page, `${scope} [data-absence-to]`, to);
  await page.waitForFunction((sel) => !document.querySelector(`${sel} [data-action="record-absence"]`)?.disabled, { timeout: WAIT }, scope);
  await page.click(`${scope} [data-action="record-absence"]`);
  await page.waitForSelector(`${scope} [data-absence="${from}"]`, { timeout: WAIT });
}

/** Makes somebody scrum master of a project through the API: the roles page has scenarios of its own. */
async function grantScrumMaster(page, projectKey, email) {
  await page.evaluate(
    async (key, address) => {
      const { members } = await (await fetch("/api/v1/members", { credentials: "include" })).json();
      const userId = members.find((m) => m.email === address).id;
      const granted = await fetch("/api/v1/role-assignments", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: "scrum_master", projectKey: key, userId }),
      });
      if (!granted.ok) throw new Error(`grant failed: ${granted.status}`);
    },
    projectKey,
    email,
  );
}

scenario("a person records an absence on their profile and sees it listed", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-absence-form]", { timeout: WAIT });
  await recordAbsence(page, "[data-absences]", day(AWAY_FIRST_DAY), day(AWAY_LAST_DAY));
  const listed = await textOf(page, `[data-absence="${day(AWAY_FIRST_DAY)}"]`);
  expect.contains(listed, " to ", "the absence reads as a first and a last day");
});

scenario("a person records an absence fourteen months ahead and it stays listed", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-absence-form]", { timeout: WAIT });
  const far = day(FAR_AHEAD_DAY);
  await recordAbsence(page, "[data-absences]", far, far);
  await reload(page);
  await page.waitForSelector(`[data-absences] [data-absence="${far}"]`, { timeout: WAIT });
});

scenario("a scrum master records an absence for somebody on their team", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Away crew", "scrum");
  const master = unique("master");
  const member = unique("member");
  await createLocalUser(page, master);
  await createLocalUser(page, member);
  await createTeam(page, key, "Crew");
  await joinTeam(page, key, "Crew", member.name);
  await grantScrumMaster(page, key, master.email);

  await signIn(page, master.email, PASSWORD);
  await waitForApp(page);
  await goto(page, `/projects/${key}/teams`);
  await page.waitForSelector(`[data-team-member="${member.name}"] [data-action="record-member-absence"]`, { timeout: WAIT });
  await page.click(`[data-team-member="${member.name}"] [data-action="record-member-absence"]`);
  await page.waitForSelector("[data-absence-dialog] [data-absence-form]", { timeout: WAIT });
  await recordAbsence(page, "[data-absence-dialog]", day(TEAM_AWAY_DAY), day(TEAM_AWAY_DAY));

  await signIn(page, member.email, PASSWORD);
  await waitForApp(page);
  await goto(page, "/settings/profile");
  await page.waitForSelector(`[data-absences] [data-absence="${day(TEAM_AWAY_DAY)}"]`, { timeout: WAIT });
});

scenario("a person is told in their inbox when a scrum master records an absence for them, and keeps their own out of reach", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Told crew", "scrum");
  const master = unique("master");
  const member = unique("member");
  await createLocalUser(page, master);
  await createLocalUser(page, member);
  await createTeam(page, key, "Crew");
  await joinTeam(page, key, "Crew", member.name);
  await grantScrumMaster(page, key, master.email);

  // The member writes down their own days first.
  await signIn(page, member.email, PASSWORD);
  await waitForApp(page);
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-absence-form]", { timeout: WAIT });
  await recordAbsence(page, "[data-absences]", day(AWAY_FIRST_DAY), day(AWAY_LAST_DAY));

  // The scrum master adds a day for them, and is refused taking theirs away.
  await signIn(page, master.email, PASSWORD);
  await waitForApp(page);
  await goto(page, `/projects/${key}/teams`);
  await page.waitForSelector(`[data-team-member="${member.name}"] [data-action="record-member-absence"]`, { timeout: WAIT });
  await page.click(`[data-team-member="${member.name}"] [data-action="record-member-absence"]`);
  await page.waitForSelector("[data-absence-dialog] [data-absence-form]", { timeout: WAIT });
  const before = Date.now();
  await recordAbsence(page, "[data-absence-dialog]", day(TEAM_AWAY_DAY), day(TEAM_AWAY_DAY));
  await page.click(`[data-absence-dialog] [data-absence="${day(AWAY_FIRST_DAY)}"] [data-action="remove-absence"]`);
  await confirm(page);
  await page.waitForFunction(
    (name) => document.querySelector("[data-absence-dialog]")?.innerText.includes(`Only ${name} or an administrator can change this absence.`),
    { timeout: WAIT },
    member.name,
  );
  await page.waitForSelector(`[data-absence-dialog] [data-absence="${day(AWAY_FIRST_DAY)}"]`, { timeout: WAIT });

  // The member is mailed and finds a notice in the inbox; neither says which days.
  const mail = await mailFor(member.email, master.name, { since: before });
  expect.contains(mail.text, "/settings/profile", "the mail points at her profile");
  expect.equal(mail.text.includes(day(TEAM_AWAY_DAY)), false, "the mail does not carry the days");
  await signIn(page, member.email, PASSWORD);
  await waitForApp(page);
  await goto(page, "/inbox");
  const notice = '[data-notification="absence"]';
  await eventually(page, notice);
  const told = await textOf(page, notice);
  expect.contains(told, `${master.name} recorded an absence for you`, "the notice says who did what");
  expect.equal(told.includes(day(TEAM_AWAY_DAY)), false, `the notice carries no days: ${told}`);
  await page.click(notice);
  await waitForPath(page, "/settings/profile");
  await page.waitForSelector(`[data-absences] [data-absence="${day(TEAM_AWAY_DAY)}"]`, { timeout: WAIT });
  await page.waitForSelector(`[data-absences] [data-absence="${day(AWAY_FIRST_DAY)}"]`, { timeout: WAIT });
});

scenario("a person records an absence and the project calendar shows them away", async ({ page }) => {
  const who = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Away on the calendar", { visit: false });
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
