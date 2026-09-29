// The running sprint's burndown and what an issue carries: fields, files, time and labels.

import { writeFileSync } from "node:fs";
import { scenario } from "../runner.mjs";
import {
  clickButton,
  commitIssue,
  confirm,
  createIssue,
  createProject,
  createSprint,
  createToken,
  day,
  estimateIssue,
  expect,
  fill,
  goto,
  openFilters,
  planSprint,
  reload,
  selectByLabel,
  signUp,
  startSprint,
  textOf,
  WAIT,
  writeInEditor,
} from "../helpers.mjs";

scenario("the running sprint burns down on the dashboard and its history stays", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Burning");
  const big = await createIssue(page, key, "the five point piece", "Task");
  const small = await createIssue(page, key, "the three point piece", "Task");
  await createSprint(page, key, "Sprint A");
  await planSprint(page, "Sprint A", { from: "2026-09-07", to: "2026-09-18", capacity: 10 });
  await estimateIssue(page, big, 5);
  await estimateIssue(page, small, 3);
  await commitIssue(page, big, "Sprint A");
  await commitIssue(page, small, "Sprint A");
  await startSprint(page, key, "Sprint A");

  // The dashboard the project was born with draws the running sprint: the
  // start wrote the first day, and today is read live.
  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForSelector('[data-widget="burndown"] [data-burndown="Sprint A"] svg', { timeout: WAIT });
  let summary = await textOf(page, '[data-widget="burndown"] [data-burndown-summary]');
  expect.contains(summary, "8 of 8 pts left", "nothing is done yet");

  await goto(page, `/issues/${small}`);
  await clickButton(page, "Close");
  await page.waitForFunction(() => document.body.innerText.includes("Done"), { timeout: WAIT });
  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForFunction(
    () => document.querySelector('[data-widget="burndown"] [data-burndown-summary]')?.textContent.includes("5 of 8"),
    { timeout: WAIT },
  );
  summary = await textOf(page, '[data-widget="burndown"] [data-burndown-summary]');
  expect.contains(summary, "1/2 issues done", "the live point counts the finished issue");
  expect.truthy(await page.$('[data-widget="burndown"] [data-series="remaining"] path'), "the remaining line is drawn");
  expect.truthy(await page.$('[data-widget="burndown"] [data-series="ideal"] path'), "and the line it should follow");

  // Over, the sprint keeps what it came to, and its curve can still be asked for.
  await goto(page, `/projects/${key}/sprints`);
  await page.waitForSelector('[data-sprint="Sprint A"]', { timeout: WAIT });
  await clickButton(page, "Complete sprint");
  await page.waitForFunction(() => document.body.innerText.includes("Completed"), { timeout: WAIT });

  await goto(page, `/projects/${key}/dashboard`);
  await page.waitForSelector('[data-widget="burndown"]', { timeout: WAIT });
  await clickButton(page, "Arrange");
  await page.waitForSelector('[data-add-widget="sprint_history"]', { timeout: WAIT });
  await page.click('[data-add-widget="sprint_history"]');
  await page.waitForSelector('[data-widget="sprint_history"] [data-sprint-outcome="Sprint A"]', { timeout: WAIT });
  await clickButton(page, "Done arranging");
  const outcome = await textOf(page, '[data-widget="sprint_history"] [data-sprint-outcome="Sprint A"]');
  expect.contains(outcome, "1 finished, 1 carried", "the sprint remembers its issues as well as its points");
  expect.contains(outcome, "3 pts", "and the points it completed");
  await page.evaluate(() => {
    const row = document.querySelector('[data-sprint-outcome="Sprint A"]');
    [...row.querySelectorAll("button")].find((b) => b.textContent.trim() === "Curve")?.click();
  });
  await page.waitForSelector('[data-widget="sprint_history"] [data-burndown="Sprint A"] [data-series="remaining"] path', { timeout: WAIT });
  expect.truthy(
    !(await textOf(page, '[data-widget="sprint_history"] [data-burndown="Sprint A"]')).includes("today is live"),
    "a finished sprint's curve is what was written, not read live",
  );
});

