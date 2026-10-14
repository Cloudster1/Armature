// The board and its swimlanes.

import { scenario, serialScenario } from "../runner.mjs";
import {
  bodyText,
  cardsIn,
  clickButton,
  coinStatus,
  confirm,
  createBoard,
  createIssue,
  createProject,
  dragCardTo,
  dragConnect,
  expect,
  fill,
  goto,
  issueRows,
  keepWorkflow,
  openIssueBeside,
  openWorkflowDesign,
  reload,
  replaceValue,
  selectByLabel,
  signUp,
  textOf,
  unique,
  uniqueName,
  WAIT,
  waitForButtonToGo,
  withViewport,
} from "../helpers.mjs";

scenario("every project gets a board with a swimlane per state", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Boarded", { visit: false });
  await createIssue(page, key, "a card to show", { visit: false });

  await goto(page, `/projects/${key}/board`);
  const board = await bodyText(page);
  for (const state of ["TO DO", "IN PROGRESS", "IN REVIEW", "DONE"]) {
    expect.contains(board, state, `the ${state} swimlane`);
  }
  expect.contains(board, "a card to show", "the card is on the board");
});

scenario("an issue opens beside the list and the list stays in reach", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Beside", { visit: false });
  await createIssue(page, key, "first of three", { type: "Task", visit: false });
  await createIssue(page, key, "second of three", { type: "Task", visit: false });
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

// Reported as one Escape closing the confirm and the issue under it: on a
// narrow screen the issue is a drawer, and each Escape is one step back.
scenario("Escape in the issue drawer closes only what is on top of it", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Stacked", { visit: false });
  const issueKey = await createIssue(page, key, "logged then kept", { visit: false });
  await goto(page, `/projects/${key}`);
  await openIssueBeside(page, issueKey);
  const drawer = `[data-issue-drawer="${issueKey}"]`;
  expect.truthy(await page.$(`${drawer}[aria-modal="true"]`), "a narrow screen gets the drawer");

  await page.waitForSelector(`${drawer} #worklog-duration`, { timeout: WAIT });
  await page.type(`${drawer} #worklog-duration`, "1h");
  await clickButton(page, "Log work");
  await page.waitForSelector(`${drawer} [data-worklog]`, { timeout: WAIT });

  // Escape in the confirm closes the confirm; the issue and its log stay.
  const remove = `${drawer} [data-worklog] button[aria-label^="Remove work log"]`;
  await page.focus(remove);
  await page.keyboard.press("Enter");
  await page.waitForSelector('[data-confirm="work log"]', { timeout: WAIT });
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => !document.querySelector('[data-confirm="work log"]'), { timeout: WAIT });
  expect.truthy(await page.$(drawer), "the issue stayed open under the confirm");
  expect.truthy(await page.$(`${drawer} [data-worklog]`), "and the work log is still there");
  expect.truthy(
    await page.evaluate(() => (document.activeElement?.getAttribute("aria-label") ?? "").startsWith("Remove work log")),
    "focus is back on Remove",
  );

  // Escape in the header's menu closes the menu, not the issue.
  await page.focus(`${drawer} [data-action="issue-more"]`);
  await page.keyboard.press("Enter");
  await page.waitForSelector('[role="menu"]', { timeout: WAIT });
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => !document.querySelector('[role="menu"]'), { timeout: WAIT });
  expect.truthy(await page.$(drawer), "the issue stayed open under the menu");
  expect.equal(await page.evaluate(() => document.activeElement?.getAttribute("data-action")), "issue-more", "focus is back on the menu's button");

  // Escape while a comment is being written keeps the drawer and the draft.
  await page.waitForSelector(`${drawer} #new-comment`, { timeout: WAIT });
  await page.click(`${drawer} #new-comment`);
  await page.type(`${drawer} #new-comment`, "half a thought");
  // A listener added after the app's sees the key once the app has handled it;
  // two frames on, whatever that Escape would have closed is gone.
  await page.evaluate(() => {
    window.escapeHandled = false;
    document.addEventListener("keydown", function seen(event) {
      if (event.key !== "Escape") return;
      document.removeEventListener("keydown", seen);
      requestAnimationFrame(() => requestAnimationFrame(() => (window.escapeHandled = true)));
    });
  });
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => window.escapeHandled, { timeout: WAIT });
  expect.truthy(await page.$(drawer), "the issue stayed open while a comment was written");
  const draft = await page.$eval(`${drawer} #new-comment`, (el) => (el.tagName === "TEXTAREA" ? el.value : el.textContent));
  expect.contains(draft, "half a thought", "and the draft is kept");

  // From the drawer itself, Escape closes it.
  await page.click(`${drawer} h2`);
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => !document.querySelector("[data-issue-panel]"), { timeout: WAIT });
});

