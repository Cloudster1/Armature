// Workflows, schemes and the designer.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  deleteScheme,
  assignmentRows,
  assignWorkflow,
  bodyText,
  clickButton,
  createIssue,
  createProject,
  createScheme,
  createWorkflow,
  dragConnect,
  dragNode,
  expect,
  followWorkflow,
  goto,
  grantRole,
  inviteMember,
  movesOffered,
  nodePosition,
  openWorkflowDesign,
  pickBySearch,
  press,
  replaceValue,
  revokeAll,
  selectByLabel,
  selectNode,
  signIn,
  signUp,
  textOf,
  unique,
  uniqueName,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("a project follows the default workflows until it says otherwise", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Following", { visit: false });

  const rows = await assignmentRows(page, key);
  expect.contains(await bodyText(page), "This project follows the default", "it says so plainly");
  for (const [type, text] of Object.entries(rows)) {
    expect.contains(text, "All projects", `${type} is decided for all projects`);
  }
});

scenario("a project overrides one issue type and inherits the rest", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  // Workflows and schemes are everybody's, so these have names nobody else took.
  const triage = uniqueName("Bug triage");
  const triageScheme = uniqueName("Triage scheme");
  const key = await createProject(page, "Overriding", { visit: false });

  await createWorkflow(page, {
    name: triage,
    statuses: ["To Do", "Done"],
    opensIn: "To Do",
    transitions: [{ name: "Fixed", from: "To Do", to: "Done" }],
  });
  await createScheme(page, {
    name: triageScheme,
    issueType: "Bug",
    workflow: triage,
  });

  await goto(page, `/projects/${key}/workflows`);
  await pickBySearch(page, "#project-scheme", triageScheme);
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(() => document.body.innerText.includes("This project uses its own scheme"), { timeout: WAIT });

  const rows = await assignmentRows(page, key);
  expect.contains(rows.Bug, triage, "bugs use the project's workflow");
  expect.contains(rows.Bug, "This project", "and the project is named as the one that decided");
  expect.contains(rows.Task, "All projects", "tasks still follow the default");

  // The table is a description of behaviour, so the behaviour has to match it.
  const bug = await createIssue(page, key, "the override decides this one", { type: "Bug", visit: false });
  const bugMoves = await movesOffered(page, bug);
  expect.truthy(bugMoves.includes("Fixed"), "the bug offers the override's move");
  expect.truthy(!bugMoves.includes("Start progress"), "and not the default's");

  const task = await createIssue(page, key, "this one is ordinary", { type: "Task", visit: false });
  const taskMoves = await movesOffered(page, task);
  expect.truthy(taskMoves.includes("Start progress"), "the task offers the default's move");
  expect.truthy(!taskMoves.includes("Fixed"), "and not the override's");
});

scenario("the project's workflow page draws the workflow each issue type follows", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  // Workflows and schemes are everybody's, so these have names nobody else took.
  const triage = uniqueName("Bug triage");
  const drawnScheme = uniqueName("Drawn scheme");
  const key = await createProject(page, "Drawn", { visit: false });

  await createWorkflow(page, {
    name: triage,
    statuses: ["To Do", "Done"],
    opensIn: "To Do",
    transitions: [{ name: "Fixed", from: "To Do", to: "Done" }],
  });
  await createScheme(page, { name: drawnScheme, issueType: "Bug", workflow: triage });

  await goto(page, `/projects/${key}/workflows`);
  await pickBySearch(page, "#project-scheme", drawnScheme);
  await clickButton(page, "Use this scheme");
  await page.waitForSelector('[data-testid="workflow-assignments"] [data-assignment="Bug"]', { timeout: WAIT });

  // The picture follows the row that is clicked, and nothing on it can be grabbed.
  await page.click('[data-assignment="Bug"] td:first-child');
  await page.waitForSelector(`[data-workflow-graph="${triage}"] [data-workflow-edge="Fixed"]`, { timeout: WAIT });
  expect.equal(await page.$$eval('[data-workflow-graph] [role="button"]', (els) => els.length), 0, "the drawing has no buttons");
  expect.equal(await page.$$eval("[data-workflow-graph] [data-connect-handle]", (els) => els.length), 0, "and no rings to drag from");

  await page.click('[data-assignment="Task"] td:first-child');
  await page.waitForSelector('[data-workflow-graph="Default software workflow"] [data-workflow-node="In Progress"]', { timeout: WAIT });
  expect.equal(await page.$(`[data-workflow-graph="${triage}"]`), null, "one drawing at a time");
});

