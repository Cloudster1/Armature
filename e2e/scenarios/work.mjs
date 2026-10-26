// Everyday work across the product: mentions, rules, webhooks, versions, searches, bulk edits, moves, imports.

import { writeFileSync } from "node:fs";
import { scenario, serialScenario } from "../runner.mjs";
import {
  acceptInvite,
  addWidget,
  applyBulk,
  bodyText,
  clickButton,
  createComponent,
  createIssue,
  createProject,
  createRule,
  createVersion,
  createWebhook,
  dashboardFixture,
  eventually,
  expect,
  exportedCSV,
  goto,
  importCSV,
  inboxRows,
  incomingAddress,
  inviteMember,
  labelIssues,
  mailFor,
  mentionInComment,
  moveIssue,
  pickName,
  removeWebhook,
  ruleMenu,
  saveSearch,
  selectAllIssues,
  selectByLabel,
  signIn,
  signUp,
  textOf,
  unique,
  uniqueName,
  unreadCount,
  versionMenu,
  WAIT,
  waitForApp,
  waitForPath,
  webhookMenu,
} from "../helpers.mjs";

scenario("a mention in a comment lands in the person's inbox and makes them a watcher", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Mentions", { visit: false });
  const issueKey = await createIssue(page, key, "Wire the doorbell", { visit: false });
  const ben = await inviteMember(page, "ben");
  await acceptInvite(page, ben, { visit: false });

  // Back as the owner: the picker offers Ben after an at sign, and the comment names him.
  await signIn(page, owner.email);
  await waitForPath(page, "/");
  const before = Date.now();
  await mentionInComment(page, issueKey, "Have a look", ben.name);
  // The name picked in the editor is a mention node, drawn as one in the comment.
  await page.waitForSelector("[data-comment] [data-mention]", { timeout: WAIT });
  await page.waitForSelector('[data-testid="watchers-panel"]', { timeout: WAIT });
  await page.waitForFunction((name) => document.querySelector('[data-testid="watchers-panel"]')?.innerText.includes(name), { timeout: WAIT }, ben.name);

  // Ben is told twice over: a mail with the words, and a row in his inbox that opens the issue.
  const mail = await mailFor(ben.email, issueKey, { since: before, saying: "mentioned you" });
  expect.contains(mail.text, "mentioned you", "the mail says why");
  await signIn(page, ben.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  expect.equal((await unreadCount(page)) >= 1, true, "the bell counts it");
  const rows = await inboxRows(page);
  expect.equal(
    rows.some((r) => r.kind === "mentioned" && !r.read && r.text.includes(issueKey)),
    true,
    `the inbox lists the mention: ${JSON.stringify(rows)}`,
  );
  await page.click('[data-notification="mentioned"]');
  await waitForPath(page, `/issues/${issueKey}`);
  await goto(page, "/inbox");
  await page.waitForSelector('[data-inbox-view="all"]', { timeout: WAIT });
  await page.click('[data-inbox-view="all"]');
  await page.waitForSelector('[data-notification="mentioned"][data-notification-read="true"]', { timeout: WAIT });

  // How he is told is his to set.
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-notification-settings]", { timeout: WAIT });
  await page.click('[data-pref-mail="commented"]');
  await page.click("[data-pref-watch-own]");
  await page.click('[data-action="save-notifications"]');
  await page.waitForFunction(() => document.body.innerText.includes("Notification settings saved"), { timeout: WAIT });
  // Watching his own issues stays off when he comes back.
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-pref-watch-own]:not(:checked)", { timeout: WAIT });
});

scenario("a rule labels a ticket when it is moved, and the run log says so", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Automated", { visit: false });
  await createRule(page, key, { name: "Mark moved work", trigger: "An issue moves", action: "Add a label", value: "moving" });
  expect.contains(await bodyText(page), "When an issue moves, then add the label moving.", "the table reads the rule as a sentence");

  const issueKey = await createIssue(page, key, "Oil the hinge", { visit: false });
  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");
  // The worker runs the rule from the stream; the label arrives on its own.
  await eventually(page, '[data-label="moving"]');

  await goto(page, `/projects/${key}/automation`);
  await page.waitForSelector('[data-rule="Mark moved work"]', { timeout: WAIT });
  await ruleMenu(page, "Mark moved work", "rule-log");
  await page.waitForSelector('[data-rule-run="done"]', { timeout: WAIT });
  expect.contains(await textOf(page, '[data-rule-log="Mark moved work"]'), `labelled ${issueKey} moving`, "the log says what was done");
});