scenario("a board narrows to my cards, says what it hides, and keeps the filter in its address", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  // The scenario's own project stands in for the seeded one, whose cards nobody holds.
  const key = await createProject(page, "Narrowed", { visit: false });
  const toDo = await createIssue(page, key, "mine to do", { assignee: "me", visit: false });
  const asWell = await createIssue(page, key, "mine as well", { assignee: "me", visit: false });
  await createIssue(page, key, "nobody holds this", { visit: false });
  const mine = [toDo, asWell].sort().join(" ");
  await createBoard(page, key, "Second look", "kanban");
  const shown = () => page.evaluate(() => [...document.querySelectorAll("[data-card]")].map((el) => el.getAttribute("data-card")).sort().join(" "));
  const showing = (count) => page.waitForFunction((n) => document.querySelectorAll("[data-card]").length === n, { timeout: WAIT }, count);

  await goto(page, `/projects/${key}/board`);
  await showing(3);
  await clickButton(page, "Only mine");
  await showing(2);
  expect.equal(await shown(), mine, "only my two cards are on the board");
  expect.equal(await textOf(page, "[data-hidden-cards]"), "1 card hidden by the filters", "the board says one is hidden");
  expect.equal(
    await page.$eval('[data-swimlane="To Do"] [data-lane-count]', (el) => el.getAttribute("data-lane-count")),
    "2 of 3",
    "the lane still counts the card it hides",
  );

  // A reload is the link opened again.
  expect.contains(page.url(), "mine=true", "the filter is in the address");
  await reload(page);
  await showing(2);
  expect.equal(await page.$eval('[data-quick-filter="mine"]', (el) => el.getAttribute("aria-pressed")), "true", "Only mine is still pressed");

  // The filter goes with the board picked, and the address names both.
  await showBoard(page, "Second look");
  await showing(2);
  await page.type('input[aria-label="Filter cards by key or summary"]', "AS WELL");
  await showing(1);
  await reload(page);
  await page.waitForFunction(() => document.querySelector("[data-board-showing]")?.getAttribute("data-board-showing") === "Second look", { timeout: WAIT });
  await showing(1);
  expect.equal(await shown(), asWell, "the text matched the summary, ignoring case");

  await clickButton(page, "Clear filters");
  await showing(3);
  expect.equal(await page.$("[data-hidden-cards]"), null, "nothing is hidden once cleared");
});

scenario("an issue opens beside the board and the arrow keys walk the cards", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Carded", { visit: false });
  const first = await createIssue(page, key, "top card", { visit: false });
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

scenario("an edit made beside the board shows on its card without a reload", async ({ page }) => {
  const who = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Edited", { visit: false });
  const issueKey = await createIssue(page, key, "edit me in the panel");
  const card = `[data-card="${issueKey}"]`;

  await withViewport(page, 1600, async () => {
    await goto(page, `/projects/${key}/board`);
    await page.waitForSelector(card, { timeout: WAIT });
    expect.truthy(await page.$(`${card} [title="Unassigned"]`), "the card starts with nobody on it");

    await page.click(`[data-open-drawer="${issueKey}"]`);
    await page.waitForSelector(`[data-issue-panel="${issueKey}"]`, { timeout: WAIT });

    // The assignee and a label, both changed in the panel, reach the card it sits beside.
    await selectByLabel(page, '[data-issue-panel] select[aria-label="Assignee"]', who.name);
    await page.waitForSelector(`${card} [title="${who.name}"]`, { timeout: WAIT });

    await page.type('[data-issue-panel] input[aria-label="Add a label"]', "backend");
    await page.keyboard.press("Enter");
    await page.waitForSelector(`${card} [data-label="backend"]`, { timeout: WAIT });

    expect.truthy(await page.$(`[data-issue-panel="${issueKey}"]`), "the panel stayed open while the card caught up");
  });
});

scenario("dragging a card between swimlanes takes the workflow transition", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Dragging", { visit: false });
  const issueKey = await createIssue(page, key, "drag me along", { visit: false });

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
  // The card moves at once; the report follows the server's answer.
  await page.waitForFunction(() => document.body.innerText.includes("Start progress"), { timeout: WAIT });

  // And the issue itself really moved, post-function and all.
  await goto(page, `/issues/${issueKey}`);
  const detail = await bodyText(page);
  expect.contains(detail, "In Progress", "the issue is in the new status");
  expect.contains(detail, "moved this from To Do to In Progress", "the changelog recorded it");
});

