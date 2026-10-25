// The running sprint's burndown and what an issue carries: fields, files, time and labels.

import { writeFileSync } from "node:fs";
import { scenario, serialScenario } from "../runner.mjs";
import {
  bodyText,
  changeType,
  clickButton,
  commitIssue,
  confirm,
  createIssue,
  createProject,
  createSprint,
  createToken,
  day,
  editorText,
  estimateIssue,
  expect,
  fill,
  followWorkflow,
  goto,
  issueRows,
  openFilters,
  openIssueBeside,
  patchThroughAPI,
  speakThroughAPI,
  planSprint,
  reload,
  pickPerson,
  selectByLabel,
  settled,
  signUp,
  startSprint,
  textOf,
  unique,
  uniqueName,
  WAIT,
  waitForButtonToGo,
  withViewport,
  writeInEditor,
} from "../helpers.mjs";

/** Wide enough for the issue panel to dock beside the list rather than cover it. */
const DOCKED_VIEWPORT_WIDTH = 1600;

scenario("the running sprint burns down on the dashboard and its history stays", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Burning", { visit: false });
  const big = await createIssue(page, key, "the five point piece", { type: "Task", visit: false });
  const small = await createIssue(page, key, "the three point piece", { type: "Task", visit: false });
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
  await page.waitForFunction(() => document.querySelector('[data-widget="burndown"] [data-burndown-summary]')?.textContent.includes("5 of 8"), {
    timeout: WAIT,
  });
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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Recorded", { visit: false });

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
  const who = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Timed", { visit: false });
  const issueKey = await createIssue(page, key, "needs hours and words", { visit: false });
  await goto(page, `/issues/${issueKey}`);

  // Assign it to the only person there is, and hand the report to them too.
  await pickPerson(page, 'select[aria-label="Assignee"]', who.name);
  await page.waitForFunction((n) => document.body.innerText.includes(`assigned this to ${n}`), { timeout: WAIT }, who.name);
  await page.waitForSelector('select[aria-label="Reporter"]', { timeout: 10_000 });

  // Labels are coined by typing them; the second one is offered back. Labels
  // are everybody's, so these are words no other scenario uses.
  const backend = uniqueName("backend", "-");
  const urgent = uniqueName("urgent", "-");
  await page.type('input[aria-label="Add a label"]', backend);
  await page.keyboard.press("Enter");
  await page.waitForSelector(`[data-labels] [data-label="${backend}"]`, { timeout: WAIT });
  await page.type('input[aria-label="Add a label"]', `${urgent},`);
  await page.waitForSelector(`[data-labels] [data-label="${urgent}"]`, { timeout: WAIT });
  await page.waitForFunction((words) => document.body.innerText.includes(`labelled this ${words}`), { timeout: WAIT }, `${backend}, ${urgent}`);

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

  // An entry logged wrong is corrected in place, and the time spent with it.
  await page.click('[data-worklog] [data-action="edit-worklog"]');
  await page.waitForSelector("[data-worklog-editor]", { timeout: WAIT });
  await page.click('[data-worklog-editor] input[id$="-duration"]', { clickCount: 3 });
  await page.type('[data-worklog-editor] input[id$="-duration"]', "45m");
  await page.click('[data-worklog-editor] [data-action="save-worklog"]');
  await page.waitForFunction(() => document.querySelector("[data-time-spent]")?.textContent.trim() === "45m", { timeout: WAIT });
  await page.waitForFunction(() => !document.querySelector("[data-worklog-editor]"), { timeout: WAIT });
  expect.contains(await textOf(page, "[data-worklog]"), "read the spec", "the note stays with it");

  // A comment is the author's to correct, or to take back.
  await page.type("#new-comment", "Teh spec is clear.");
  await clickButton(page, "Comment");
  await page.waitForFunction(() => document.querySelector("[data-comment]")?.textContent.includes("Teh spec"), { timeout: WAIT });
  await page.click('[data-comment] [data-action="edit-comment"]');
  await writeInEditor(page, '[data-comment] [id^="comment-"]', "The spec is clear.");
  await page.click('[data-comment] [data-action="save-comment"]');
  await page.waitForSelector("[data-comment] [data-comment-edited]", { timeout: WAIT });
  expect.contains(await textOf(page, "[data-comment]"), "The spec is clear.", "the comment says what was meant");
  await page.type("#new-comment", "Posted on the wrong issue.");
  await clickButton(page, "Comment");
  await page.waitForFunction(() => document.querySelectorAll("[data-comment]").length === 2, { timeout: WAIT });
  await page.click('[data-activity="comments"] li:last-child [data-action="delete-comment"]');
  await confirm(page);
  await page.waitForFunction(() => !document.body.innerText.includes("Posted on the wrong issue."), { timeout: WAIT });
  expect.equal((await page.$$("[data-comment]")).length, 1, "only the comment taken back is gone");

  // Words that are not time are refused with the field named, not saved.
  await page.click('input[aria-label="Remaining"]', { clickCount: 3 });
  await page.type('input[aria-label="Remaining"]', "a while");
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => document.body.innerText.includes("Remaining takes time such as"), { timeout: WAIT });

  // The label shows in the project's list, and the list filters by it.
  await goto(page, `/projects/${key}`);
  await page.waitForSelector(`[data-label="${backend}"]`, { timeout: WAIT });
  await openFilters(page);
  await page.type('input[aria-label="Filter by label"]', urgent);
  await page.waitForSelector(`[role="option"][data-label-option="${urgent}"]`, { timeout: WAIT });
  await page.click(`[role="option"][data-label-option="${urgent}"]`);
  await page.waitForFunction(() => document.body.innerText.includes("1 issue"), { timeout: WAIT });
});

