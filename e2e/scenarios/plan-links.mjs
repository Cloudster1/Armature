// Moving and linking work on the plan, the graph, and milestones.

import { scenario } from "../runner.mjs";
import {
  assignMilestone,
  clickButton,
  commitIssue,
  createIssue,
  createMilestone,
  createProject,
  createSprint,
  day,
  dragGraphLink,
  dragLink,
  dragRow,
  expect,
  goto,
  milestoneProgress,
  openPlanFilters,
  planFigure,
  planRows,
  planSprint,
  reload,
  runQuery,
  scheduleIssue,
  selectByLabel,
  setClosedForDays,
  signUp,
  textOf,
  WAIT,
} from "../helpers.mjs";

scenario("a ticket is moved under another on the plan", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Moving");
  const epic = await createIssue(page, key, "The epic", "Epic");
  const story = await createIssue(page, key, "A loose story", "Story");

  await goto(page, `/projects/${key}/plan`);
  const rows = await planRows(page);
  expect.equal(rows.slice(0, 2).join(","), `${epic},${story}`, "two roots to start with");

  // Dropping the story on the epic makes it the epic's child, one level in.
  await dragRow(page, story, epic);
  await page.waitForFunction(
    (k) => document.querySelector(`[data-plan-row="${k}"]`)?.dataset.planDepth === "1",
    { timeout: WAIT },
    story,
  );
  await goto(page, `/issues/${story}`);
  await page.waitForFunction((k) => document.body.innerText.includes(k), { timeout: WAIT }, epic);

  // Dropping it above the rows makes it a root again.
  await goto(page, `/projects/${key}/plan`);
  await dragRow(page, story, "top");
  await page.waitForFunction(
    (k) => document.querySelector(`[data-plan-row="${k}"]`)?.dataset.planDepth === "0",
    { timeout: WAIT },
    story,
  );

  // A story cannot hold an epic: the drop is refused with the reason, and nothing moves.
  await dragRow(page, epic, story);
  await page.waitForFunction(() => document.body.innerText.includes("cannot be the parent of"), { timeout: WAIT });
  expect.equal(await page.$eval(`[data-plan-row="${epic}"]`, (el) => el.dataset.planDepth), "0", "the epic stayed a root");
});

scenario("a dependency is drawn on the plan by dragging from one bar to another", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Depending");
  const first = await createIssue(page, key, "Build the token store", "Story");
  const second = await createIssue(page, key, "Send the reset email", "Story");
  await scheduleIssue(page, first, "2026-03-02", "2026-03-06");
  await scheduleIssue(page, second, "2026-03-09", "2026-03-13");

  // Dragging the handle at the end of one bar onto another row draws the arrow.
  await goto(page, `/projects/${key}/plan`);
  await dragLink(page, first, second);
  await page.waitForSelector(`[data-plan-arrow="${first}->${second}"]`, { timeout: WAIT });
  expect.equal(await page.$$eval("[data-plan-arrow]", (els) => els.length), 1, "one dependency is drawn");

  // The other way round would make the first wait on itself, and is refused with the reason.
  await dragLink(page, second, first);
  await page.waitForFunction(() => document.body.innerText.includes("already blocks"), { timeout: WAIT });
  expect.equal(await page.$$eval("[data-plan-arrow]", (els) => els.length), 1, "still one dependency");

  // The ticket shows the link too.
  await goto(page, `/issues/${first}`);
  await page.waitForSelector(`[data-issue-link="${second}"]`, { timeout: WAIT });
  expect.contains(await page.$eval(`[data-issue-link="${second}"]`, (el) => el.innerText), "blocks", "the ticket says what it blocks");

  // Selecting the arrow on the plan offers to remove it, and removing it takes the arrow away.
  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector(`[data-plan-arrow="${first}->${second}"]`, { timeout: WAIT });
  await page.$eval(`[data-plan-arrow-hit="${first}->${second}"]`, (el) => el.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  // The control sits on the arrow itself, inside the calendar pane.
  await page.waitForSelector('[data-testid="plan-calendar"] [data-plan-unlink]', { timeout: WAIT });
  await clickButton(page, `Remove dependency ${first} blocks ${second}`);
  await page.waitForFunction(() => document.querySelectorAll("[data-plan-arrow]").length === 0, { timeout: WAIT });
});

