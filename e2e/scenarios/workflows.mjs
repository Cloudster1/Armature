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
  goto,
  grantRole,
  inviteMember,
  movesOffered,
  nodePosition,
  openWorkflowDesign,
  replaceValue,
  revokeAll,
  selectByLabel,
  selectNode,
  signIn,
  signUp,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("a project follows the organization's workflows until it says otherwise", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Following");

  const rows = await assignmentRows(page, key);
  expect.contains(await bodyText(page), "This project follows the organization", "it says so plainly");
  for (const [type, text] of Object.entries(rows)) {
    expect.contains(text, "Organization", `${type} is decided by the organization`);
  }
});

scenario("a project overrides one issue type and inherits the rest", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Overriding");

  await createWorkflow(page, {
    name: "Bug triage",
    statuses: ["To Do", "Done"],
    opensIn: "To Do",
    transitions: [{ name: "Fixed", from: "To Do", to: "Done" }],
  });
  await createScheme(page, {
    name: "Triage scheme",
    issueType: "Bug",
    workflow: "Bug triage",
  });

  await goto(page, `/projects/${key}/workflows`);
  await selectByLabel(page, "#project-scheme", "Triage scheme");
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(
    () => document.body.innerText.includes("This project uses its own scheme"),
    { timeout: WAIT },
  );

  const rows = await assignmentRows(page, key);
  expect.contains(rows.Bug, "Bug triage", "bugs use the project's workflow");
  expect.contains(rows.Bug, "This project", "and the project is named as the one that decided");
  expect.contains(rows.Task, "Organization", "tasks still follow the organization");

  // The table is a description of behaviour, so the behaviour has to match it.
  const bug = await createIssue(page, key, "the override decides this one", "Bug");
  const bugMoves = await movesOffered(page, bug);
  expect.truthy(bugMoves.includes("Fixed"), "the bug offers the override's move");
  expect.truthy(!bugMoves.includes("Start progress"), "and not the organization's");

  const task = await createIssue(page, key, "this one is ordinary", "Task");
  const taskMoves = await movesOffered(page, task);
  expect.truthy(taskMoves.includes("Start progress"), "the task offers the organization's move");
  expect.truthy(!taskMoves.includes("Fixed"), "and not the override's");
});

scenario("the project's workflow page draws the workflow each issue type follows", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Drawn");

  await createWorkflow(page, {
    name: "Bug triage",
    statuses: ["To Do", "Done"],
    opensIn: "To Do",
    transitions: [{ name: "Fixed", from: "To Do", to: "Done" }],
  });
  await createScheme(page, { name: "Drawn scheme", issueType: "Bug", workflow: "Bug triage" });

  await goto(page, `/projects/${key}/workflows`);
  await selectByLabel(page, "#project-scheme", "Drawn scheme");
  await clickButton(page, "Use this scheme");
  await page.waitForSelector('[data-testid="workflow-assignments"] [data-assignment="Bug"]', { timeout: WAIT });

  // The picture follows the row that is clicked, and nothing on it can be grabbed.
  await page.click('[data-assignment="Bug"] td:first-child');
  await page.waitForSelector('[data-workflow-graph="Bug triage"] [data-workflow-edge="Fixed"]', { timeout: WAIT });
  expect.equal(await page.$$eval('[data-workflow-graph] [role="button"]', (els) => els.length), 0, "the drawing has no buttons");
  expect.equal(await page.$$eval("[data-workflow-graph] [data-connect-handle]", (els) => els.length), 0, "and no rings to drag from");

  await page.click('[data-assignment="Task"] td:first-child');
  await page.waitForSelector('[data-workflow-graph="Default software workflow"] [data-workflow-node="In Progress"]', { timeout: WAIT });
  expect.equal(await page.$('[data-workflow-graph="Bug triage"]'), null, "one drawing at a time");
});