// Serial: a label is the organization's, and renaming one renames it for everybody.
serialScenario("a label is listed for everyone and renamed everywhere", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Labelled", { visit: false });
  const issueKey = await createIssue(page, key, "carries the word", { visit: false });
  const urgent = uniqueName("urgent", "-");
  const blocker = uniqueName("blocker", "-");
  await goto(page, `/issues/${issueKey}`);
  await page.type('input[aria-label="Add a label"]', urgent);
  await page.keyboard.press("Enter");
  await page.waitForSelector(`[data-labels] [data-label="${urgent}"]`, { timeout: WAIT });

  // The installation holds every scenario's words, so the page is searched for this one.
  await goto(page, `/settings/labels?q=${encodeURIComponent(urgent)}`);
  const row = `[data-label-row="${urgent}"]`;
  await page.waitForSelector(row, { timeout: WAIT });
  await page.evaluate((sel) => {
    [...document.querySelector(sel).querySelectorAll("button")].find((b) => b.textContent.trim() === "Edit")?.click();
  }, row);
  await page.waitForSelector(`${row} input`, { timeout: 10_000 });
  await page.click(`${row} input`, { clickCount: 3 });
  await page.type(`${row} input`, blocker);
  await clickButton(page, "Save label");
  await waitForButtonToGo(page, "Save label");
  await goto(page, `/settings/labels?q=${encodeURIComponent(blocker)}`);
  await page.waitForSelector(`[data-label-row="${blocker}"]`, { timeout: WAIT });
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector(`[data-labels] [data-label="${blocker}"]`, { timeout: WAIT });
});

