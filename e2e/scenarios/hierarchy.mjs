// Epics, their children and the tree.

import { scenario } from "../runner.mjs";
import {
  addChild,
  bodyText,
  clickButton,
  createIssue,
  createProject,
  expect,
  goto,
  selectByLabel,
  signUp,
  treeRows,
  WAIT,
} from "../helpers.mjs";

scenario("an epic holds stories and the tree shows the nesting", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Nesting");

  const epic = await createIssue(page, key, "Password and recovery", "Epic");
  const story = await addChild(page, epic, "Add password reset by email", "Story");

  await goto(page, `/projects/${key}/hierarchy`);
  await page.waitForFunction((s) => document.body.innerText.includes(s), { timeout: WAIT }, "Add password reset");

  const rows = await treeRows(page);
  const epicRow = rows.find((r) => r.text.includes("Password and recovery"));
  const storyRow = rows.find((r) => r.text.includes("Add password reset"));

  expect.truthy(epicRow, "the epic is in the tree");
  expect.truthy(storyRow, "the story is in the tree");
  expect.truthy(storyRow.indent > epicRow.indent, "the story is indented under its epic");
  expect.contains(storyRow.text, story, "the story row names its key");
});

scenario("a child's page shows the chain above it", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Breadcrumb");

  const initiative = await createIssue(page, key, "Make signing in effortless", "Initiative");
  const epic = await addChild(page, initiative, "Password and recovery", "Epic");
  const story = await addChild(page, epic, "Add password reset by email", "Story");

  await goto(page, `/issues/${story}`);
  await page.waitForSelector('nav[aria-label="Parent issues"]', { timeout: WAIT });

  const trail = await page.$eval('nav[aria-label="Parent issues"]', (el) => el.innerText);
  expect.contains(trail, "Make signing in effortless", "the initiative is in the breadcrumb");
  expect.contains(trail, "Password and recovery", "the epic is in the breadcrumb");
});

scenario("an epic reports how much of its work is done", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Rollup");

  const epic = await createIssue(page, key, "Portal performance", "Epic");
  const first = await addChild(page, epic, "Cache the account page", "Story");
  await addChild(page, epic, "Compress the bundle", "Story");

  await goto(page, `/issues/${epic}`);
  await page.waitForFunction(() => document.body.innerText.includes("0/2 done"), { timeout: WAIT });

  // Close one of them, and the epic's roll-up has to follow.
  await goto(page, `/issues/${first}`);
  await clickButton(page, "Close");
  await goto(page, `/issues/${epic}`);
  await page.waitForFunction(() => document.body.innerText.includes("1/2 done"), { timeout: WAIT });
});

scenario("an issue can be moved to a different epic", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Moving");

  const first = await createIssue(page, key, "Password and recovery", "Epic");
  const second = await createIssue(page, key, "Portal performance", "Epic");
  const story = await addChild(page, first, "Add password reset by email", "Story");

  await goto(page, `/issues/${story}`);
  await selectByLabel(page, "#issue-parent", `${second} · Portal performance`);

  // The move is a change like any other, so the history has to show it.
  await page.waitForFunction(
    (s) => document.body.innerText.includes(s),
    { timeout: WAIT },
    `moved this under ${second}`,
  );

  await goto(page, `/issues/${first}`);
  expect.notContains(await bodyText(page), "Add password reset by email", "the old epic gave the story up");
});

scenario("the levels a parent may hold are the only ones offered", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Offered");

  const epic = await createIssue(page, key, "Password and recovery", "Epic");
  await goto(page, `/issues/${epic}`);
  await clickButton(page, "Add child");
  await page.waitForSelector("#child-type", { timeout: WAIT });

  const offered = await page.$eval("#child-type", (el) =>
    [...el.options].map((o) => o.textContent.trim()),
  );
  expect.notContains(offered.join(","), "Epic", "an epic is not offered under an epic");
  expect.notContains(offered.join(","), "Subtask", "a subtask is not offered under an epic");
  expect.contains(offered.join(","), "Story", "a story is offered under an epic");

  // And a subtask, at the bottom, is offered nothing at all.
  const story = await addChild(page, epic, "Add password reset", "Story");
  const subtask = await addChild(page, story, "Design the email", "Subtask");
  await goto(page, `/issues/${subtask}`);
  expect.notContains(await bodyText(page), "Add child", "nothing goes underneath a subtask");
});

scenario("a card on the board names the epic it belongs to", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Chips");

  const epic = await createIssue(page, key, "Password and recovery", "Epic");
  const story = await addChild(page, epic, "Add password reset by email", "Story");
  const subtask = await addChild(page, story, "Design the email", "Subtask");

  await goto(page, `/projects/${key}/board`);
  await page.waitForSelector(`[data-card="${story}"]`, { timeout: WAIT });

  const card = await page.$eval(`[data-card="${story}"]`, (el) => el.innerText);
  expect.contains(card, "Password and recovery", "the card names its epic");

  // A story inside an epic still belongs on the board; its subtasks do not.
  const board = await bodyText(page);
  expect.contains(board, story, "the story is on the board");
  expect.notContains(board, subtask, "the subtask is not on the board");
});
