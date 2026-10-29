// Projects, issues and the workflow they move through; people coming and going.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  deleteMyAccount,
  bodyText,
  clickButton,
  createIssue,
  createIssueThroughForm,
  createProject,
  createProjectThroughForm,
  expect,
  findMembers,
  fill,
  goto,
  grantRole,
  pickProjectAction,
  press,
  inviteMember,
  openSetupTab,
  PASSWORD,
  reload,
  revokeAll,
  waitForPath,
  settled,
  signIn,
  signInThroughForm,
  signUp,
  switchOff,
  textOf,
  unique,
  WAIT,
  editorText,
  setEditorMode,
  waitForApp,
  waitForProjectRow,
  openProjectRow,
  createProjects,
  archiveProjects,
  uniqueName,
} from "../helpers.mjs";

/** A page of the projects list, as PROJECT_PAGE_SIZE in web/src/config.ts has it. */
const PROJECT_PAGE = 50;

/** The keys on the projects list, in the order it shows them. */
const listedKeys = (page) => page.$$eval("tr[data-project]", (rows) => rows.map((row) => row.getAttribute("data-project")));

scenario("a project can be created and is listed", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });

  const key = await createProjectThroughForm(page, "Customer Portal");
  expect.truthy(key.length >= 2, `project key ${JSON.stringify(key)} looks wrong`);

  // Found the way a person finds one among thousands: by typing into the search.
  await goto(page, "/projects");
  await page.type('[aria-label="Find projects"]', key);
  await page.waitForFunction((k) => new URL(location.href).searchParams.get("q") === k, { timeout: WAIT }, key);
  await page.waitForSelector(`[data-project="${key}"]`, { timeout: WAIT });
  const listing = await textOf(page, `[data-project="${key}"]`);
  expect.contains(listing, "Customer Portal", "the project is listed");
  expect.contains(listing, key, "its key is shown");
});

// Thousands of projects come a page at a time: the search finds them by name or
// key, and the address keeps both, so a reload or a link opens the same page.
scenario("the projects page finds projects by name or key and pages through them", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const marker = uniqueName("Paged");
  const keys = await createProjects(page, marker, PROJECT_PAGE + 1);
  try {
    await goto(page, "/projects");
    await page.type('[aria-label="Find projects"]', marker);
    await page.waitForFunction((m) => new URL(location.href).searchParams.get("q") === m, { timeout: WAIT }, marker);
    await page.waitForFunction(
      (range) => document.querySelector("[data-page-range]")?.textContent === range,
      { timeout: WAIT },
      `1 to ${PROJECT_PAGE} of ${keys.length}`,
    );
    expect.contains(await bodyText(page), `${keys.length} projects`, "the count is of every match, not of the page");
    expect.equal((await listedKeys(page)).join(","), keys.slice(0, PROJECT_PAGE).join(","), "the first page holds the first matches by key");

    await press(page, '[aria-label="Next page"]');
    await page.waitForFunction((n) => new URL(location.href).searchParams.get("offset") === String(n), { timeout: WAIT }, PROJECT_PAGE);
    await page.waitForFunction(
      (k) => document.querySelectorAll("tr[data-project]").length === 1 && document.querySelector(`tr[data-project="${k}"]`),
      { timeout: WAIT },
      keys[PROJECT_PAGE],
    );

    await reload(page);
    await page.waitForSelector(`tr[data-project="${keys[PROJECT_PAGE]}"]`, { timeout: WAIT });
    expect.equal((await listedKeys(page)).join(","), keys[PROJECT_PAGE], "a reload opens the same page of the same search");

    const one = keys[7];
    await goto(page, `/projects?q=${one.toLowerCase()}`);
    await page.waitForSelector(`tr[data-project="${one}"]`, { timeout: WAIT });
    expect.equal((await listedKeys(page)).join(","), one, "a key, in any case, finds its project alone");
  } finally {
    await archiveProjects(page, keys);
  }
});