scenario("a drag the workflow forbids is refused and the card stays put", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Forbidden", { visit: false });
  const issueKey = await createIssue(page, key, "no shortcuts here", { visit: false });

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Merging", { visit: false });
  const issueKey = await createIssue(page, key, "lands in the merged lane", { visit: false });

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
  // The In Progress lane can take In Review once the board, refetched after
  // the delete, no longer has it claimed.
  const reviewBox = await page.waitForFunction(
    () => {
      if (document.querySelector('[data-swimlane-editor="In Review"]')) return null;
      const box = [...(document.querySelector('[data-swimlane-editor="In Progress"]')?.querySelectorAll("label") ?? [])]
        .find((l) => l.textContent.trim() === "In Review")
        ?.querySelector("input");
      return box && !box.disabled ? box : null;
    },
    { timeout: WAIT },
  );

  // The box is checked only from the saved board, never ahead of it.
  await reviewBox.evaluate((box) => box.click());
  await page.waitForFunction(
    () =>
      [...(document.querySelector('[data-swimlane-editor="In Progress"]')?.querySelectorAll("label") ?? [])]
        .find((l) => l.textContent.trim() === "In Review")
        ?.querySelector("input")?.checked === true,
    { timeout: WAIT },
  );

  await clickButton(page, "Done configuring");
  await page.waitForFunction(
    () => !document.querySelector("[data-swimlane-editor]") && document.body.innerText.includes("In Progress · In Review"),
    { timeout: WAIT },
  );

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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Orphans", { visit: false });
  const issueKey = await createIssue(page, key, "about to be orphaned", { visit: false });

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

/** The board's lanes, left to right, as the page draws them. */
function laneOrder(page) {
  return page.$$eval("[data-swimlane]", (els) => els.map((el) => el.getAttribute("data-swimlane")).join(", "));
}

// Serial: the organization's own workflow is every project's.
serialScenario("a lane for a status added to the workflow is moved off the end and stays there", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "On hold", { visit: false });
  // Statuses are everybody's, so this one has a name nobody else took.
  const hold = uniqueName("On Hold");
  const putBack = await keepWorkflow("Default software workflow");

  try {
    // The workflow from the report: On Hold, in and out of In Progress.
    await openWorkflowDesign(page, "Default software workflow");
    await coinStatus(page, { name: hold, category: "In progress" });
    await dragConnect(page, "In Progress", hold);
    await replaceValue(page, "#field-transition-name", "Hold");
    await dragConnect(page, hold, "In Progress");
    await replaceValue(page, "#field-transition-name", "Resume");
    await clickButton(page, "Save workflow");
    await waitForButtonToGo(page, "Save workflow");

    await goto(page, `/projects/${key}/board`);
    await page.waitForSelector(`[data-swimlane="${hold}"]`, { timeout: WAIT });
    expect.equal(await laneOrder(page), `To Do, In Progress, In Review, Done, ${hold}`, "the new lane starts last");

    // Twice to the left, each press once the one before has landed.
    await clickButton(page, "Configure swimlanes");
    const left = `button[aria-label="Move ${hold} left"]:not([disabled])`;
    for (const want of [`To Do, In Progress, In Review, ${hold}, Done`, `To Do, In Progress, ${hold}, In Review, Done`]) {
      await page.waitForSelector(left, { timeout: WAIT });
      await page.click(left);
      await page.waitForFunction(
        (order) => [...document.querySelectorAll("[data-swimlane]")].map((el) => el.getAttribute("data-swimlane")).join(", ") === order,
        { timeout: WAIT },
        want,
      );
    }
    await clickButton(page, "Done configuring");

    await reload(page);
    await page.waitForSelector(`[data-swimlane="${hold}"]`, { timeout: WAIT });
    expect.equal(await laneOrder(page), `To Do, In Progress, ${hold}, In Review, Done`, "the board kept the order after a reload");
  } finally {
    await putBack();
  }
});