scenario("a project administrator picks a workflow for one issue type without drawing one", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  // Workflows and schemes are everybody's, so these have names nobody else took.
  const triage = uniqueName("Bug triage");
  const key = await createProject(page, "Mapped", { visit: false });
  await createWorkflow(page, {
    name: triage,
    statuses: ["To Do", "Done"],
    opensIn: "To Do",
    transitions: [{ name: "Fixed", from: "To Do", to: "Done" }],
  });

  const who = await inviteMember(page, "projadmin");
  await acceptInvite(page, who, { visit: false });
  await signIn(page, owner.email);
  await waitForApp(page);
  await revokeAll(page, who.name);
  await grantRole(page, { who: who.name, role: "Project administrator", projectKey: key });

  await signIn(page, who.email);
  await waitForApp(page);

  // One select per row, and no scheme to name: the scheme is the server's bookkeeping.
  await assignWorkflow(page, key, "Bug", triage);
  const rows = await assignmentRows(page, key);
  expect.contains(rows.Bug, triage, "bugs follow the workflow the row was given");
  expect.contains(rows.Bug, "This project", "and the project is named as the one that decided");
  expect.contains(rows.Task, "All projects", "tasks still follow the default");
  expect.equal(await page.$("#project-scheme"), null, "the whole-scheme picker is an administrator's, not theirs");

  const bug = await createIssue(page, key, "mapped by a row", { type: "Bug", visit: false });
  const moves = await movesOffered(page, bug);
  expect.truthy(moves.includes("Fixed"), "the bug offers the mapped workflow's move");
  expect.truthy(!moves.includes("Start progress"), "and not the default's");

  // Handing the last type back leaves nothing of the project's own behind.
  await assignWorkflow(page, key, "Bug", "Follow the default");
  await page.waitForFunction(() => document.body.innerText.includes("This project follows the default"), { timeout: WAIT });
});

scenario("a project can hand the decision back to the default", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  // Workflows and schemes are everybody's, so these have names nobody else took.
  const leanScheme = uniqueName("Lean scheme");
  const lean = uniqueName("Lean");
  const key = await createProject(page, "Handing back", { visit: false });

  await createWorkflow(page, { name: lean, statuses: ["To Do", "Done"], opensIn: "Done" });
  await createScheme(page, { name: leanScheme, issueType: "Bug", workflow: lean });

  await goto(page, `/projects/${key}/workflows`);
  await pickBySearch(page, "#project-scheme", leanScheme);
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(() => document.body.innerText.includes("This project uses its own scheme"), { timeout: WAIT });

  await clickButton(page, "Follow the default");
  await page.waitForFunction(() => document.body.innerText.includes("This project follows the default"), { timeout: WAIT });

  const rows = await assignmentRows(page, key);
  expect.contains(rows.Bug, "All projects", "bugs follow the default again");
});

scenario("a workflow that could not work is refused with a reason", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  await goto(page, "/settings/workflows");
  await clickButton(page, "New workflow");
  await page.waitForSelector("#field-name", { timeout: WAIT });
  await page.type("#field-name", "Nameless move");
  for (const status of ["To Do", "Done"]) {
    await selectByLabel(page, "#field-add-status", status);
    await clickButton(page, "Add status");
  }
  await dragConnect(page, "To Do", "Done");
  await replaceValue(page, "#field-transition-name", "");

  // The designer says so first, and the server says so when asked anyway.
  expect.contains(await bodyText(page), "Every transition needs a name", "the designer warns before the round trip");
  await clickButton(page, "Create workflow");
  await page.waitForFunction(() => [...document.querySelectorAll('[role="alert"]')].some((el) => /every transition needs a name/i.test(el.textContent)), {
    timeout: WAIT,
  });
});