// Labels are everybody's, so a label field shows none of them until something
// is typed, then offers the few whose start it is.
scenario("a label field suggests an existing label as its start is typed, and shows none before", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Suggested", { visit: false });
  const tagged = await createIssue(page, key, "already carries the word", { visit: false });
  const later = await createIssue(page, key, "takes the word on its page", { visit: false });
  const word = uniqueName("frontend", "-");
  const start = word.slice(0, -2);
  await goto(page, `/projects/${key}`);
  await page.evaluate(
    async (issue, name) => {
      const tagging = await fetch(`/api/v1/issues/${issue}/labels`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ labels: [name] }),
      });
      if (!tagging.ok) throw new Error(`tagging failed: ${tagging.status}`);
    },
    tagged,
    word,
  );
  await goto(page, `/projects/${key}`);

  // Typing the start offers it first; Enter adds the one the arrow reached.
  const suggestedFirst = (box, name) =>
    page.waitForFunction(
      (input, wanted) => {
        const list = document.getElementById(document.querySelector(input)?.getAttribute("aria-controls") ?? "");
        return list?.querySelector('[role="option"]')?.getAttribute("data-label-option") === wanted;
      },
      { timeout: WAIT },
      box,
      name,
    );

  await clickButton(page, "New issue");
  await page.waitForSelector("[data-create-issue] #create-labels", { timeout: WAIT });
  await settled(page);
  expect.equal(
    await page.$eval("[data-create-issue]", (dialog, name) => dialog.innerText.includes(name), word),
    false,
    "the dialog draws no label nobody asked for",
  );
  await fill(page, "Summary", "picks the word from the suggestions");
  await page.type("[data-create-issue] #create-labels", start);
  await suggestedFirst("[data-create-issue] #create-labels", word);
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await page.waitForSelector(`[data-create-issue] [data-label="${word}"]`, { timeout: WAIT });
  await clickButton(page, "Create issue");
  await page.waitForFunction(() => !document.querySelector("[data-create-issue]"), { timeout: WAIT });
  const made = await page.evaluate(async (project) => {
    const found = await (await fetch(`/api/v1/projects/${project}/issues?text=${encodeURIComponent("picks the word")}`, { credentials: "include" })).json();
    return found.issues.map((issue) => issue.labels.map((l) => l.name));
  }, key);
  expect.equal(JSON.stringify(made), JSON.stringify([[word]]), "the issue is made with the label picked");

  // The issue page offers it the same way.
  await goto(page, `/issues/${later}`);
  await page.waitForSelector('input[aria-label="Add a label"]', { timeout: WAIT });
  await page.click('input[aria-label="Add a label"]');
  expect.equal(await page.$('[role="listbox"]'), null, "focusing the box offers nothing yet");
  await page.type('input[aria-label="Add a label"]', start);
  await suggestedFirst('input[aria-label="Add a label"]', word);
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await page.waitForSelector(`[data-labels] [data-label="${word}"]`, { timeout: WAIT });
  await page.waitForFunction((name) => document.body.innerText.includes(`labelled this ${name}`), { timeout: WAIT }, word);
});

scenario("a page another application puts on an issue is listed under Pages, a requirement under Requirements", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Specced", { visit: false });
  const issueKey = await createIssue(page, key, "the checkout flow", { type: "Story", visit: false });
  const secret = await createToken(page, "Page sync");
  const spec = "https://wiki.example.com/pages/checkout";
  const requirement = "https://req.example.com/requirements/R-7";

  // The application syncs over the API with a token, and sends the same page
  // twice: the second time it has been retitled.
  const put = (body) =>
    page.evaluate(
      async (bearer, target, link) => {
        const response = await fetch(`/api/v1/issues/${target}/remote-links`, {
          method: "POST",
          headers: { Authorization: `Bearer ${bearer}`, "Content-Type": "application/json" },
          body: JSON.stringify(link),
        });
        return response.status;
      },
      secret,
      issueKey,
      body,
    );
  expect.equal(await put({ url: spec, title: "Checkout spec", source: "Stator" }), 201, "the page is put on the issue");
  expect.equal(await put({ url: spec, title: "Checkout spec, second draft", source: "Stator" }), 200, "the same page again is retitled");
  expect.equal(
    await put({ url: requirement, title: "R-7 Pay by card", source: "Exciter", relation: "requirement" }),
    201,
    "a requirement is traced to the issue",
  );

  await goto(page, `/issues/${issueKey}`);
  const row = `[data-testid="pages-panel"] [data-remote-link-group="page"] [data-remote-link="${spec}"]`;
  const traced = `[data-testid="pages-panel"] [data-remote-link-group="requirement"] [data-remote-link="${requirement}"]`;
  await page.waitForSelector(row, { timeout: WAIT });
  await page.waitForSelector(traced, { timeout: WAIT });
  expect.equal(await page.$$eval("[data-remote-link-group=page] [data-remote-link]", (rows) => rows.length), 1, "one row for the page");
  expect.equal(
    await page.$$eval('[data-testid="pages-panel"] h2', (hs) => hs.map((h) => h.textContent.trim()).join(" | ")),
    "Pages (1) | Requirements (1)",
    "pages and requirements are headed apart, pages first",
  );
  expect.contains(await textOf(page, traced), "Exciter", "the requirement names where it lives");
  const link = await page.$eval(`${row} a`, (a) => ({
    text: a.textContent.trim(),
    href: a.getAttribute("href"),
    target: a.getAttribute("target"),
    rel: a.getAttribute("rel"),
  }));
  expect.equal(link.text, "Checkout spec, second draft", "the row carries the latest title");
  expect.equal(link.href, spec, "and goes to the page");
  expect.equal(link.target, "_blank", "in a new tab");
  expect.contains(link.rel, "noopener", "without handing the page this one");
  expect.contains(await textOf(page, row), "Stator", "the row names where the page lives");

  // Somebody who edits the issue can take each off, and the section goes with the last.
  await page.click(`${row} button[aria-label="Remove the page Checkout spec, second draft"]`);
  await page.waitForFunction(() => !document.querySelector("[data-remote-link-group=page]"), { timeout: WAIT });
  await page.click(`${traced} button[aria-label="Remove the requirement R-7 Pay by card"]`);
  await page.waitForFunction(() => !document.querySelector('[data-testid="pages-panel"]'), { timeout: WAIT });
});

