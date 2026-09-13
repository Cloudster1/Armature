// Signing up, in and out, tokens, and the palette.

import { scenario } from "../runner.mjs";
import {
  askGuide,
  bodyText,
  clickButton,
  confirm,
  createIssue,
  createProject,
  createToken,
  expect,
  fill,
  goto,
  openPalette,
  PASSWORD,
  reload,
  signIn,
  signUp,
  submit,
  textOf,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("a new organization can be created and lands in the app", async ({ page }) => {
  const who = await signUp(page);

  expect.contains(await textOf(page, "h1"), who.name.split(" ")[0], "welcome heading");
  expect.contains(await bodyText(page), who.org, "the organization name is shown");
  expect.contains(await bodyText(page), "Owner", "the first member is the owner");
});

scenario("signing out ends the session and protects the app", async ({ page }) => {
  await signUp(page);

  await page.click('[data-action="sign-out"]');
  await waitForPath(page, "/login");

  // Going back to the app must not restore it from cache or a stale cookie.
  await goto(page, "/");
  await waitForPath(page, "/login");
});

scenario("an existing account can sign back in", async ({ page }) => {
  const who = await signUp(page);

  await page.click('[data-action="sign-out"]');
  await waitForPath(page, "/login");

  await signIn(page, who.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  expect.contains(await bodyText(page), who.org, "back in the same organization");
});

scenario("a wrong password is refused without saying whether the account exists", async ({ page }) => {
  const who = await signUp(page);
  await page.click('[data-action="sign-out"]');
  await waitForPath(page, "/login");

  await signIn(page, who.email, "definitely not the password");
  await page.waitForSelector("[role=alert]", { timeout: 10_000 });

  const message = await textOf(page, "[role=alert]");
  expect.contains(message.toLowerCase(), "invalid email or password", "rejection message");
  expect.equal(new URL(page.url()).pathname, "/login", "still on the sign-in page");

  // The same message for an address with no account at all.
  await signIn(page, `nobody-${Date.now()}@armature.test`, PASSWORD);
  await page.waitForSelector("[role=alert]", { timeout: 10_000 });
  expect.equal(await textOf(page, "[role=alert]"), message, "unknown address gives the same answer");
});

scenario("the session survives a full page reload", async ({ page }) => {
  const who = await signUp(page);

  await reload(page);
  await waitForPath(page, "/");
  expect.contains(await bodyText(page), who.org, "still signed in after reload");
});

scenario("signing in is not offered to someone already signed in", async ({ page }) => {
  await signUp(page);

  await goto(page, "/login");
  await waitForPath(page, "/");
  await waitForApp(page);
});

scenario("an API token can be created and revoked", async ({ page }) => {
  await signUp(page);

  await page.click('a[href="/settings/tokens"]');
  await waitForPath(page, "/settings/tokens");
  expect.contains(await bodyText(page), "No tokens yet", "the list starts empty");

  await fill(page, "New token name", "Deploy pipeline");
  await submit(page);

  await page.waitForFunction(() => document.body.innerText.includes("Copy this token now"), { timeout: WAIT });
  const shown = await bodyText(page);
  expect.contains(shown, "armature_pat_", "the secret is shown once");
  expect.contains(shown, "Deploy pipeline", "the token is listed");

  // Dismissing the panel must not lose the token itself.
  await page.evaluate(() => {
    const button = [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Done");
    button?.click();
  });
  await page.waitForFunction(() => !document.body.innerText.includes("Copy this token now"), { timeout: 10_000 });
  expect.contains(await bodyText(page), "Deploy pipeline", "the token survives dismissing the panel");

  // And the secret is never shown again.
  await reload(page);
  await page.waitForFunction(() => document.body.innerText.includes("Deploy pipeline"), { timeout: 10_000 });
  expect.notContains(await bodyText(page), "armature_pat_", "the secret is not shown after a reload");

  await clickButton(page, "Revoke");
  await confirm(page);
  await page.waitForFunction(() => document.body.innerText.includes("No tokens yet"), { timeout: WAIT });
});

scenario("the palette jumps to an issue by its key", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Palette");
  await createIssue(page, key, "found by key", "Task");
  await openPalette(page);
  await page.type("[data-palette] input", `${key}-1`);
  await page.waitForSelector(`[data-palette-option="key:${key}-1"]`, { timeout: WAIT });
  await page.keyboard.press("Enter");
  await waitForPath(page, `/issues/${key}-1`);
  await page.waitForFunction(() => document.body.innerText.includes("found by key"), { timeout: WAIT });
});

scenario("a question typed into the palette lands on the answer and points at it", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Guided");
  await goto(page, `/projects/${key}`);
  await waitForApp(page);

  const answer = await askGuide(page, "how do I share a dashboard");
  expect.equal(answer, "answer:action:share-dashboard", "the best answer is the share action");
  await waitForPath(page, `/projects/${key}/dashboard`);
  await page.waitForSelector("[data-guide-callout]", { timeout: WAIT });
  expect.contains(await page.$eval("[data-guide-callout]", (el) => el.textContent), "Share on a dashboard", "the callout says what Share does");
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => !document.querySelector("[data-guide-callout]"), { timeout: WAIT });

  // A question that names a search becomes one.
  const query = await askGuide(page, "what is due this week");
  expect.equal(query, "answer:intent:due-week", "the question is a query");
  await waitForPath(page, "/search");
  await page.waitForFunction(() => decodeURIComponent(location.search).includes("endOfWeek()"), { timeout: WAIT });

  // And nonsense gets an honest sentence rather than a guess. Without a model
  // configured, which is this stack's state, nobody is offered to ask one.
  await page.click('[data-action="guide"]');
  await page.waitForSelector('[data-palette][data-palette-mode="ask"]', { timeout: WAIT });
  await page.type("[data-palette] input", "purple monkey dishwasher");
  await page.waitForSelector("[data-palette-empty]", { timeout: WAIT });
  expect.contains(await bodyText(page), "No answer for that yet", "the guide says it has none");
  expect.equal(await page.$('[data-palette-option="ask-assistant"]'), null, "no model is offered when none is configured");
  await page.keyboard.press("Escape");
});