scenario("issues get sequential keys within their project", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Sequencing", { visit: false });

  const first = await createIssueThroughForm(page, key, "the very first issue");
  const second = await createIssueThroughForm(page, key, "the second issue");

  expect.equal(first, `${key}-1`, "first issue key");
  expect.equal(second, `${key}-2`, "second issue key");
});

scenario("a new issue starts in the workflow's first status", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Starting", { visit: false });
  const issueKey = await createIssueThroughForm(page, key, "brand new work");

  await goto(page, `/issues/${issueKey}`);
  const detail = await bodyText(page);
  expect.contains(detail, "To Do", "starts in To Do");
  expect.contains(detail, "Unassigned", "starts unassigned");
});

scenario("only the workflow's legal moves are offered", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Legal moves", { visit: false });
  const issueKey = await createIssue(page, key, "walk the workflow", { visit: false });

  await goto(page, `/issues/${issueKey}`);

  const buttonsFromTodo = await page.$$eval("button", (bs) => bs.map((b) => b.textContent.trim()));
  expect.truthy(buttonsFromTodo.includes("Start progress"), "Start progress is offered from To Do");
  expect.truthy(buttonsFromTodo.includes("Close"), "the global Close is offered");
  expect.truthy(!buttonsFromTodo.includes("Approve"), `Approve should not be reachable from To Do, saw: ${buttonsFromTodo.join(", ")}`);
});

scenario("taking a transition applies the workflow's post-functions", async ({ page }) => {
  const who = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Post functions", { visit: false });
  const issueKey = await createIssue(page, key, "assign on start", { visit: false });

  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");

  // Start progress assigns the issue to whoever took it.
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });
  const after = await bodyText(page);
  expect.contains(after, "In Progress", "status moved");
  expect.contains(after, who.name, "the workflow assigned the issue to the person who started it");
});

scenario("the changelog records every move", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Changelog", { visit: false });
  const issueKey = await createIssue(page, key, "keep a record", { visit: false });

  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");

  // The history is its own query, so wait for its content rather than for the
  // status badge that changes first.
  await page.waitForFunction(() => document.body.innerText.includes("moved this from To Do to In Progress"), { timeout: WAIT });

  const history = await bodyText(page);
  expect.containsInsensitive(history, "History", "the history section is shown");
  expect.contains(history, `created ${issueKey}`, "the creation is the first entry");
});

scenario("a person can take their data and go, and their comment stays", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Records", { visit: false });
  const issueKey = await createIssue(page, key, "keep the lights on", { visit: false });
  const leaver = await inviteMember(page, "leaver");
  await acceptInvite(page, leaver, { visit: false });

  await goto(page, `/issues/${issueKey}`);
  await page.type("#new-comment", "I will look at this on Monday.");
  await clickButton(page, "Comment");
  // The editor holds the words as they are typed, so only the saved comment in the list says the server has it.
  await page.waitForFunction(() => [...document.querySelectorAll("[data-comment]")].some((c) => c.textContent.includes("I will look at this on Monday.")), {
    timeout: WAIT,
  });

  // The export is one file with everything in it.
  const exported = await page.evaluate(async () => {
    const response = await fetch("/api/v1/auth/me/export", { credentials: "include" });
    return { status: response.status, disposition: response.headers.get("content-disposition"), body: await response.text() };
  });
  expect.equal(exported.status, 200, "the export is served");
  expect.contains(exported.disposition ?? "", "attachment", "as a file");
  expect.contains(exported.body, "I will look at this on Monday.", "holding what they wrote");

  await deleteMyAccount(page);
  await signInThroughForm(page, leaver.email);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains((await textOf(page, "[role=alert]")).toLowerCase(), "invalid email or password", "the address no longer signs in");

  await signIn(page, owner.email, PASSWORD, { visit: false });
  await goto(page, `/issues/${issueKey}`);
  await page.waitForFunction(() => document.body.innerText.includes("I will look at this on Monday."), { timeout: WAIT });
  expect.contains(await bodyText(page), "Former user", "the comment stays, by Former user");
});