/** Opens one of the project's boards by name and waits until it is the one shown. */
async function showBoard(page, name) {
  await page.waitForSelector(`[data-board="${name}"]`, { timeout: WAIT });
  await page.click(`[data-board="${name}"]`);
  await page.waitForFunction(
    (n) => document.querySelector("[data-board-showing]")?.getAttribute("data-board-showing") === n,
    { timeout: WAIT },
    name,
  );
}

/** The first lane's name and limit, and the grouping, the panel shows for the board on screen. */
async function editorState(page) {
  await clickButton(page, "Configure swimlanes");
  await page.waitForSelector("[data-swimlane-editor]", { timeout: WAIT });
  const state = await page.evaluate(() => ({
    first: document.querySelector("#field-swimlane-1-name")?.value,
    limit: document.querySelector("[data-swimlane-editor] #field-card-limit")?.value,
    grouped: [...document.querySelectorAll("button[aria-pressed=true]")].map((b) => b.textContent.trim()),
  }));
  await clickButton(page, "Done configuring");
  return state;
}

// A limit only the team board is given, to look for on the other.
const TEAM_LIMIT = "3";

scenario("a team board's swimlanes are configured and the first board is left alone", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Lanes", { visit: false });
  await createBoard(page, key, "Team flow", "kanban");

  await showBoard(page, "Team flow");
  await clickButton(page, "Configure swimlanes");
  await page.waitForSelector('[data-swimlane-editor="To Do"]', { timeout: WAIT });
  await fill(page, "Swimlane 1 name", "Queued");
  await page.click('[data-swimlane-editor="To Do"] #field-card-limit', { clickCount: 3 });
  await page.keyboard.sendCharacter(TEAM_LIMIT);
  await page.evaluate(() => document.querySelector('[data-swimlane-editor="To Do"] button[type=submit]').click());
  await page.waitForSelector('[data-swimlane="Queued"]', { timeout: WAIT });
  await clickButton(page, "Assignee");
  await page.waitForFunction(
    () => [...document.querySelectorAll("button[aria-pressed=true]")].some((b) => b.textContent.trim() === "Assignee"),
    { timeout: WAIT },
  );
  await clickButton(page, "Done configuring");

  await showBoard(page, "Lanes board");
  await page.waitForSelector('[data-swimlane="To Do"]', { timeout: WAIT });
  expect.equal(await page.$('[data-swimlane="Queued"]'), null, "the first board kept its own first lane");
  const first = await editorState(page);
  expect.equal(first.first, "To Do", "the panel beside the first board shows the first board's lanes");
  expect.equal(first.limit, "", "and none of the team board's limit");
  expect.truthy(first.grouped.includes("No rows"), "nor its grouping");

  await showBoard(page, "Team flow");
  const team = await editorState(page);
  expect.equal(team.first, "Queued", "the team board kept what was set on it");
  expect.equal(team.limit, TEAM_LIMIT, "including its limit");
  expect.truthy(team.grouped.includes("Assignee"), "and its grouping");
});

scenario("a board made by mistake is renamed, retyped and deleted", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Mistakes", { visit: false });
  await createBoard(page, key, "Oops", "scrum");
  await showBoard(page, "Oops");

  await clickButton(page, "Rename");
  await fill(page, "Board name", "Release flow");
  await clickButton(page, "Save name");
  await showBoard(page, "Release flow");

  await clickButton(page, "Change type");
  await page.waitForSelector('h1 [data-board-type="kanban"]', { timeout: WAIT });

  await clickButton(page, "Delete");
  await page.waitForSelector('[data-confirm="board Release flow"]', { timeout: WAIT });
  await confirm(page);
  await page.waitForFunction(() => !document.querySelector('[data-board="Release flow"]'), { timeout: WAIT });
  await page.waitForFunction(
    () => document.querySelector("[data-board-showing]")?.getAttribute("data-board-showing") === "Mistakes board",
    { timeout: WAIT },
  );
});