scenario("an issue takes a description, the project's own fields and a file", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Recorded");

  // The project defines what it wants to record.
  await goto(page, `/projects/${key}/fields`);
  await clickButton(page, "New field");
  await fill(page, "Field name", "Customer");
  await selectByLabel(page, "#field-kind", "Select");
  await page.type("#field-options", "Acme\nGlobex");
  await clickButton(page, "Create field");
  await page.waitForSelector('[data-field="Customer"]', { timeout: WAIT });

  await clickButton(page, "New field");
  await fill(page, "Field name", "Cost");
  await selectByLabel(page, "#field-kind", "Number");
  await clickButton(page, "Create field");
  await page.waitForSelector('[data-field="Cost"]', { timeout: WAIT });

  // An issue is filed with a description.
  await goto(page, `/projects/${key}`);
  await clickButton(page, "New issue");
  await selectByLabel(page, "#issue-type", "Story");
  await fill(page, "Summary", "the story with a description");
  await page.type("#field-description", "As a customer I want to know why.\nDone means the why is written down.");
  await clickButton(page, "Create issue");
  await page.waitForFunction(() => document.body.innerText.includes("the story with a description"), { timeout: WAIT });
  const issueKey = await page.evaluate(() => {
    const row = [...document.querySelectorAll("a")].find((a) => a.textContent.includes("the story with a description"));
    return row?.getAttribute("href")?.split("/").pop() ?? "";
  });

  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector("[data-description]", { timeout: WAIT });
  expect.contains(await textOf(page, "[data-description]"), "Done means the why is written down.", "the description is shown");

  // And the description can be changed afterwards.
  await clickButton(page, "Edit");
  await writeInEditor(page, "#issue-description", "Rewritten after the refinement session.");
  await clickButton(page, "Save description");
  await page.waitForFunction(() => document.body.innerText.includes("Rewritten after the refinement session."), { timeout: WAIT });
  expect.truthy(!(await textOf(page, "[data-description]")).includes("As a customer"), "the old text is gone");

  // And a description saved empty is gone, not kept.
  await clickButton(page, "Edit");
  await writeInEditor(page, "#issue-description", "");
  await clickButton(page, "Save description");
  await page.waitForFunction(() => document.body.innerText.includes("No description yet."), { timeout: WAIT });
  await page.waitForFunction(() => document.body.innerText.includes("cleared the description"), { timeout: WAIT });

  // The fields are answered on the issue and land in the changelog by name.
  await page.waitForSelector('[data-custom-field="Customer"] select', { timeout: WAIT });
  await selectByLabel(page, '[data-custom-field="Customer"] select', "Globex");
  await page.waitForFunction(() => document.body.innerText.includes("changed Customer from empty to Globex"), { timeout: WAIT });
  await page.type('[data-custom-field="Cost"] input', "1200");
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => document.body.innerText.includes("changed Cost from empty to 1200"), { timeout: WAIT });

  // A file goes up, comes back byte for byte, and can be taken away.
  const notes = `notes for ${issueKey}\nline two\n`;
  writeFileSync("/tmp/e2e-notes.txt", notes);
  const picker = await page.$('input[type=file][aria-label="Attach a file"]');
  await picker.uploadFile("/tmp/e2e-notes.txt");
  await page.waitForSelector('[data-attachment="e2e-notes.txt"]', { timeout: WAIT });
  expect.contains(await textOf(page, '[data-attachment="e2e-notes.txt"]'), "B", "the size is shown");
  const fetched = await page.evaluate(async () => {
    const link = document.querySelector('[data-attachment="e2e-notes.txt"] a');
    const response = await fetch(link.getAttribute("href"));
    return { status: response.status, text: await response.text(), disposition: response.headers.get("content-disposition") };
  });
  expect.equal(fetched.status, 200, "the file downloads");
  expect.equal(fetched.text, notes, "the bytes are the ones that went up");
  expect.contains(fetched.disposition ?? "", "e2e-notes.txt", "the download keeps its name");
  await page.waitForFunction(() => document.body.innerText.includes("attached e2e-notes.txt"), { timeout: WAIT });

  await page.click('[aria-label="Remove e2e-notes.txt"]');
  await confirm(page);
  await page.waitForFunction(() => document.querySelector('[data-attachment="e2e-notes.txt"]') === null, { timeout: WAIT });
  await page.waitForFunction(() => document.body.innerText.includes("removed e2e-notes.txt"), { timeout: WAIT });

  // What was answered is what the issue keeps.
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector('[data-custom-field="Customer"] select', { timeout: WAIT });
  expect.equal(await page.$eval('[data-custom-field="Customer"] select', (el) => el.value), "Globex", "the answer survives a reload");
  expect.equal(await page.$eval('[data-custom-field="Cost"] input', (el) => el.value), "1200", "so does the number");
});