// Serial: a webhook hears every project, and the list of them is everybody's.
serialScenario("a webhook endpoint gets a signed test delivery, and it starts a rule", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Hooked", { visit: false });
  // The receiver is Armature itself: an incoming rule that files an issue.
  await createRule(page, key, { name: "From the pager", trigger: "An incoming call", action: "Create an issue", text: "An alert came in" });
  const address = await incomingAddress(page, "From the pager");

  // Subscribed to a topic that never fires here, so only the test's ping goes
  // through: an endpoint that took everything and filed an issue for each
  // would be feeding itself.
  const hook = uniqueName("Pager");
  try {
    const secret = await createWebhook(page, hook, `http://api:8080${address}`, { topics: ["sla.breached"] });
    expect.equal(secret.startsWith("armature_whs_"), true, "the secret is shown once");
    await webhookMenu(page, hook, "webhook-test");
    await page.waitForFunction(() => document.body.innerText.includes("Delivered, 202"), { timeout: WAIT });
    await webhookMenu(page, hook, "webhook-deliveries");
    await page.waitForSelector('[data-delivery="ping"][data-delivery-state="delivered"]', { timeout: WAIT });
    await page.keyboard.press("Escape");

    // The ping went through the incoming address, and the rule filed the issue.
    await goto(page, `/projects/${key}`);
    await eventually(page, "[data-issue-row]");
    await page.waitForFunction(() => document.body.innerText.includes("An alert came in"), { timeout: 30_000 });
  } finally {
    await removeWebhook(hook);
  }
});

scenario("a version is released and its notes list the finished work", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Shipping", { visit: false });
  await createVersion(page, key, "1.0", "2026-12-24");
  const fixed = await createIssue(page, key, "Fix the login", { visit: false });
  const open = await createIssue(page, key, "Polish the icons");

  // Both name the version as what fixes them; one gets finished.
  for (const issueKey of [fixed, open]) {
    await goto(page, `/issues/${issueKey}`);
    await pickName(page, "issue-fix-versions", "1.0");
  }
  await goto(page, `/issues/${fixed}`);
  await clickButton(page, "Close");
  await page.waitForFunction(() => document.body.innerText.includes("Done"), { timeout: WAIT });

  await goto(page, `/projects/${key}/releases`);
  await page.waitForSelector('[data-version="1.0"]', { timeout: WAIT });
  expect.contains(await textOf(page, '[data-version="1.0"]'), "1 of 2 done", "progress counts what fixes it");
  await versionMenu(page, "1.0", "version-notes");
  await page.waitForSelector(`[data-notes-issue="${fixed}"]`, { timeout: WAIT });
  const notes = await textOf(page, '[data-release-notes="1.0"]');
  expect.contains(notes, "Fix the login", "the notes list the finished issue");
  expect.notContains(notes, "Polish the icons", "and not the open one");
  await page.keyboard.press("Escape");

  await versionMenu(page, "1.0", "version-release");
  await page.waitForFunction(() => document.body.innerText.includes("1.0 released"), { timeout: WAIT });
  await page.click('[data-versions-view="released"]');
  await page.waitForSelector('[data-version="1.0"][data-version-state="released"]', { timeout: WAIT });
});

scenario("a ticket filed with a component lands on the component's default assignee", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Parts", { visit: false });
  await createComponent(page, key, "Billing", { assignee: owner.name });
  expect.equal(
    await page.$eval('[data-component="Billing"] [data-component-assignee]', (el) => el.dataset.componentAssignee),
    owner.name,
    "the component says who takes new work",
  );

  const issueKey = await createIssue(page, key, "Invoice is wrong", { visit: false });
  await goto(page, `/issues/${issueKey}`);
  await pickName(page, "issue-components", "Billing");
  // Filing through the API with the component, as the create dialog will: the assignee is the component's.
  const filed = await page.evaluate(async (projectKey) => {
    const components = await (await fetch(`/api/v1/projects/${projectKey}/components`, { credentials: "include" })).json();
    const r = await fetch(`/api/v1/projects/${projectKey}/issues`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ summary: "Refund failed", componentIds: [components.components[0].id] }),
    });
    return (await r.json()).issue;
  }, key);
  expect.equal(filed.assignee?.name, owner.name, "the default assignee took it");
  expect.equal(filed.components[0]?.name, "Billing", "in the component");
  await goto(page, `/issues/${filed.key}`);
  await page.waitForSelector('[data-picked="Billing"]', { timeout: WAIT });
  expect.contains(await bodyText(page), "assigned this to", "the history says who took it");
});

