// Projects, issues and the workflow they move through; people coming and going.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  deleteMyAccount,
  bodyText,
  clickButton,
  createIssue,
  createProject,
  expect,
  goto,
  inviteAddress,
  inviteMember,
  tryAcceptInvite,
  removeMember,
  reload,
  signIn,
  signOut,
  signUp,
  textOf,
  unique,
  WAIT,
  editorText,
  setEditorMode,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("a project can be created and is listed", async ({ page }) => {
  await signUp(page);

  const key = await createProject(page, "Customer Portal");
  expect.truthy(key.length >= 2, `project key ${JSON.stringify(key)} looks wrong`);

  await goto(page, "/projects");
  const listing = await bodyText(page);
  expect.contains(listing, "Customer Portal", "the project is listed");
  expect.contains(listing, key, "its key is shown");
});

scenario("issues get sequential keys within their project", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Sequencing");

  const first = await createIssue(page, key, "the very first issue");
  const second = await createIssue(page, key, "the second issue");

  expect.equal(first, `${key}-1`, "first issue key");
  expect.equal(second, `${key}-2`, "second issue key");
});

scenario("a new issue starts in the workflow's first status", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Starting");
  const issueKey = await createIssue(page, key, "brand new work");

  await goto(page, `/issues/${issueKey}`);
  const detail = await bodyText(page);
  expect.contains(detail, "To Do", "starts in To Do");
  expect.contains(detail, "Unassigned", "starts unassigned");
});

scenario("only the workflow's legal moves are offered", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Legal moves");
  const issueKey = await createIssue(page, key, "walk the workflow");

  await goto(page, `/issues/${issueKey}`);

  const buttonsFromTodo = await page.$$eval("button", (bs) => bs.map((b) => b.textContent.trim()));
  expect.truthy(buttonsFromTodo.includes("Start progress"), "Start progress is offered from To Do");
  expect.truthy(buttonsFromTodo.includes("Close"), "the global Close is offered");
  expect.truthy(
    !buttonsFromTodo.includes("Approve"),
    `Approve should not be reachable from To Do, saw: ${buttonsFromTodo.join(", ")}`,
  );
});

scenario("taking a transition applies the workflow's post-functions", async ({ page }) => {
  const who = await signUp(page);
  const key = await createProject(page, "Post functions");
  const issueKey = await createIssue(page, key, "assign on start");

  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");

  // Start progress assigns the issue to whoever took it.
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });
  const after = await bodyText(page);
  expect.contains(after, "In Progress", "status moved");
  expect.contains(after, who.name, "the workflow assigned the issue to the person who started it");
});

scenario("the changelog records every move", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Changelog");
  const issueKey = await createIssue(page, key, "keep a record");

  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");

  // The history is its own query, so wait for its content rather than for the
  // status badge that changes first.
  await page.waitForFunction(
    () => document.body.innerText.includes("moved this from To Do to In Progress"),
    { timeout: WAIT },
  );

  const history = await bodyText(page);
  expect.containsInsensitive(history, "History", "the history section is shown");
  expect.contains(history, `created ${issueKey}`, "the creation is the first entry");
});

scenario("a person can take their data and go, and their comment stays", async ({ page }) => {
  const owner = await signUp(page);
  const key = await createProject(page, "Records");
  const issueKey = await createIssue(page, key, "keep the lights on");
  const leaver = await inviteMember(page, "leaver");
  await acceptInvite(page, leaver);

  await goto(page, `/issues/${issueKey}`);
  await page.type("#new-comment", "I will look at this on Monday.");
  await clickButton(page, "Comment");
  await page.waitForFunction(() => document.body.innerText.includes("I will look at this on Monday."), { timeout: WAIT });

  // The export is one file with everything in it.
  const exported = await page.evaluate(async () => {
    const response = await fetch("/api/v1/auth/me/export", { credentials: "include" });
    return { status: response.status, disposition: response.headers.get("content-disposition"), body: await response.text() };
  });
  expect.equal(exported.status, 200, "the export is served");
  expect.contains(exported.disposition ?? "", "attachment", "as a file");
  expect.contains(exported.body, "I will look at this on Monday.", "holding what they wrote");

  await deleteMyAccount(page);
  await signIn(page, leaver.email);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains((await textOf(page, "[role=alert]")).toLowerCase(), "invalid email or password", "the address no longer signs in");

  await signIn(page, owner.email);
  await goto(page, `/issues/${issueKey}`);
  await page.waitForFunction(() => document.body.innerText.includes("I will look at this on Monday."), { timeout: WAIT });
  expect.contains(await bodyText(page), "Former user", "the comment stays, by Former user");
});

scenario("an administrator lets a member go", async ({ page }) => {
  const owner = await signUp(page);
  const member = await inviteMember(page, "goer");
  await acceptInvite(page, member);
  await goto(page, "/");
  await page.waitForSelector("header", { timeout: WAIT });

  await signIn(page, owner.email);
  await removeMember(page, member.name);
  await goto(page, "/settings/access");
  await page.click('[data-access-tab="Members"]');
  await page.waitForSelector("[data-member]", { timeout: WAIT });
  expect.truthy(!(await bodyText(page)).includes(member.name), "the member is gone from the list");
});

scenario("an invitation to an address with an account is that account's to accept", async ({ page }) => {
  const owner = await signUp(page);
  await signOut(page);
  const invitee = await signUp(page, unique("invitee"));
  await signIn(page, owner.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  const token = await inviteAddress(page, invitee.email);

  // Whoever holds the link cannot take it for the account, whatever password they give.
  await signOut(page);
  await goto(page, "/login");
  expect.equal(await tryAcceptInvite(page, { token, name: "Somebody Else" }), "sign_in_to_accept", "nobody signed in is asked to sign in");

  // Signed in as the invitee, it is theirs.
  await signIn(page, invitee.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  expect.equal(await tryAcceptInvite(page, { token }), "", "the invitee takes their own invitation");
  await goto(page, "/");
  await waitForApp(page);
  expect.contains(await bodyText(page), owner.org, "and is in the organization that invited them");
});

scenario("a description keeps its formatting in both editor modes", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Written");
  const issueKey = await createIssue(page, key, "needs a plan");

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
  await page.waitForSelector('textarea#issue-description', { timeout: WAIT });
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
  await page.waitForSelector('textarea#issue-description', { timeout: WAIT });
  await setEditorMode(page, "rich");
});

scenario("a comment can be added and appears on the issue", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Talking");
  const issueKey = await createIssue(page, key, "needs discussion");

  await goto(page, `/issues/${issueKey}`);
  await page.type("#new-comment", "This looks like a duplicate.");
  await clickButton(page, "Comment");

  await page.waitForFunction(
    () => document.body.innerText.includes("This looks like a duplicate."),
    { timeout: WAIT },
  );
});

scenario("the issue list filters by scope", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Filtering");
  const open = await createIssue(page, key, "still open here");
  await createIssue(page, key, "will be closed");

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
  await signUp(page);
  const key = await createProject(page, "Counting");
  await createIssue(page, key, "one");
  await createIssue(page, key, "two");

  await goto(page, "/projects");
  const listing = await bodyText(page);
  expect.contains(listing, "2 open", "open count");
  expect.contains(listing, "2 total", "total count");
});
