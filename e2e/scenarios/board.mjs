// The board and its swimlanes.

import { scenario } from "../runner.mjs";
import {
  bodyText,
  cardsIn,
  clickButton,
  confirm,
  createIssue,
  createProject,
  dragCardTo,
  expect,
  goto,
  issueRows,
  openIssueBeside,
  signUp,
  textOf,
  WAIT,
  withViewport,
} from "../helpers.mjs";

scenario("every project gets a board with a swimlane per state", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Boarded");
  await createIssue(page, key, "a card to show");

  await goto(page, `/projects/${key}/board`);
  const board = await bodyText(page);
  for (const state of ["TO DO", "IN PROGRESS", "IN REVIEW", "DONE"]) {
    expect.contains(board, state, `the ${state} swimlane`);
  }
  expect.contains(board, "a card to show", "the card is on the board");
});

scenario("an issue opens beside the list and the list stays in reach", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Beside");
  await createIssue(page, key, "first of three", "Task");
  await createIssue(page, key, "second of three", "Task");
  await createIssue(page, key, "third of three", "Bug");

  // Wide enough to dock: the list narrows and stays clickable beside the issue.
  await withViewport(page, 1600, async () => {
    await goto(page, `/projects/${key}`);
    const rows = await issueRows(page);
    expect.equal(rows.length, 3, "three rows to walk");

    await openIssueBeside(page, rows[0]);
    expect.equal(await page.$('[data-issue-drawer]'), null, "docked, not a drawer over the page");
    const widths = await page.evaluate(() => ({
      main: document.querySelector("main").getBoundingClientRect().width,
      panel: document.querySelector("[data-issue-panel]").getBoundingClientRect().width,
    }));
    expect.truthy(widths.main + widths.panel <= 1600 && widths.panel >= 400, "the panel took a column and the page kept the rest");
    expect.truthy((await issueRows(page)).length === 3, "the rows are still on the page");
    expect.equal(await page.$eval(`[data-issue-row="${rows[0]}"]`, (el) => el.hasAttribute("data-selected")), true, "the open row is marked");

    // Down walks the list; the panel's own button does the same.
    await page.keyboard.press("ArrowDown");
    await page.waitForSelector(`[data-issue-panel="${rows[1]}"]`, { timeout: WAIT });
    await page.click('[data-action="panel-next"]');
    await page.waitForSelector(`[data-issue-panel="${rows[2]}"]`, { timeout: WAIT });
    expect.equal(await page.$eval('[data-action="panel-next"]', (el) => el.disabled), true, "nothing after the last row");

    // The list is still a list: it can be filtered with the panel open.
    await page.type('input[aria-label="Search issues"]', "third");
    await page.waitForFunction(() => document.querySelectorAll("[data-issue-row]").length === 1, { timeout: WAIT });
    expect.truthy(await page.$(`[data-issue-panel="${rows[2]}"]`), "filtering left the panel where it was");

    // Escape from the page closes it; from the search field it would not.
    await page.click("[data-issue-panel] h2");
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => !document.querySelector("[data-issue-panel]"), { timeout: WAIT });
  });

  // Too narrow to dock: the same click opens the drawer over the page.
  await goto(page, `/projects/${key}`);
  const rows = await issueRows(page);
  await openIssueBeside(page, rows[0]);
  expect.truthy(await page.$('[data-issue-drawer][aria-modal="true"]'), "a narrow screen gets the drawer");
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => !document.querySelector("[data-issue-panel]"), { timeout: WAIT });
});

scenario("an issue opens beside the board and the arrow keys walk the cards", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Carded");
  const first = await createIssue(page, key, "top card");
  const second = await createIssue(page, key, "card below");

  await withViewport(page, 1600, async () => {
    await goto(page, `/projects/${key}/board`);
    const cards = await cardsIn(page, "To Do");
    expect.equal(cards.length, 2, "two cards in To Do");

    // A click on the card, away from its links, opens it beside the board.
    await page.evaluate((k) => document.querySelector(`[data-card="${k}"]`).click(), cards[0]);
    await page.waitForSelector(`[data-issue-panel="${cards[0]}"]`, { timeout: WAIT });
    await page.keyboard.press("ArrowDown");
    await page.waitForSelector(`[data-issue-panel="${cards[1]}"]`, { timeout: WAIT });

    // Dragging still works with the panel open, and the panel stays.
    await dragCardTo(page, first, "In Progress");
    await page.waitForFunction(
      (k) => document.querySelector('[data-swimlane="In Progress"]')?.querySelector(`[data-card="${k}"]`) !== null,
      { timeout: WAIT },
      first,
    );
    expect.truthy(await page.$(`[data-issue-panel="${cards[1]}"]`), "the panel outlived the drag");
    expect.truthy([first, second].includes(cards[1]), "the walked card is one of ours");
  });
});