scenario("a search is saved, starred and offered on the search page", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Searches", { visit: false });
  await createIssue(page, key, "Something open", { visit: false });
  // Shared filters are everybody's to see, so this one has a name of its own.
  const open = uniqueName("Open work");
  const query = `project = ${key} AND statusCategory != done`;
  await goto(page, `/search?q=${encodeURIComponent(query)}`);
  await saveSearch(page, open, { shared: true });
  // Saving lands on the saved search: its name is the title and its chip is pressed.
  await page.waitForFunction(() => document.body.innerText.includes("A saved search"), { timeout: WAIT });
  expect.equal(await page.$eval(`[data-filter="${open}"]`, (el) => el.getAttribute("aria-pressed")), "true", "the chip is pressed");
  expect.equal(await page.$eval(`[data-filter="${open}"]`, (el) => el.dataset.filterShared), "true", "and shared");
  await page.click(`[data-filter-star="${open}"]`);
  await page.waitForSelector(`[data-filter-star="${open}"][data-starred="true"]`, { timeout: WAIT });

  // The address a subscription mail links to runs the saved search.
  const id = await page.evaluate(
    async (name) => (await (await fetch("/api/v1/filters", { credentials: "include" })).json()).filters.find((f) => f.name === name).id,
    open,
  );
  await goto(page, `/filters/${id}`);
  await waitForPath(page, "/search");
  await page.waitForFunction((n) => document.body.innerText.includes(n) && document.body.innerText.includes("Something open"), { timeout: WAIT }, open);

  await goto(page, "/filters");
  await page.waitForSelector(`[data-filter-row="${open}"]`, { timeout: WAIT });
  await page.click('[data-filters-view="starred"]');
  await page.waitForSelector(`[data-filter-row="${open}"]`, { timeout: WAIT });
  await selectByLabel(page, `[data-filter-subscription="${open}"]`, "Daily at 08:00");
  await page.waitForFunction((n) => document.body.innerText.includes(`You will get ${n} daily`), { timeout: WAIT }, open);

  // Export offers the columns and a file link.
  await goto(page, `/search?q=${encodeURIComponent(query)}`);
  await page.waitForSelector('[data-action="export-csv"]', { timeout: WAIT });
  await page.click('[data-action="export-csv"]');
  await page.waitForSelector("[data-export-dialog]", { timeout: WAIT });
  await page.click('[data-export-column="labels"]');
  const href = await page.$eval('[data-action="download-csv"]', (a) => a.getAttribute("href"));
  expect.contains(href, "columns=", "the link carries the columns");
  expect.contains(href, "labels", "including the one ticked");
  const csv = await page.evaluate(async (h) => (await fetch(h, { credentials: "include" })).text(), href);
  expect.contains(csv.split("\n")[0], "key,summary", "the file starts with its header");
  expect.contains(csv, "Something open", "and holds the issue");
});

