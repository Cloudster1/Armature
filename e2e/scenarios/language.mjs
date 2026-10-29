// The pages a person reads speak the language their profile names (#371).

import { scenario } from "../runner.mjs";
import { bodyText, createIssue, createProject, expect, goto, signOut, signUp, speakThroughAPI, textOf, WAIT } from "../helpers.mjs";

// The issue list, an issue's page and its panels speak the person's language (#371).
scenario("a person whose profile says German reads the issue list and an issue's page in German", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Deutsch", { visit: false });
  const issueKey = await createIssue(page, key, "Ein Vorgang auf Deutsch", { visit: false });
  await speakThroughAPI(page, "de-DE");

  await goto(page, `/projects/${key}`);
  await page.waitForFunction(() => document.documentElement.lang === "de", { timeout: WAIT });
  await page.waitForSelector(`a[href="/issues/${issueKey}"]`, { timeout: WAIT });
  const list = await bodyText(page);
  expect.containsInsensitive(list, "Zusammenfassung", "the list's summary column");
  expect.notContains(list, "SUMMARY", "the English column heading");

  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector("[data-description]", { timeout: WAIT });
  const view = await bodyText(page);
  for (const word of ["Beschreibung", "Aktivität", "Verlauf", "Bearbeiter"]) expect.containsInsensitive(view, word, `the issue's page says ${word}`);
  expect.notContains(view, "Activity", "the English section name");
});

// The work pages speak the person's language too (#371): the board, search and the milestones.
scenario("a person whose profile says German works on the board, in search and among milestones in German", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Arbeit", { visit: false });
  await speakThroughAPI(page, "de-DE");

  await goto(page, `/projects/${key}/board`);
  await page.waitForFunction(() => document.documentElement.lang === "de", { timeout: WAIT });
  await page.waitForSelector('input[aria-label="Karten nach Schlüssel oder Zusammenfassung filtern"]', { timeout: WAIT });

  await goto(page, "/search");
  await page.waitForFunction(() => document.body.innerText.includes("Jeder Vorgang, den Sie sehen können"), { timeout: WAIT });

  await goto(page, `/projects/${key}/milestones`);
  await page.waitForFunction(() => document.body.innerText.includes("Noch keine Meilensteine"), { timeout: WAIT });
  expect.notContains(await bodyText(page), "No milestones yet", "the English empty state");
});

// Settings and the sign-in page speak the person's language too (#371). A tab's
// hook stays English, so a scenario finds it whatever the reader sees.
scenario("a person whose profile says German sets things up and signs in again in German", async ({ page }) => {
  await signUp(page);
  await speakThroughAPI(page, "de-DE");

  await goto(page, "/admin/access");
  await page.waitForFunction(() => document.documentElement.lang === "de", { timeout: WAIT });
  await page.waitForSelector('[data-access-tab="Single sign-on"]', { timeout: WAIT });
  expect.equal(await textOf(page, '[data-access-tab="Single sign-on"]'), "Zentrale Anmeldung", "the sign-on tab's words");
  expect.equal(await textOf(page, '[data-access-tab="Roles"]'), "Rollen", "the roles tab's words");

  // The sign-in page has nobody yet; it paints in the language this browser remembers.
  await signOut(page);
  await page.waitForFunction(() => document.querySelector('[data-action="password-sign-in"]')?.textContent.trim() === "Mit Passwort anmelden", {
    timeout: WAIT,
  });
  expect.equal(await textOf(page, '[data-action="single-sign-on"]'), "Mit SSO anmelden", "the single sign-on button's words");
  expect.containsInsensitive(await bodyText(page), "Passwort vergessen?", "the way back in");
});

// The service desk's queues and the guide speak German too (#371): a German
// question in the guide finds the page it asks about. A desk has no plan, so
// the question is one every project answers.
scenario("a person whose profile says German reads the queues and asks the guide in German", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Hilfe", { template: "service-desk", visit: false });
  await speakThroughAPI(page, "de-DE");

  await goto(page, `/projects/${key}/queues`);
  await page.waitForFunction(() => document.documentElement.lang === "de", { timeout: WAIT });
  await page.waitForFunction(() => document.body.innerText.includes("Warteschlangen"), { timeout: WAIT });

  await page.click('[data-action="guide"]');
  await page.waitForSelector('[data-palette-mode="ask"] input', { timeout: WAIT });
  await page.type('[data-palette-mode="ask"] input', "Wie erstelle ich ein Zugriffstoken?");
  await page.waitForSelector('[data-palette-option="answer:action:create-token"]', { timeout: WAIT });
});