scenario("an issue is assigned, labelled, estimated in time and worked on", async ({ page }) => {
  const who = await signUp(page);
  const key = await createProject(page, "Timed");
  const issueKey = await createIssue(page, key, "needs hours and words");
  await goto(page, `/issues/${issueKey}`);

  // Assign it to the only person there is, and hand the report to them too.
  await selectByLabel(page, 'select[aria-label="Assignee"]', who.name);
  await page.waitForFunction((n) => document.body.innerText.includes(`assigned this to ${n}`), { timeout: WAIT }, who.name);
  await page.waitForSelector('select[aria-label="Reporter"]', { timeout: 10_000 });

  // Labels are coined by typing them; the second one is offered back.
  await page.type('input[aria-label="Add a label"]', "backend");
  await page.keyboard.press("Enter");
  await page.waitForSelector('[data-labels] [data-label="backend"]', { timeout: WAIT });
  await page.type('input[aria-label="Add a label"]', "urgent,");
  await page.waitForSelector('[data-labels] [data-label="urgent"]', { timeout: WAIT });
  await page.waitForFunction(() => document.body.innerText.includes("labelled this backend, urgent"), { timeout: WAIT });

  // Time: an estimate written the way people write it, then work logged
  // against it, which brings the remaining time down.
  await page.click('input[aria-label="Estimated"]', { clickCount: 3 });
  await page.type('input[aria-label="Estimated"]', "1d");
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => document.body.innerText.includes("estimated this at 1d"), { timeout: WAIT });
  await page.type("#worklog-duration", "2h 30m");
  await page.type("#worklog-note", "read the spec");
  await clickButton(page, "Log work");
  await page.waitForSelector("[data-worklog]", { timeout: WAIT });
  expect.equal(await textOf(page, "[data-time-spent]"), "2h 30m", "the spent time is the log");
  expect.equal(await page.$eval('input[aria-label="Remaining"]', (el) => el.value), "5h 30m", "what remains came down");

  // Words that are not time are refused with the field named, not saved.
  await page.click('input[aria-label="Remaining"]', { clickCount: 3 });
  await page.type('input[aria-label="Remaining"]', "a while");
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => document.body.innerText.includes("Remaining takes time such as"), { timeout: WAIT });

  // The label shows in the project's list, and the list filters by it.
  await goto(page, `/projects/${key}`);
  await page.waitForSelector('[data-label="backend"]', { timeout: WAIT });
  await openFilters(page);
  await selectByLabel(page, 'select[aria-label="Filter by label"]', "urgent (1)");
  await page.waitForFunction(() => document.body.innerText.includes("1 issue"), { timeout: WAIT });

  // And a label is the organization's: it is listed, and can be renamed everywhere.
  await goto(page, "/settings/labels");
  await page.waitForSelector('[data-label-row="urgent"]', { timeout: WAIT });
  await page.evaluate(() => {
    const row = document.querySelector('[data-label-row="urgent"]');
    [...row.querySelectorAll("button")].find((b) => b.textContent.trim() === "Edit")?.click();
  });
  await page.waitForSelector("input#label-name-" + (await page.$eval('[data-label-row="urgent"] input', (el) => el.id.replace("label-name-", ""))), { timeout: 10_000 });
  await page.click('[data-label-row="urgent"] input', { clickCount: 3 });
  await page.type('[data-label-row="urgent"] input', "blocker");
  await clickButton(page, "Save label");
  await page.waitForSelector('[data-label-row="blocker"]', { timeout: WAIT });
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector('[data-labels] [data-label="blocker"]', { timeout: WAIT });
});

scenario("a page another application puts on an issue is listed under Pages", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Specced");
  const issueKey = await createIssue(page, key, "the checkout flow", "Story");
  const secret = await createToken(page, "Page sync");
  const spec = "https://wiki.example.com/pages/checkout";

  // The application syncs over the API with a token, and sends the same page
  // twice: the second time it has been retitled.
  const put = (title) =>
    page.evaluate(
      async (bearer, target, url, pageTitle) => {
        const response = await fetch(`/api/v1/issues/${target}/remote-links`, {
          method: "POST",
          headers: { Authorization: `Bearer ${bearer}`, "Content-Type": "application/json" },
          body: JSON.stringify({ url, title: pageTitle, source: "Stator" }),
        });
        return response.status;
      },
      secret,
      issueKey,
      spec,
      title,
    );
  expect.equal(await put("Checkout spec"), 201, "the page is put on the issue");
  expect.equal(await put("Checkout spec, second draft"), 200, "the same page again is retitled");

  await goto(page, `/issues/${issueKey}`);
  const row = `[data-testid="pages-panel"] [data-remote-link="${spec}"]`;
  await page.waitForSelector(row, { timeout: WAIT });
  expect.equal(await page.$$eval("[data-remote-link]", (rows) => rows.length), 1, "one row for the page");
  const link = await page.$eval(`${row} a`, (a) => ({ text: a.textContent.trim(), href: a.getAttribute("href"), target: a.getAttribute("target"), rel: a.getAttribute("rel") }));
  expect.equal(link.text, "Checkout spec, second draft", "the row carries the latest title");
  expect.equal(link.href, spec, "and goes to the page");
  expect.equal(link.target, "_blank", "in a new tab");
  expect.contains(link.rel, "noopener", "without handing the page this one");
  expect.contains(await textOf(page, row), "Stator", "the row names where the page lives");

  // Somebody who edits the issue can take the page off, and the section goes with it.
  await page.click(`${row} button[aria-label="Remove the page Checkout spec, second draft"]`);
  await page.waitForFunction(() => !document.querySelector('[data-testid="pages-panel"]'), { timeout: WAIT });
});