scenario("dragging a card between swimlanes takes the workflow transition", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Dragging");
  const issueKey = await createIssue(page, key, "drag me along");

  await goto(page, `/projects/${key}/board`);
  expect.equal((await cardsIn(page, "To Do")).join(","), issueKey, "starts in To Do");

  await dragCardTo(page, issueKey, "In Progress");
  await page.waitForFunction(
    (k) => {
      const lane = document.querySelector('[data-swimlane="In Progress"]');
      return lane?.querySelector(`[data-card="${k}"]`) !== null;
    },
    { timeout: WAIT },
    issueKey,
  );

  expect.equal((await cardsIn(page, "To Do")).join(","), "", "left the old swimlane");
  expect.contains(await bodyText(page), "Start progress", "the transition it took is reported");

  // And the issue itself really moved, post-function and all.
  await goto(page, `/issues/${issueKey}`);
  const detail = await bodyText(page);
  expect.contains(detail, "In Progress", "the issue is in the new status");
  expect.contains(detail, "moved this from To Do to In Progress", "the changelog recorded it");
});

scenario("a drag the workflow forbids is refused and the card stays put", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Forbidden");
  const issueKey = await createIssue(page, key, "no shortcuts here");

  await goto(page, `/projects/${key}/board`);
  // To Do to In Review has no transition behind it.
  await dragCardTo(page, issueKey, "In Review");

  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains(
    await textOf(page, "[role=alert]"),
    "does not allow that move",
    "the refusal explains itself",
  );
  expect.equal((await cardsIn(page, "To Do")).join(","), issueKey, "the card stayed where it was");
});

scenario("swimlanes can be reconfigured to cover several states", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Merging");
  const issueKey = await createIssue(page, key, "lands in the merged lane");

  await goto(page, `/projects/${key}/board`);
  await clickButton(page, "Configure swimlanes");
  await page.waitForFunction(() => document.body.innerText.includes("States in this swimlane"), {
    timeout: WAIT,
  });

  // Delete In Review, then add its state to the In Progress lane.
  await page.evaluate(() => {
    const card = [...document.querySelectorAll("input")]
      .find((i) => i.value === "In Review")
      ?.closest("[data-swimlane-editor]");
    [...(card?.querySelectorAll("button") ?? [])]
      .find((b) => b.textContent.trim() === "Delete")
      ?.click();
  });
  await confirm(page);
  await page.waitForFunction(
    () => !document.body.innerText.includes("states in this swimlane".toUpperCase()) || true,
    { timeout: 5_000 },
  );
  await new Promise((r) => setTimeout(r, 1200));

  await page.evaluate(() => {
    const card = [...document.querySelectorAll("input")]
      .find((i) => i.value === "In Progress")
      ?.closest("[data-swimlane-editor]");
    const label = [...(card?.querySelectorAll("label") ?? [])].find(
      (l) => l.textContent.trim() === "In Review",
    );
    label?.querySelector("input")?.click();
  });
  await new Promise((r) => setTimeout(r, 1500));

  await clickButton(page, "Done configuring");
  await new Promise((r) => setTimeout(r, 800));

  const board = await bodyText(page);
  expect.notContains(board, "IN REVIEW", "the deleted swimlane is gone");
  expect.contains(board, "In Progress · In Review", "the remaining lane covers both states");

  // A card moved into In Review now appears in the merged lane.
  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });
  await clickButton(page, "Ready for review");
  await page.waitForFunction(() => document.body.innerText.includes("In Review"), { timeout: WAIT });

  await goto(page, `/projects/${key}/board`);
  expect.contains((await cardsIn(page, "In Progress")).join(","), issueKey, "the card is in the merged lane");
});

scenario("cards in states no swimlane covers are surfaced, not hidden", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Orphans");
  const issueKey = await createIssue(page, key, "about to be orphaned");

  await goto(page, `/issues/${issueKey}`);
  await clickButton(page, "Start progress");
  await page.waitForFunction(() => document.body.innerText.includes("In Progress"), { timeout: WAIT });

  await goto(page, `/projects/${key}/board`);
  await clickButton(page, "Configure swimlanes");
  await page.waitForFunction(() => document.body.innerText.includes("States in this swimlane"), {
    timeout: WAIT,
  });
  await page.evaluate(() => {
    const card = [...document.querySelectorAll("input")]
      .find((i) => i.value === "In Progress")
      ?.closest("[data-swimlane-editor]");
    [...(card?.querySelectorAll("button") ?? [])]
      .find((b) => b.textContent.trim() === "Delete")
      ?.click();
  });
  await confirm(page);

  await page.waitForFunction(
    () => document.body.innerText.includes("states no swimlane covers"),
    { timeout: WAIT },
  );
  expect.contains(await bodyText(page), issueKey, "the orphaned card is named");
});
