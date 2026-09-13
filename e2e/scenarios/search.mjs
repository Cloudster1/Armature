// Search: suggestions and the query language.

import { scenario } from "../runner.mjs";
import {
  clickButton,
  createIssue,
  createProject,
  expect,
  goto,
  pickSuggestion,
  issueRows,
  planFigure,
  planRows,
  runQuery,
  signUp,
  WAIT,
  waitForPath,
} from "../helpers.mjs";

scenario("the search bar suggests what to type and what it finds", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Suggesting");
  const issueKey = await createIssue(page, key, "The printer is on fire", "Bug");

  // A field, its operator, then a value, each picked from the list under the bar.
  await goto(page, "/search");
  await page.waitForSelector("[data-query]", { timeout: WAIT });
  await page.click("[data-query]");
  await page.type("[data-query]", "sta");
  await pickSuggestion(page, "status");
  await page.waitForFunction(() => document.querySelector("[data-query]")?.value === "status ", { timeout: WAIT });
  await pickSuggestion(page, "=");
  await page.type("[data-query]", "to");
  await pickSuggestion(page, '"To Do"');
  expect.equal(await page.$eval("[data-query]", (el) => el.value), 'status = "To Do" ', "the bar reads what was picked");
  await page.keyboard.press("Enter");
  await page.waitForFunction((k) => [...document.querySelectorAll("[data-issue-row]")].some((r) => r.dataset.issueRow === k), { timeout: WAIT }, issueKey);

  // Words find issues, and Enter on one opens it.
  await page.click("[data-query]", { clickCount: 3 });
  await page.keyboard.press("Backspace");
  await page.type("[data-query]", "printer");
  await page.waitForSelector(`[data-query-suggestions] [data-suggestion="issue"][data-suggestion-text="${issueKey}"]`, { timeout: WAIT });
  await pickSuggestion(page, issueKey);
  await waitForPath(page, `/issues/${issueKey}`);
});

scenario("a query finds the tickets it describes and says where it went wrong", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Querying");
  const firstBug = await createIssue(page, key, "Login fails with a valid password", "Bug");
  const secondBug = await createIssue(page, key, "Crash on save", "Bug");
  const story = await createIssue(page, key, "Write the docs", "Story");

  // The search page lists what the query says, in the order it asks for.
  await goto(page, `/search?q=${encodeURIComponent(`project = ${key} AND type = Bug ORDER BY key DESC`)}`);
  let rows = await issueRows(page);
  expect.equal(rows.join(","), `${secondBug},${firstBug}`, "the bugs, newest key first, and not the story");

  // A misspelt field is pointed at, with the right name offered.
  await runQuery(page, "assigne = currentUser()");
  await page.waitForSelector("[data-query-error]", { timeout: WAIT });
  const complaint = await page.$eval("[data-query-error]", (el) => el.innerText);
  expect.contains(complaint, 'Did you mean "assignee"', "the sentence offers the field that was meant");
  expect.equal(await page.$eval("[data-query-caret]", (el) => el.textContent), "^", "the caret is under the first character");

  // The project's own list takes a query too.
  await goto(page, `/projects/${key}`);
  await clickButton(page, "Query");
  await runQuery(page, "type = Story");
  await page.waitForFunction(
    (k) => [...document.querySelectorAll("[data-issue-row]")].map((r) => r.dataset.issueRow).join(",") === k,
    { timeout: WAIT },
    story,
  );

  // The plan draws only what the query matched, and its meter still counts the whole project.
  await goto(page, `/projects/${key}/plan?q=${encodeURIComponent("type = Bug")}`);
  rows = await planRows(page);
  expect.truthy(rows.includes(firstBug) && rows.includes(secondBug) && !rows.includes(story), `the plan shows the bugs only: ${rows}`);
  expect.equal(await planFigure(page, "issues"), "3", "the meter is about the whole plan");
});