scenario("an administrator lets a member go by switching them off", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const member = await inviteMember(page, "goer");
  await acceptInvite(page, member, { visit: false });
  await goto(page, "/");
  await page.waitForSelector("header", { timeout: WAIT });

  await signIn(page, owner.email, PASSWORD, { visit: false });
  await switchOff(page, member.email);
  // Searched for by name, since the list is a page of thousands.
  await findMembers(page, member.name);
  expect.equal(await page.$eval("[data-members-total]", (el) => el.getAttribute("data-members-total")), "0", "the member is gone from the list");
});

// Reported as "can't log in": the X beside the Member tag was taken for
// revoking a role, and the person it removed signed in to an empty shell.
scenario("somebody let go is told at sign-in that their account is switched off", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const member = await inviteMember(page, "letgo");
  await acceptInvite(page, member, { visit: false });

  await signIn(page, owner.email);
  await waitForApp(page);
  await grantRole(page, { who: member.name, role: "Global administrator" });
  await switchOff(page, member.email);

  await signInThroughForm(page, member.email);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains(await textOf(page, "[role=alert]"), "Your account has been switched off. Ask an administrator.", "the sign-in page says what to do");
  expect.equal(new URL(page.url()).pathname, "/login", "nobody is signed in to an empty shell");
});

scenario("a description keeps its formatting in both editor modes", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Written", { visit: false });
  const issueKey = await createIssue(page, key, "needs a plan", { visit: false });

  // Typed markdown shortcuts format as they are typed; the saved document
  // is drawn as headings and lists.
  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Add description");
  await page.waitForSelector('#issue-description[data-editor="rich"]', { timeout: WAIT });
  await page.type("#issue-description", "## Plan");
  await page.keyboard.press("Enter");
  await page.type("#issue-description", "- first step");
  await clickButton(page, "Save description");
  await page.waitForSelector("[data-description] h3", { timeout: WAIT });
  expect.equal(await page.$eval("[data-description] h3", (el) => el.textContent), "Plan", "the heading is drawn as one");
  expect.equal(await page.$eval("[data-description] li", (el) => el.textContent), "first step", "and the list as one");

  // The markdown face shows the same document as text and takes edits.
  await clickButton(page, "Edit");
  await setEditorMode(page, "markdown");
  await page.waitForSelector("textarea#issue-description", { timeout: WAIT });
  const source = await editorText(page, "#issue-description");
  expect.contains(source, "## Plan", "the heading reads as markdown");
  expect.contains(source, "- first step", "and so does the list");
  await page.type("#issue-description", "\n- second step");
  await clickButton(page, "Save description");
  await page.waitForFunction(() => document.querySelectorAll("[data-description] li").length === 2, { timeout: WAIT });

  // The choice is this browser's, and it survives a reload.
  await page.reload();
  await waitForApp(page);
  await clickButton(page, "Edit");
  await page.waitForSelector("textarea#issue-description", { timeout: WAIT });
  await setEditorMode(page, "rich");
});

scenario("a comment can be added and appears on the issue", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Talking", { visit: false });
  const issueKey = await createIssue(page, key, "needs discussion", { visit: false });

  await goto(page, `/issues/${issueKey}`);
  await page.type("#new-comment", "This looks like a duplicate.");
  await clickButton(page, "Comment");

  await page.waitForFunction(() => document.body.innerText.includes("This looks like a duplicate."), { timeout: WAIT });
});

