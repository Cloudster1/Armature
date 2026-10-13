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
  selectAllIssues,
  signUp,
  unique,
  uniqueName,
  WAIT,
  waitForPath,
} from "../helpers.mjs";

scenario("the search bar suggests what to type and what it finds", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Suggesting", { visit: false });
  // Every scenario's issues are searched together, so the words are this one's own.
  const word = uniqueName("printer", "");
  const issueKey = await createIssue(page, key, `The ${word} is on fire`, { type: "Bug", visit: false });

  // A field, its operator, then a value, each picked from the list under the bar,
  // after the project the search is about.
  await goto(page, "/search");
  await page.waitForSelector("[data-query]", { timeout: WAIT });
  await page.click("[data-query]");
  const scope = `project = ${key} AND `;
  await page.type("[data-query]", `${scope}sta`);
  await pickSuggestion(page, "status");
  await page.waitForFunction((want) => document.querySelector("[data-query]")?.value === want, { timeout: WAIT }, `${scope}status `);
  await pickSuggestion(page, "=");
  await page.type("[data-query]", "to");
  await pickSuggestion(page, '"To Do"');
  expect.equal(await page.$eval("[data-query]", (el) => el.value), `${scope}status = "To Do" `, "the bar reads what was picked");
  await page.keyboard.press("Enter");
  await page.waitForFunction((k) => [...document.querySelectorAll("[data-issue-row]")].some((r) => r.dataset.issueRow === k), { timeout: WAIT }, issueKey);

  // Words find issues, and Enter on one opens it.
  await page.click("[data-query]", { clickCount: 3 });
  await page.keyboard.press("Backspace");
  await page.type("[data-query]", word);
  await page.waitForSelector(`[data-query-suggestions] [data-suggestion="issue"][data-suggestion-text="${issueKey}"]`, { timeout: WAIT });
  await pickSuggestion(page, issueKey);
  await waitForPath(page, `/issues/${issueKey}`);
});

scenario("a query finds the tickets it describes and says where it went wrong", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Querying", { visit: false });
  const firstBug = await createIssue(page, key, "Login fails with a valid password", { type: "Bug", visit: false });
  const secondBug = await createIssue(page, key, "Crash on save", { type: "Bug", visit: false });
  const story = await createIssue(page, key, "Write the docs", { type: "Story", visit: false });

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

// Two full pages of the issue lists and a few over, so the last page is short.
const PAST_TWO_PAGES = 205;
const PAGER = 'nav[aria-label="Issue pages"]';

/** Waits for the pager to say which rows are shown, and for the rows to be the ones it says. */
async function showsRange(page, range, rows) {
  await page.waitForFunction(
    (pager, want, count) =>
      document.querySelector(`${pager} [data-page-range]`)?.textContent.trim() === want &&
      document.querySelectorAll("[data-issue-row]").length === count,
    { timeout: WAIT },
    PAGER,
    range,
    rows,
  );
  return page.$$eval("[data-issue-row]", (els) => els.map((el) => el.dataset.issueRow));
}

async function nextPage(page) {
  await page.click(`${PAGER} button[aria-label="Next page"]`);
}

/** Loads the address on screen afresh, as a reload or a shared link would. */
async function reload(page) {
  const { pathname, search } = new URL(page.url());
  await goto(page, `${pathname}${search}`);
}

scenario("search and the project's issue list page past the first hundred", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Long list", { visit: false });
  await goto(page, `/projects/${key}`);

  // Filed through the API: two hundred trips through the dialog would test the dialog.
  const filed = await page.evaluate(async (projectKey, count) => {
    let made = 0;
    for (let i = 0; i < count; i += 1) {
      const response = await fetch(`/api/v1/projects/${projectKey}/issues`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ summary: `listed work ${i}` }),
      });
      if (response.ok) made += 1;
    }
    return made;
  }, key, PAST_TWO_PAGES);
  expect.equal(filed, PAST_TWO_PAGES, "the work was filed");

  // The project's list says how much of the whole it shows, and pages to the rest.
  await goto(page, `/projects/${key}`);
  const first = await showsRange(page, "1 to 100 of 205", 100);
  expect.truthy(await page.evaluate(() => document.body.innerText.includes("1 to 100 of 205 issues")), "the count says how many of the total are shown");
  await nextPage(page);
  const second = await showsRange(page, "101 to 200 of 205", 100);
  expect.equal(new URL(page.url()).searchParams.get("offset"), "100", "the page is in the address");
  await reload(page);
  await showsRange(page, "101 to 200 of 205", 100);
  await nextPage(page);
  const third = await showsRange(page, "201 to 205 of 205", 5);
  expect.equal(new Set([...first, ...second, ...third]).size, PAST_TWO_PAGES, "every issue is on one page or another");

  // Search pages the same result, and a selection covers only the page it was made on.
  await goto(page, `/search?q=${encodeURIComponent(`project = ${key}`)}`);
  await showsRange(page, "1 to 100 of 205", 100);
  expect.equal(await page.$eval('[data-testid="search-count"]', (el) => el.textContent.trim()), "1 to 100 of 205 issues", "the count says which of the result is shown");
  await nextPage(page);
  await showsRange(page, "101 to 200 of 205", 100);
  expect.equal(new URL(page.url()).searchParams.get("offset"), "100", "the page is in the address");
  await reload(page);
  await showsRange(page, "101 to 200 of 205", 100);
  await selectAllIssues(page);
  expect.contains(await page.$eval("[data-bulk-bar]", (el) => el.innerText), "100 selected on this page", "the selection says it is this page");
  await nextPage(page);
  await showsRange(page, "201 to 205 of 205", 5);
  expect.equal(await page.$("[data-bulk-bar]"), null, "another page starts with nothing selected");
});