scenario("the dependency graph shows every ticket and grows an edge by dragging", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Graphing");
  const first = await createIssue(page, key, "Build the token store", "Story");
  const second = await createIssue(page, key, "Send the reset email", "Story");
  const third = await createIssue(page, key, "Write the release notes", "Bug");
  await page.evaluate(
    async (from, to) => {
      const response = await fetch(`/api/v1/issues/${from}/links`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type: "Blocks", targetKey: to }),
      });
      if (!response.ok) throw new Error(`link failed: ${response.status}`);
    },
    first,
    second,
  );

  // Every ticket is drawn: the linked pair as an edge, the third under the caption.
  await goto(page, `/projects/${key}/plan?view=dependencies`);
  await page.waitForSelector(`[data-graph-edge="${first}->${second}"]`, { timeout: WAIT });
  expect.equal(await page.$$eval("[data-graph-node]", (els) => els.length), 3, "three tickets on the graph");
  const unlinkedTop = await page.$eval("[data-graph-unlinked]", (el) => el.getBoundingClientRect().top);
  const thirdTop = await page.$eval(`[data-graph-node="${third}"]`, (el) => el.getBoundingClientRect().top);
  const firstTop = await page.$eval(`[data-graph-node="${first}"]`, (el) => el.getBoundingClientRect().top);
  expect.truthy(thirdTop > unlinkedTop && firstTop < unlinkedTop, "the unlinked ticket sits under the caption, the linked ones above it");

  // Dragging the third's dot onto the first makes the third block the first.
  await dragGraphLink(page, third, first);
  await page.waitForSelector(`[data-graph-edge="${third}->${first}"]`, { timeout: WAIT });
  expect.equal(await page.$$eval("[data-graph-edge]", (els) => els.length), 2, "two dependencies");
  await page.waitForFunction(() => !document.querySelector("[data-graph-unlinked]"), { timeout: WAIT });

  // Selecting an edge offers to remove it, and the timeline agrees on what is left.
  await page.$eval(`[data-graph-edge-hit="${third}->${first}"]`, (el) => el.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  await page.waitForSelector('[data-testid="graph-canvas"] [data-graph-unlink]', { timeout: WAIT });
  await clickButton(page, `Remove dependency ${third} blocks ${first}`);
  await page.waitForFunction(() => document.querySelectorAll("[data-graph-edge]").length === 1, { timeout: WAIT });

  // The query narrows the graph to what it matched.
  await runQuery(page, "type = Bug");
  await page.waitForFunction(() => document.querySelectorAll("[data-graph-node]").length === 1, { timeout: WAIT });
  expect.equal(await page.$eval("[data-graph-node]", (el) => el.dataset.graphNode), third, "only the bug is left");
});

scenario("tickets closed long ago leave the plan after the days the reader set", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Tidy");
  const open = await createIssue(page, key, "Still open", "Story");
  const finished = await createIssue(page, key, "Finished today", "Story");
  await goto(page, `/issues/${finished}`);
  await clickButton(page, "Close");
  await page.waitForFunction(() => document.body.innerText.includes("Done"), { timeout: WAIT });

  // Closed today is well within the default fortnight, so it is still drawn.
  await goto(page, `/projects/${key}/plan`);
  let rows = await planRows(page);
  expect.truthy(rows.includes(finished) && rows.includes(open), "both tickets are on the plan at the default");
  await openPlanFilters(page);
  expect.equal(await page.$eval("[data-plan-closed-for]", (el) => el.value), "14", "the default is a fortnight");

  // Zero days means done work is not drawn at all; open work is untouched.
  await setClosedForDays(page, 0);
  await page.waitForFunction((k) => !document.querySelector(`[data-plan-row="${k}"]`), { timeout: WAIT }, finished);
  rows = await planRows(page);
  expect.truthy(!rows.includes(finished) && rows.includes(open), "the finished ticket is cut and the open one stays");

  // The choice is the reader's, so it is still there after a reload.
  await reload(page);
  rows = await planRows(page);
  expect.truthy(!rows.includes(finished), "the cut survives a reload");
  await openPlanFilters(page);
  expect.equal(await page.$eval("[data-plan-closed-for]", (el) => el.value), "0", "and so does the number");

  // Blank means everything, and the ticket is back.
  await setClosedForDays(page, "");
  await page.waitForSelector(`[data-plan-row="${finished}"]`, { timeout: WAIT });
});