// The bar offers only the transitions every ticked row shares, so one already
// under way keeps Start progress off the list until it is unticked.
scenario("three tickets are edited at once and only a transition they all share is offered", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Batches", { visit: false });
  const a = await createIssue(page, key, "Batch one", { visit: false });
  const b = await createIssue(page, key, "Batch two", { visit: false });
  const c = await createIssue(page, key, "Batch three, already going", { visit: false });
  await goto(page, `/issues/${c}`);
  await clickButton(page, "Start progress");
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });

  await goto(page, `/search?q=${encodeURIComponent(`project = ${key}`)}`);
  await selectAllIssues(page);
  // The shared names arrive after the tick: the list either offers some or says none are shared.
  await page.waitForFunction(
    () => {
      const select = document.querySelector("#field-bulk-transition");
      return select && (select.disabled || select.options.length > 1);
    },
    { timeout: WAIT },
  );
  const offered = await page.$$eval("#field-bulk-transition option", (options) => options.map((o) => o.textContent.trim()));
  expect.equal(offered.includes("Start progress"), false, "the issue in progress does not share Start progress");
  const { refused, title } = await applyBulk(page, { priority: "high" });
  expect.contains(title, "3 changed", "all three took the priority");
  expect.equal(refused.length, 0, "and none was refused");
  await clickButton(page, "Done");

  await page.click(`[data-select-issue="${a}"]`);
  await page.click(`[data-select-issue="${b}"]`);
  await page.waitForSelector('[data-bulk-bar="2"]', { timeout: WAIT });
  const moved = await applyBulk(page, { transition: "Start progress" });
  expect.contains(moved.title, "2 changed", "the two still to do started");
  await clickButton(page, "Done");
  await goto(page, `/issues/${a}`);
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });
  await goto(page, `/issues/${b}`);
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });
});

scenario("a retired label is stripped from every issue a search finds, and the rest stay", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Retiring", { visit: false });
  const retired = uniqueName("retired", "-");
  const kept = uniqueName("kept", "-");
  const a = await createIssue(page, key, "Labelled one", { visit: false });
  const b = await createIssue(page, key, "Labelled two", { visit: false });
  await labelIssues(page, [a, b], [retired, kept]);

  await goto(page, `/search?q=${encodeURIComponent(`project = ${key}`)}`);
  await page.waitForSelector(`[data-label="${retired}"]`, { timeout: WAIT });
  await selectAllIssues(page);
  const { refused, title } = await applyBulk(page, { removeLabel: retired });
  expect.contains(title, "2 changed", "both lost the label");
  expect.equal(refused.length, 0, "and none was refused");
  await clickButton(page, "Done");
  await page.waitForFunction((name) => !document.querySelector(`[data-label="${name}"]`), { timeout: WAIT }, retired);
  expect.equal((await page.$$(`[data-label="${kept}"]`)).length, 2, "the other label stays on both");
});

scenario("one issue is exported from its page, ticked rows from search, and a project's list from the list", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Exports", { visit: false });
  const a = await createIssue(page, key, "Export me alone", { visit: false });
  const b = await createIssue(page, key, "Export me with a friend", { visit: false });
  await createIssue(page, key, "Leave me out", { visit: false });
  const keysOf = (rows) =>
    rows
      .slice(1)
      .map((row) => row.split(",")[0])
      .sort()
      .join(" ");

  // The issue that is open, from its More menu.
  await goto(page, `/issues/${a}`);
  await page.waitForSelector('[data-action="issue-more"]', { timeout: WAIT });
  await page.click('[data-action="issue-more"]');
  const one = await exportedCSV(page, '[data-action="export-issue"]');
  expect.contains(one.rows[0], "key,summary", "the file starts with its header");
  expect.equal(keysOf(one.rows), a, "and holds that issue alone");

  // Two ticked rows on search, with no change chosen.
  await goto(page, `/search?q=${encodeURIComponent(`project = ${key}`)}`);
  await page.waitForSelector(`[data-select-issue="${a}"]`, { timeout: WAIT });
  await page.click(`[data-select-issue="${a}"]`);
  await page.click(`[data-select-issue="${b}"]`);
  await page.waitForSelector('[data-bulk-bar="2"]', { timeout: WAIT });
  const two = await exportedCSV(page, '[data-action="export-selected"]');
  expect.equal(keysOf(two.rows), [a, b].sort().join(" "), "the ticked rows and nothing else");

  // The project's list, narrowed to words in the summary, on every page.
  await goto(page, `/projects/${key}`);
  await page.waitForSelector('input[aria-label="Search issues"]', { timeout: WAIT });
  await page.type('input[aria-label="Search issues"]', "Export me");
  await page.waitForFunction(() => !document.body.innerText.includes("Leave me out"), { timeout: WAIT });
  const list = await exportedCSV(page, '[data-action="export-list"]');
  expect.contains(list.href, `project=${key}`, "the file is the project's");
  expect.equal(keysOf(list.rows), [a, b].sort().join(" "), "and holds what the list shows");
});