scenario("a workflow drawn on the canvas comes back as it was left, rules included", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  // Workflows and schemes are everybody's, so these have names nobody else took.
  const drawnScheme = uniqueName("Drawn scheme");
  const drawn = uniqueName("Drawn");
  const key = await createProject(page, "Drawn up", { visit: false });

  await goto(page, "/settings/workflows");
  await clickButton(page, "New workflow");
  await page.waitForSelector("#field-name", { timeout: WAIT });
  await page.type("#field-name", drawn);
  for (const status of ["To Do", "In Progress", "Done"]) {
    await selectByLabel(page, "#field-add-status", status);
    await clickButton(page, "Add status");
  }

  // Statuses are laid out by category until somebody moves one.
  const before = await nodePosition(page, "In Progress");
  const after = await dragNode(page, "In Progress", 96, 120);
  expect.equal(after.x - before.x, 96, "the status moved right by the drag");
  expect.equal(after.y - before.y, 120, "and down by the drag");

  // A transition drawn from the ring is named after where it goes, and takes a rule.
  await dragConnect(page, "To Do", "In Progress");
  await replaceValue(page, "#field-transition-name", "Start");
  await selectByLabel(page, "#field-rule", "Comment required");
  await clickButton(page, "Add rule");
  await page.waitForSelector('[data-rule="validator.comment_required"]', { timeout: WAIT });

  // One from anywhere, with a post-function that needs configuring.
  await dragConnect(page, "any status", "Done");
  await replaceValue(page, "#field-transition-name", "Close");
  await selectByLabel(page, "#field-rule", "Add a comment");
  await page.type("#field-rule-text", "Closed from the canvas.");
  await clickButton(page, "Add rule");
  await page.waitForSelector('[data-rule="postfunction.add_comment"]', { timeout: WAIT });

  await clickButton(page, "Create workflow");
  await waitForPath(page, "/settings/workflows");

  // Reopened, the picture is the one that was saved.
  await openWorkflowDesign(page, drawn);
  await page.waitForSelector('[data-workflow-node="In Progress"]', { timeout: WAIT });
  const reopened = await nodePosition(page, "In Progress");
  expect.equal(reopened.x, after.x, "In Progress is where it was dropped, across");
  expect.equal(reopened.y, after.y, "and down");
  await page.waitForSelector('[data-workflow-edge="Start"]', { timeout: WAIT });
  await page.waitForSelector('[data-workflow-edge="Close"]', { timeout: WAIT });
  await selectNode(page, "To Do");
  await clickButton(page, "Start to In Progress");
  await page.waitForSelector('[data-rule="validator.comment_required"]', { timeout: WAIT });

  // And the rule drawn there is the rule that holds: a bug on this workflow
  // is not started until somebody says why.
  await createScheme(page, { name: drawnScheme, issueType: "Bug", workflow: drawn });
  await goto(page, `/projects/${key}/workflows`);
  await pickBySearch(page, "#project-scheme", drawnScheme);
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(() => document.body.innerText.includes("This project uses its own scheme"), { timeout: WAIT });
  const issueKey = await createIssue(page, key, "needs a word first", { type: "Bug", visit: false });
  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start");
  await page.waitForSelector("[data-transition-dialog] textarea", { timeout: WAIT });
  await page.type("[data-transition-dialog] textarea", "Picking this up now.");
  await press(page, '[data-transition-dialog] [data-action="transition-go"]');
  await page.waitForSelector("header .status-in_progress", { timeout: WAIT });
});

scenario("a move that needs a resolution asks for one on the issue page and records it", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Resolving", { visit: false });
  // Workflows are everybody's, so this one has a name nobody else took.
  await followWorkflow(page, key, "Bug", {
    name: uniqueName("Closed with a reason"),
    statuses: ["To Do", "Done"],
    rules: [
      { kind: "validator", type: "validator.resolution_required" },
      { kind: "postfunction", type: "postfunction.set_resolved" },
    ],
  });
  const issueKey = await createIssue(page, key, "nobody wants this", { type: "Bug", visit: false });

  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "To Done");
  await page.waitForSelector("[data-transition-dialog] select", { timeout: WAIT });
  await selectByLabel(page, "[data-transition-dialog] select", "Won't do");
  await press(page, '[data-transition-dialog] [data-action="transition-go"]');

  await page.waitForSelector("header .status-done", { timeout: WAIT });
  await page.waitForFunction(() => document.querySelector("[data-issue-history]")?.innerText.includes("resolved this as Won't do"), { timeout: WAIT });
});

scenario("a scheme a project uses is not offered for deletion and says which project to move", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  // Workflows and schemes are everybody's, so these have names nobody else took.
  const leanScheme = uniqueName("Lean scheme");
  const lean = uniqueName("Lean");
  const key = await createProject(page, "Still using", { visit: false });

  await createWorkflow(page, { name: lean, statuses: ["To Do", "Done"], opensIn: "To Do" });
  await createScheme(page, { name: leanScheme, issueType: "Bug", workflow: lean });

  await goto(page, `/projects/${key}/workflows`);
  await pickBySearch(page, "#project-scheme", leanScheme);
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(() => document.body.innerText.includes("This project uses its own scheme"), { timeout: WAIT });

  await goto(page, `/settings/workflows/schemes?q=${encodeURIComponent(leanScheme)}`);
  const card = `[data-scheme-card="${leanScheme}"]`;
  await page.waitForSelector(`${card} [data-scheme-move-first]`, { timeout: WAIT });
  expect.contains(await textOf(page, `${card} [data-scheme-move-first]`), `move ${key} to another scheme first`, "the card names the project to move");
  expect.truthy(!(await page.$(`${card} [data-action="delete"]`)), "and offers no delete the server would refuse");

  // Once the project hands the decision back, the scheme can go.
  await goto(page, `/projects/${key}/workflows`);
  await clickButton(page, "Follow the default");
  await page.waitForFunction(() => document.body.innerText.includes("This project follows the default"), { timeout: WAIT });
  await deleteScheme(page, leanScheme);
  await page.waitForFunction((sel) => !document.querySelector(sel), { timeout: WAIT }, card);
});