scenario("a draft started in the side panel stays with its own issue", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Drafted", { visit: false });
  const a = await createIssue(page, key, "the issue stepped back to", { type: "Task", visit: false });
  const b = await createIssue(page, key, "the issue written on", { type: "Task", visit: false });

  await goto(page, `/issues/${a}`);
  await clickButton(page, "Add description");
  await writeInEditor(page, "#issue-description", "What A is about.");
  await clickButton(page, "Save description");
  await page.waitForFunction(() => document.querySelector("[data-description]")?.textContent.includes("What A is about."), { timeout: WAIT });

  await withViewport(page, DOCKED_VIEWPORT_WIDTH, async () => {
    await goto(page, `/projects/${key}`);
    const rows = await issueRows(page);
    const towardB = rows.indexOf(b) > rows.indexOf(a) ? "next" : "prev";
    const towardA = towardB === "next" ? "prev" : "next";

    // A is seen first, so it is cached when the panel comes back to it.
    await openIssueBeside(page, a);
    await page.keyboard.press(towardB === "next" ? "ArrowDown" : "ArrowUp");
    await page.waitForSelector(`[data-issue-panel="${b}"] [data-issue-page="${b}"]`, { timeout: WAIT });
    await clickButton(page, "Add description");
    await writeInEditor(page, "#issue-description", "What B is about.");
    await page.type("#new-comment", "Half a thought on B");

    await page.click(`[data-action="panel-${towardA}"]`);
    await page.waitForSelector(`[data-issue-panel="${a}"] [data-issue-page="${a}"]`, { timeout: WAIT });
    expect.equal(await page.$("#issue-description"), null, "A opens without B's editor");
    expect.contains(await textOf(page, "[data-description]"), "What A is about.", "A shows its own description");
    expect.equal((await editorText(page, "#new-comment")).trim(), "", "A's comment box is empty");
    // Ctrl+Enter in A's empty box has nothing to send.
    await page.focus("#new-comment");
    await page.keyboard.down("Control");
    await page.keyboard.press("Enter");
    await page.keyboard.up("Control");
    await settled(page);

    // Back on B, both drafts are where they were left, and they go to B.
    await page.click(`[data-action="panel-${towardB}"]`);
    await page.waitForSelector(`[data-issue-panel="${b}"] [data-issue-page="${b}"] #issue-description`, { timeout: WAIT });
    expect.contains(await editorText(page, "#issue-description"), "What B is about.", "B's description draft waited for it");
    expect.contains(await editorText(page, "#new-comment"), "Half a thought on B", "and so did B's comment");
    await clickButton(page, "Save description");
    await page.waitForFunction(() => document.querySelector("[data-description]")?.textContent.includes("What B is about."), { timeout: WAIT });
    await clickButton(page, "Comment");
    await page.waitForFunction(() => document.querySelector('[data-activity="comments"]')?.textContent.includes("Half a thought on B"), { timeout: WAIT });
  });

  await goto(page, `/issues/${a}`);
  await page.waitForSelector("[data-description]", { timeout: WAIT });
  expect.contains(await textOf(page, "[data-description]"), "What A is about.", "A's description is unchanged");
  expect.truthy(!(await bodyText(page)).includes("What B is about."), "B's description never reached A");
  expect.truthy(!(await bodyText(page)).includes("Half a thought on B"), "and neither did B's comment");

  await goto(page, `/issues/${b}`);
  await page.waitForFunction(() => document.querySelector('[data-activity="comments"]')?.textContent.includes("Half a thought on B"), { timeout: WAIT });
  expect.contains(await textOf(page, "[data-description]"), "What B is about.", "B kept what was written on it");
});