scenario("a read-only token is marked and refused a write", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Read only");
  const secret = await createToken(page, "Assistant", { readOnly: true });
  expect.equal(secret.startsWith("armature_pat_"), true, "the secret is shown once");
  await page.waitForSelector('[data-token="Assistant"][data-token-scope="read"]', { timeout: WAIT });
  expect.contains(await bodyText(page), "Read only", "the row wears the mark");

  // The token reads what its owner reads and is refused the write, with the code that names why.
  const answers = await page.evaluate(
    async (bearer, projectKey) => {
      const headers = { Authorization: `Bearer ${bearer}`, "Content-Type": "application/json" };
      const read = await fetch(`/api/v1/projects/${projectKey}/issues`, { headers });
      const write = await fetch(`/api/v1/projects/${projectKey}/issues`, { method: "POST", headers, body: JSON.stringify({ summary: "from a read token" }) });
      return { read: read.status, write: write.status, code: (await write.json()).error?.code };
    },
    secret,
    key,
  );
  expect.equal(answers.read, 200, "a read token reads");
  expect.equal(answers.write, 403, "a read token is refused a write");
  expect.equal(answers.code, "read_only_token", "the refusal names the token");
});

scenario("a token confined to one project cannot reach another", async ({ page }) => {
  await signUp(page);
  const near = await createProject(page, "Near");
  const far = await createProject(page, "Far");
  const secret = await createToken(page, "Pipeline", { projects: [near] });
  await page.waitForSelector(`[data-token="Pipeline"][data-token-projects="${near}"]`, { timeout: WAIT });
  expect.contains(await bodyText(page), `Only ${near}`, "the row says what the token reaches");

  const answers = await page.evaluate(
    async (bearer, nearKey, farKey) => {
      const headers = { Authorization: `Bearer ${bearer}`, "Content-Type": "application/json" };
      const here = await fetch(`/api/v1/projects/${nearKey}/issues`, { headers });
      const there = await fetch(`/api/v1/projects/${farKey}/issues`, { headers });
      const listed = await (await fetch("/api/v1/projects", { headers })).json();
      return { here: here.status, there: there.status, listed: (listed.projects ?? []).map((p) => p.key) };
    },
    secret,
    near,
    far,
  );
  expect.equal(answers.here, 200, "the key reads the project it names");
  expect.equal(answers.there, 404, "the project it does not name is not found");
  expect.equal(answers.listed.join(","), near, "the listing shows only what the key names");
});