scenario("the issue list filters by scope", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Filtering", { visit: false });
  const open = await createIssue(page, key, "still open here", { visit: false });
  await createIssue(page, key, "will be closed", { visit: false });

  // Close the second one.
  await goto(page, `/projects/${key}`);
  const closedKey = `${key}-2`;
  await goto(page, `/issues/${closedKey}`);
  await clickButton(page, "Close");
  await page.waitForFunction(() => document.body.innerText.includes("Done"), { timeout: WAIT });

  await goto(page, `/projects/${key}`);
  const openView = await bodyText(page);
  expect.contains(openView, open, "the open issue is listed by default");
  expect.notContains(openView, "will be closed", "the closed issue is hidden from the open view");

  await clickButton(page, "all");
  await page.waitForFunction(() => document.body.innerText.includes("will be closed"), { timeout: WAIT });
});

scenario("a project's counts follow its issues", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Counting", { visit: false });
  await createIssue(page, key, "one", { visit: false });
  await createIssue(page, key, "two", { visit: false });

  await openProjectRow(page, key);
  const row = await textOf(page, `[data-project="${key}"]`);
  expect.contains(row, "2 open", "open count");
  expect.contains(row, "2 total", "total count");
});

// A project points at its documentation elsewhere, and the sidebar offers it
// one click from the board, in a tab of its own.
scenario("a project's sidebar links to its documentation", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Documented", { visit: false });
  await goto(page, `/projects/${key}/settings`);
  expect.truthy((await page.$("[data-project-docs]")) === null, "a new project links to no docs");

  await fill(page, "Documentation", "wiki.example.com");
  await clickButton(page, "Save changes");
  await page.waitForFunction(() => document.querySelector("#field-documentation")?.getAttribute("aria-invalid") === "true", { timeout: WAIT });
  expect.contains(await bodyText(page), "http:// or https://", "the refusal says what an address looks like");

  const address = "https://wiki.example.com/spaces/docs";
  await fill(page, "Documentation", address);
  await fill(page, "Link name", "Team wiki");
  await clickButton(page, "Save changes");
  const link = await page.waitForSelector(`[data-project-docs="${address}"]`, { timeout: WAIT });
  const shown = await link.evaluate((a) => ({ text: a.textContent.trim(), href: a.href, target: a.target, rel: a.rel }));
  expect.equal(shown.text, "Team wiki", "the entry wears the link's name");
  expect.equal(shown.href, address, "the entry opens the documentation");
  expect.equal(shown.target, "_blank", "the documentation opens in a tab of its own");
  expect.contains(shown.rel, "noopener", "the opened page cannot reach back into Armature");

  await fill(page, "Documentation", "");
  await clickButton(page, "Save changes");
  await page.waitForFunction(() => !document.querySelector("[data-project-docs]"), { timeout: WAIT });
});

// Archiving deletes nothing, so it asks nothing: its toast's Undo brings the
// project back, and so does Restore on its row once the archived are shown.
scenario("an archived project comes back by Undo and by Restore", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Brought back", { visit: false });
  await openProjectRow(page, key);

  // The toast lasts seconds, so Undo is pressed as soon as it says the project went.
  await pickProjectAction(page, key, "archive-project");
  await page.waitForFunction(() => document.body.innerText.includes("Archived Brought back"), { timeout: WAIT });
  await clickButton(page, "Undo");
  await settled(page);
  await reload(page);
  await waitForProjectRow(page, key, { archived: false });

  await pickProjectAction(page, key, "archive-project");
  await page.waitForFunction((k) => !document.querySelector(`[data-project="${k}"]`), { timeout: WAIT }, key);
  await press(page, "[data-show-archived]");
  await waitForProjectRow(page, key, { archived: true });
  await pickProjectAction(page, key, "restore-project");
  await waitForProjectRow(page, key, { archived: false });

  await reload(page);
  await waitForProjectRow(page, key, { archived: false });
});