scenario("a ticket moves to another project and keeps its history", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const from = await createProject(page, "From here", { visit: false });
  const to = await createProject(page, "To there", { visit: false });
  const issueKey = await createIssue(page, from, "Wandering ticket", { visit: false });
  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });

  const path = await moveIssue(page, issueKey, to);
  const newKey = path.split("/").pop();
  expect.equal(newKey.startsWith(`${to}-`), true, `the issue has the other project's key: ${newKey}`);
  const text = await bodyText(page);
  expect.contains(text, `Moved from ${issueKey}`, "the comment says where it came from");
  expect.contains(text, "moved this from To Do to In Progress", "and the history from before the move is still there");
  expect.contains(text, "In Progress", "in the same status");

  // The old address still finds it.
  await goto(page, `/issues/${issueKey}`);
  await page.waitForFunction((k) => document.body.innerText.includes(k), { timeout: WAIT }, newKey);
});

scenario("a CSV file becomes issues after a dry run", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Imports");
  const csvPath = `/tmp/import-${Date.now()}.csv`;
  // Two columns of one name, as every Jira export has, and a comment packed
  // into a cell the way one writes them: both must be read.
  writeFileSync(
    csvPath,
    "Title,Priority,Tags,Tags,Comment\nFrom the file,high,imported,again,04/Aug/26 10:20;somebody;Said in the old tracker.;;;\nAnother row,urgent,,,\n",
  );
  const dry = await importCSV(page, key, csvPath, { dry: true });
  expect.contains(dry, "1 of 2 rows would become issues", "the dry run counts what would be made");
  expect.contains(dry, "Row 3", "and names the line the row is on");
  expect.contains(dry, "not a priority", "with why");
  const done = await importCSV(page, key, csvPath);
  expect.contains(done, "1 of 2 rows became issues", "the real run makes it");
  await goto(page, `/projects/${key}`);
  await page.waitForFunction(() => document.body.innerText.includes("From the file"), { timeout: WAIT });
  await page.waitForSelector('[data-label="imported"]', { timeout: WAIT });
  await page.waitForSelector('[data-label="again"]', { timeout: WAIT });
  // The conversation the file packed into a cell came with the issue.
  const madeKey = done.match(/[A-Z][A-Z0-9]*-\d+/)?.[0];
  await goto(page, `/issues/${madeKey}`);
  await page.waitForFunction(() => document.body.innerText.includes("Said in the old tracker"), { timeout: WAIT });
});

scenario("a cumulative flow widget draws a band per status and lists the counts", async ({ page }) => {
  const { key } = await dashboardFixture(page, "Flowing");
  // One issue moves, so two statuses have something today.
  await goto(page, `/projects/${key}`);
  await page.waitForSelector("[data-issue-row]", { timeout: WAIT });
  const first = await page.$eval("[data-issue-row]", (el) => el.dataset.issueRow);
  await goto(page, `/issues/${first}`);
  await clickButton(page, "Start progress");
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });

  await goto(page, `/projects/${key}/dashboard`);
  await addWidget(page, "cumulative_flow");
  await page.waitForSelector('[data-widget="cumulative_flow"] [data-flow-band="To Do"]', { timeout: WAIT });
  await page.waitForSelector('[data-widget="cumulative_flow"] [data-flow-band="In Progress"]', { timeout: WAIT });
  const legend = await textOf(page, '[data-widget="cumulative_flow"] [data-flow-legend]');
  expect.contains(legend, "To Do 2", "two still to do");
  expect.contains(legend, "In Progress 1", "one in progress");

  // The other flow widgets draw too.
  await addWidget(page, "created_vs_resolved");
  await page.waitForSelector('[data-widget="created_vs_resolved"] [data-testid="line-chart"]', { timeout: WAIT });
  await addWidget(page, "avg_age");
  await page.waitForFunction(() => document.querySelector('[data-widget="avg_age"]')?.innerText.includes("average of 3 open"), { timeout: WAIT });
  await addWidget(page, "resolution_histogram");
  await page.waitForFunction(() => document.querySelector('[data-widget="resolution_histogram"]')?.innerText.includes("Nothing resolved"), { timeout: WAIT });
});