scenario("the plan is read two ways: by hierarchy with the numbers, and by sprint", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Two readings");

  const story = await createIssue(page, key, "a story to plan");
  const bug = await createIssue(page, key, "a bug to squash", "Bug");
  const task = await createIssue(page, key, "a task on the side");
  await scheduleIssue(page, story, day(1), day(8));
  const sprint = await createSprint(page, key, "Sprint A");
  await planSprint(page, sprint, { from: day(0), to: day(13), capacity: 10 });
  await commitIssue(page, story, "Sprint A");
  await createMilestone(page, key, "M1", day(30));
  await assignMilestone(page, bug, "M1");

  // The management view counts everything and can be narrowed by type. It
  // says nothing about sprints; that is the other view.
  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector('[data-testid="plan-meter"]', { timeout: WAIT });
  expect.equal(await page.$('[data-testid="plan-sprint-bands"]'), null, "no sprint band on the management view");
  expect.equal(await page.$('[data-testid="plan-capacity"]'), null, "no capacity list on the management view");
  expect.equal(await planFigure(page, "issues"), "3", "three issues are counted");
  expect.equal(await planFigure(page, "scheduled"), "1", "one of them is on the calendar");
  expect.equal((await planRows(page)).length, 3, "every issue has a row");

  await openPlanFilters(page);
  await page.click('[data-plan-type-filter="Bug"]');
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="plan-sidebar"] [data-plan-row]').length === 1, { timeout: WAIT });
  expect.equal((await planRows(page))[0], bug, "only the bug is left");
  expect.equal(await planFigure(page, "issues"), "3", "the numbers are still about the whole plan");

  await openPlanFilters(page);
  await page.click('[data-plan-type-filter="Bug"]');
  await openPlanFilters(page);
  await selectByLabel(page, 'select[aria-label="Filter by milestone"]', "M1");
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="plan-sidebar"] [data-plan-row]').length === 1, { timeout: WAIT });
  expect.equal((await planRows(page))[0], bug, "the milestone narrows to what counts towards it");

  // The sprint view groups the same work by iteration, backlog last.
  await clickButton(page, "Sprints");
  await page.waitForFunction(() => location.search.includes("view=sprints"), { timeout: WAIT });
  await page.waitForSelector('[data-plan-group="Backlog"]', { timeout: WAIT });
  const rows = await planRows(page);
  expect.equal(rows.join(" "), `[Sprint A] ${story} [Backlog] ${bug} ${task}`, "each sprint, then what is committed to it, then the backlog");
  expect.contains(await textOf(page, '[data-plan-group="Sprint A"]'), "1 issue", "the group says what it holds");
  await page.waitForSelector('[data-plan-group-band="Sprint A"]', { timeout: WAIT });
  await page.waitForSelector('[data-testid="plan-sprint-bands"]', { timeout: WAIT });
  await page.waitForSelector('[data-testid="plan-capacity"]', { timeout: WAIT });

  // Folding a sprint away hides its work and nothing else.
  await page.click('[data-plan-group="Sprint A"] button');
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="plan-sidebar"] [data-plan-row]').length === 2, { timeout: WAIT });
  expect.equal((await planRows(page)).join(" "), `[Sprint A] [Backlog] ${bug} ${task}`, "the sprint is folded");
});

scenario("a milestone tracks the issues assigned to it and stands on the plan", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Reaching");
  const first = await createIssue(page, key, "half of the release");
  const second = await createIssue(page, key, "the other half");

  const due = new Date(Date.now() + 10 * 86_400_000).toISOString().slice(0, 10);
  await createMilestone(page, key, "Release 1", due);
  expect.contains(await milestoneProgress(page, key, "Release 1"), "Nothing assigned yet", "an empty milestone says so");

  await assignMilestone(page, first, "Release 1");
  await assignMilestone(page, second, "Release 1");
  expect.contains(await milestoneProgress(page, key, "Release 1"), "0 of 2 done, 0%", "two assigned, nothing done");

  // Finishing an issue moves the milestone; nobody edits the milestone itself.
  await goto(page, `/issues/${first}`);
  await clickButton(page, "Close");
  await page.waitForFunction(() => document.body.innerText.includes("Done"), { timeout: WAIT });
  expect.contains(await milestoneProgress(page, key, "Release 1"), "1 of 2 done, 50%", "half way there");

  // The plan flags the day it is due, with the same number.
  await goto(page, `/projects/${key}/plan`);
  await page.waitForSelector('[data-milestone-flag="Release 1"]', { timeout: WAIT });
  expect.contains(await textOf(page, '[data-milestone-flag="Release 1"]'), "50%", "the flag carries the progress");
  expect.contains(await textOf(page, '[data-plan-milestone="Release 1"]'), "1 of 2 done", "and so does the list below");

  // A closed milestone takes no more work, and the issue page says why.
  await goto(page, `/projects/${key}/milestones`);
  await clickButton(page, "Close milestone");
  await page.waitForFunction(() => document.body.innerText.includes("Closed"), { timeout: WAIT });
  const third = await createIssue(page, key, "too late for the release");
  await goto(page, `/issues/${third}`);
  await page.waitForSelector("#issue-milestone", { timeout: WAIT });
  const offered = await page.$$eval("#issue-milestone option", (os) => os.map((o) => o.textContent.trim()));
  expect.notContains(offered.join("|"), "Release 1", "a closed milestone is not offered");
});