scenario("a task that turns out to be a bug is changed to one from its header", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Retyped", { visit: false });
  const issueKey = await createIssue(page, key, "the export drops the last row", { type: "Task", visit: false });

  await goto(page, `/issues/${issueKey}`);
  await changeType(page, "Bug");

  await page.waitForSelector('[data-action="change-type"][aria-label="Issue type: Bug"]', { timeout: WAIT });
  await page.waitForFunction(() => document.querySelector("[data-issue-history]")?.innerText.includes("changed the type from Task to Bug"), { timeout: WAIT });
  expect.equal(await page.$('[role="dialog"]'), null, "the status carried over, so nothing was asked");
});

scenario("a type change that leaves the issue's status behind says where it lands", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Relanded", { visit: false });
  // Workflows are everybody's, so this one has a name nobody else took.
  await followWorkflow(page, key, "Bug", { name: uniqueName("Bugs skip the middle"), statuses: ["To Do", "Done"] });
  const issueKey = await createIssue(page, key, "half done and mislabelled", { type: "Task", visit: false });

  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");
  await page.waitForSelector("header .status-in_progress", { timeout: WAIT });
  await changeType(page, "Bug");

  await page.waitForSelector('[role="dialog"] [data-action="confirm"]', { timeout: WAIT });
  const asked = await textOf(page, '[role="dialog"]');
  expect.contains(asked, `Change ${issueKey} to Bug?`, "the question names the issue and the type");
  expect.contains(asked, "Its workflow has no In Progress, so it moves to To Do.", "and where the issue lands");
  await confirm(page);

  await page.waitForSelector('[data-action="change-type"][aria-label="Issue type: Bug"]', { timeout: WAIT });
  await page.waitForSelector("header .status-todo", { timeout: WAIT });
  await page.waitForFunction(() => document.querySelector("[data-issue-history]")?.innerText.includes("moved this from In Progress to To Do"), {
    timeout: WAIT,
  });
});

// The same rules Stator and Exciter draw a document with, under the production stylesheet and its policy.
scenario("a description is drawn with the document styles Stator and Exciter share", async ({ page }) => {
  const refused = [];
  page.on("console", (message) => {
    if (/Content Security Policy|Refused to/i.test(message.text())) refused.push(message.text());
  });
  await signUp(page);
  const key = await createProject(page, "Docs look", { visit: false });
  const issueKey = await createIssue(page, key, "Read me", { visit: false });
  await patchThroughAPI(page, issueKey, { descriptionMarkdown: "# Steps\n\n- one\n- two\n\n1. first\n\n> Quoted, with `code` in it.\n\n```go\nx := 1\n```\n" });

  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector("[data-description] .doc-content blockquote", { timeout: WAIT });
  const look = await page.$eval("[data-description] .doc-content", (doc) => {
    const style = (selector) => getComputedStyle(doc.querySelector(selector));
    return {
      heading: style("h2").fontWeight,
      bullets: style("ul").listStyleType,
      numbers: style("ol").listStyleType,
      quoteEdge: style("blockquote").borderLeftWidth,
      inlineCode: style("blockquote code").fontFamily,
      block: style("pre").backgroundColor,
    };
  });
  expect.equal(look.heading, "600", "a heading's weight");
  expect.equal(look.bullets, "disc", "a bullet list's marker");
  expect.equal(look.numbers, "decimal", "a numbered list's marker");
  expect.equal(look.quoteEdge, "2px", "a quote's edge");
  expect.containsInsensitive(look.inlineCode, "mono", "inline code's font");
  expect.truthy(look.block !== "rgba(0, 0, 0, 0)", `a code block's ground is ${look.block}`);
  expect.equal(refused.length, 0, `the policy refused: ${refused.join(" | ")}`);
});

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