// An archived project's settings say so and stay closed, since every save would
// be refused, until Restore on the page brings it back.
scenario("an archived project's settings are closed until it is restored", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Put away", { visit: false });
  await goto(page, `/projects/${key}/settings`);

  await press(page, '[data-action="archive-project"]');
  await page.waitForSelector("[data-archived-notice]", { timeout: WAIT });
  expect.contains(await textOf(page, "[data-archived-notice]"), "Put away is archived", "the page says the project is archived");
  expect.truthy(await page.$eval("#field-project-name", (input) => input.matches(":disabled")), "the name cannot be changed while archived");

  await press(page, '[data-action="restore-project"]');
  await page.waitForFunction(() => !document.querySelector("[data-archived-notice]"), { timeout: WAIT });
  await page.waitForFunction(() => document.querySelector("#field-project-name")?.matches(":enabled"), { timeout: WAIT });
  expect.truthy((await page.$('[data-action="archive-project"]')) !== null, "the project can be archived again");
});

// A project's setup is tabs of its settings, not a group of the sidebar; the
// old addresses of those pages land on their tabs, search and hash kept.
scenario("a project's setup lives in the tabs of its settings, and its old addresses land on them", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Set up in tabs", { visit: false });
  await goto(page, `/projects/${key}`);
  await page.waitForSelector(`[data-sidebar] a[href="/projects/${key}/settings"]`, { timeout: WAIT });
  expect.equal(await page.$('[data-sidebar-group="project-admin"]'), null, "the sidebar has no project-admin group");
  const entries = await page.$$eval(`[data-sidebar] a[href^="/projects/${key}/settings"]`, (links) => links.length);
  expect.equal(entries, 1, "and one Settings entry");

  const tabs = await openAndListTabs(page, key);
  for (const tab of ["general", "workflows", "fields", "issue-view"]) expect.truthy(tabs.includes(tab), `the settings have a ${tab} tab: ${tabs.join(", ")}`);
  for (const tab of tabs.filter((each) => each !== "general")) {
    await page.click(`[data-settings-tab="${tab}"]`);
    await waitForPath(page, `/projects/${key}/settings/${tab}`);
    expect.equal(await page.$eval(`[data-settings-tab="${tab}"]`, (el) => el.getAttribute("aria-selected")), "true", `${tab} is the chosen tab`);
  }

  await openSetupTab(page, key, "fields");
  await goto(page, `/projects/${key}/fields?q=area#top`);
  await waitForPath(page, `/projects/${key}/settings/fields`);
  expect.equal(await page.evaluate(() => `${location.search}${location.hash}`), "?q=area#top", "the old address keeps its search and hash");
  await page.waitForSelector('[data-settings-tab="fields"][aria-selected="true"]', { timeout: WAIT });
});

/** Opens the project's settings from the sidebar and names the tabs on offer, by their slugs. */
async function openAndListTabs(page, key) {
  await page.click(`[data-sidebar] a[href="/projects/${key}/settings"]`);
  await page.waitForSelector("[data-settings-tab]", { timeout: WAIT });
  return page.$$eval("[data-settings-tab]", (tabs) => tabs.map((tab) => tab.getAttribute("data-settings-tab")));
}

scenario("a reader of a project sees no Settings entry, and its address tells them to ask", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Read only setup", { visit: false });
  const who = await inviteMember(page, "settingsreader");
  await acceptInvite(page, who, { visit: false });
  await signIn(page, owner.email);
  await waitForApp(page);
  await revokeAll(page, who.name);
  await grantRole(page, { who: who.name, role: "Reader", projectKey: key });

  await signIn(page, who.email);
  await waitForApp(page);
  await goto(page, `/projects/${key}`);
  await page.waitForSelector("[data-sidebar] a[href$='/board'], [data-sidebar] a[href='/projects/" + key + "']", { timeout: WAIT });
  expect.equal(await page.$(`[data-sidebar] a[href="/projects/${key}/settings"]`), null, "a reader is offered no Settings");
  await goto(page, `/projects/${key}/teams`);
  await waitForPath(page, `/projects/${key}/teams`);
  await page.waitForFunction(() => document.body.innerText.includes("Only the project's administrators change its settings"), { timeout: WAIT });
  expect.equal(await page.$("[data-settings-tab]"), null, "and no tab opens for them");
});