scenario("a project administrator picks a workflow for one issue type without drawing one", async ({ page }) => {
  const owner = await signUp(page);
  const key = await createProject(page, "Mapped");
  await createWorkflow(page, {
    name: "Bug triage",
    statuses: ["To Do", "Done"],
    opensIn: "To Do",
    transitions: [{ name: "Fixed", from: "To Do", to: "Done" }],
  });

  const who = await inviteMember(page, "projadmin");
  await acceptInvite(page, who);
  await signIn(page, owner.email);
  await waitForApp(page);
  await revokeAll(page, who.name);
  await grantRole(page, { who: who.name, role: "Project administrator", projectKey: key });

  await signIn(page, who.email);
  await waitForApp(page);

  // One select per row, and no scheme to name: the scheme is the server's bookkeeping.
  await assignWorkflow(page, key, "Bug", "Bug triage");
  const rows = await assignmentRows(page, key);
  expect.contains(rows.Bug, "Bug triage", "bugs follow the workflow the row was given");
  expect.contains(rows.Bug, "This project", "and the project is named as the one that decided");
  expect.contains(rows.Task, "Organization", "tasks still follow the organization");
  expect.equal(await page.$("#project-scheme"), null, "the whole-scheme picker is the organization's, not theirs");

  const bug = await createIssue(page, key, "mapped by a row", "Bug");
  const moves = await movesOffered(page, bug);
  expect.truthy(moves.includes("Fixed"), "the bug offers the mapped workflow's move");
  expect.truthy(!moves.includes("Start progress"), "and not the organization's");

  // Handing the last type back leaves nothing of the project's own behind.
  await assignWorkflow(page, key, "Bug", "Follow the organization");
  await page.waitForFunction(
    () => document.body.innerText.includes("This project follows the organization"),
    { timeout: WAIT },
  );
});

scenario("a project can hand the decision back to the organization", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Handing back");

  await createWorkflow(page, { name: "Lean", statuses: ["To Do", "Done"], opensIn: "Done" });
  await createScheme(page, { name: "Lean scheme", issueType: "Bug", workflow: "Lean" });

  await goto(page, `/projects/${key}/workflows`);
  await selectByLabel(page, "#project-scheme", "Lean scheme");
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(
    () => document.body.innerText.includes("This project uses its own scheme"),
    { timeout: WAIT },
  );

  await clickButton(page, "Follow the organization");
  await page.waitForFunction(
    () => document.body.innerText.includes("This project follows the organization"),
    { timeout: WAIT },
  );

  const rows = await assignmentRows(page, key);
  expect.contains(rows.Bug, "Organization", "bugs are the organization's business again");
});

scenario("a workflow that could not work is refused with a reason", async ({ page }) => {
  await signUp(page);
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
  await page.waitForFunction(
    () => [...document.querySelectorAll('[role="alert"]')].some((el) => /every transition needs a name/i.test(el.textContent)),
    { timeout: WAIT },
  );
});

scenario("a workflow drawn on the canvas comes back as it was left, rules included", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Drawn up");

  await goto(page, "/settings/workflows");
  await clickButton(page, "New workflow");
  await page.waitForSelector("#field-name", { timeout: WAIT });
  await page.type("#field-name", "Drawn");
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
  await openWorkflowDesign(page, "Drawn");
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
  // cannot be started without a comment.
  await createScheme(page, { name: "Drawn scheme", issueType: "Bug", workflow: "Drawn" });
  await goto(page, `/projects/${key}/workflows`);
  await selectByLabel(page, "#project-scheme", "Drawn scheme");
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(
    () => document.body.innerText.includes("This project uses its own scheme"),
    { timeout: WAIT },
  );
  const issueKey = await createIssue(page, key, "needs a word first", "Bug");
  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start");
  await page.waitForFunction(
    () => document.body.innerText.includes("This transition needs a comment."),
    { timeout: WAIT },
  );
});

scenario("deleting a scheme a project uses is refused and says which", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Still using");

  await createWorkflow(page, { name: "Lean", statuses: ["To Do", "Done"], opensIn: "To Do" });
  await createScheme(page, { name: "Lean scheme", issueType: "Bug", workflow: "Lean" });

  await goto(page, `/projects/${key}/workflows`);
  await selectByLabel(page, "#project-scheme", "Lean scheme");
  await clickButton(page, "Use this scheme");
  await page.waitForFunction(
    () => document.body.innerText.includes("This project uses its own scheme"),
    { timeout: WAIT },
  );

  await deleteScheme(page, "Lean scheme");
  await page.waitForFunction((k) => document.body.innerText.includes(`${k} still uses it`), { timeout: WAIT }, key);
});
