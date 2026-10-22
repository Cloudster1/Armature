// Signing up, in and out, tokens, and the palette.

import { scenario, serialScenario } from "../runner.mjs";
import {
  askGuide,
  bodyText,
  clickButton,
  confirm,
  createIssue,
  createProject,
  createToken,
  enrolSecondFactor,
  enrolThroughPage,
  expect,
  fill,
  goto,
  inviteMember,
  acceptInvite,
  keepSecondFactorRule,
  mailFor,
  nextTotp,
  openAccountMenu,
  openPalette,
  signOutThroughMenu,
  PASSWORD,
  reload,
  selectByLabel,
  signInThroughForm,
  signInWithCode,
  signOut,
  signUp,
  signUpThroughForm,
  submit,
  textOf,
  totp,
  unique,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

// Serial: the development stack keeps sign-up open, and somebody signing up
// joins everybody's installation.
serialScenario("somebody signs up through the form and joins the installation as a member", async ({ page }) => {
  await goto(page, "/signup");
  await page.waitForFunction(() => document.body.innerText.includes("Create your account"), { timeout: WAIT });
  expect.notContains((await bodyText(page)).toLowerCase(), "organization", "the form asks for no organization");

  const who = await signUpThroughForm(page);
  expect.contains(await textOf(page, "h1"), who.name.split(" ")[0], "welcome heading");
  // The seat is worded in the avatar menu's header.
  await openAccountMenu(page);
  expect.equal(await textOf(page, "[data-account-role]"), "Member", "somebody who signs up later is a member");
  await page.keyboard.press("Escape");
});

scenario("signing out ends the session and protects the app", async ({ page }) => {
  await signUp(page);

  await signOutThroughMenu(page);
  await waitForPath(page, "/login");

  // Going back to the app must not restore it from cache or a stale cookie.
  await goto(page, "/");
  await waitForPath(page, "/login");
});

scenario("signing out in a second page sends the first to sign in on its next action", async ({ page, context }) => {
  const who = await signUp(page, unique("user"), { visit: false });
  await goto(page, "/settings/tokens");

  const second = await context.newPage();
  // A new page opens narrow, where the sidebar is a rail without the settings links.
  await second.setViewport(page.viewport());
  await goto(second, "/");
  await waitForApp(second);
  await signOutThroughMenu(second);
  await waitForPath(second, "/login");
  await second.close();

  // The first page still believes in its session until it next asks for something.
  await fill(page, "New token name", "Made after signing out");
  await submit(page);
  await waitForPath(page, "/login");
  expect.equal(new URL(page.url()).searchParams.get("next"), "/settings/tokens", "the sign-in keeps where they were");

  // Signing in again returns them there.
  await fill(page, "Email", who.email);
  await fill(page, "Password", PASSWORD);
  await submit(page);
  await waitForPath(page, "/settings/tokens");
  expect.notContains(await bodyText(page), "Made after signing out", "nothing was made while signed out");
});

scenario("an existing account can sign back in", async ({ page }) => {
  const who = await signUp(page);

  await signOutThroughMenu(page);
  await waitForPath(page, "/login");

  await signInThroughForm(page, who.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  expect.contains(await bodyText(page), who.org, "back in the same organization");
});

scenario("a person changes their own password from the profile", async ({ page }) => {
  const who = await signUp(page, unique("user"), { visit: false });
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-change-password]", { timeout: WAIT });
  await fill(page, "Current password", PASSWORD);
  const next = "a freshly chosen adequate password";
  await fill(page, "New password", next);
  await clickButton(page, "Change password");
  await page.waitForFunction(() => document.body.innerText.includes("Password changed"), { timeout: WAIT });

  await signInThroughForm(page, who.email, PASSWORD);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  await signInThroughForm(page, who.email, next);
  await waitForPath(page, "/");
  await waitForApp(page);
});

scenario("a session ended from the profile sends that browser to sign in on its next action", async ({ page, context }) => {
  const who = await signUp(page, unique("user"), { visit: false });

  // The same person signs in on another device, a browser with its own cookies.
  const elsewhere = await context.browser().createBrowserContext();
  try {
    const other = await elsewhere.newPage();
    await other.setViewport(page.viewport());
    await signInThroughForm(other, who.email);
    await waitForPath(other, "/");
    await waitForApp(other);
    await goto(other, "/settings/tokens");

    await goto(page, "/settings/profile");
    await page.waitForSelector('[data-session="current"]', { timeout: WAIT });
    expect.contains(await textOf(page, '[data-session="current"]'), "This device", "this browser is marked");
    await page.waitForSelector('[data-action="end-session"]', { timeout: WAIT });
    await page.click('[data-action="end-session"]');
    await confirm(page);
    await page.waitForFunction(() => !document.querySelector('[data-action="end-session"]'), { timeout: WAIT });

    // The other browser believes in its session until it next asks for something.
    await fill(other, "New token name", "Made after being signed out");
    await submit(other);
    await waitForPath(other, "/login");
    expect.equal(new URL(other.url()).searchParams.get("next"), "/settings/tokens", "the sign-in keeps where they were");
  } finally {
    await elsewhere.close();
  }

  // This browser stays signed in.
  await reload(page);
  await page.waitForSelector('[data-session="current"]', { timeout: WAIT });
  expect.equal(new URL(page.url()).pathname, "/settings/profile", "still on the profile");
});

scenario("a wrong password is refused without saying whether the account exists", async ({ page }) => {
  const who = await signUp(page);
  await signOutThroughMenu(page);
  await waitForPath(page, "/login");

  await signInThroughForm(page, who.email, "definitely not the password");
  await page.waitForSelector("[role=alert]", { timeout: 10_000 });

  const message = await textOf(page, "[role=alert]");
  expect.contains(message.toLowerCase(), "invalid email or password", "rejection message");
  expect.equal(new URL(page.url()).pathname, "/login", "still on the sign-in page");

  // The same message for an address with no account at all.
  await signInThroughForm(page, `nobody-${Date.now()}@armature.test`, PASSWORD);
  await page.waitForSelector("[role=alert]", { timeout: 10_000 });
  expect.equal(await textOf(page, "[role=alert]"), message, "unknown address gives the same answer");
});

scenario("somebody who forgot their password sets a new one from a mailed link", async ({ page }) => {
  const who = await signUp(page, unique("forgot"), { visit: false });
  await signOut(page);
  await page.click('a[href="/forgot-password"]');
  await waitForPath(page, "/forgot-password");

  const since = Date.now() - 1000;
  await fill(page, "Email", who.email);
  await clickButton(page, "Send me a link");
  await page.waitForSelector("[data-reset-sent]", { timeout: WAIT });
  expect.contains(await bodyText(page), "If an account uses that address", "the page gives its one answer");

  // The link names the stack's own address; the secret after the # is what opens the page here.
  const mail = await mailFor(who.email, "Set a new password", { since });
  const secret = mail.text.match(/\/reset-password#(\S+)/)?.[1];
  expect.truthy(secret, "the mail carries a link to the reset page");
  await goto(page, `/reset-password#${secret}`);
  expect.equal(new URL(page.url()).hash, "", "the secret is taken off the address bar");

  const chosen = "a password chosen after forgetting";
  await fill(page, "Password", chosen);
  await fill(page, "Repeat password", chosen);
  await clickButton(page, "Set new password");
  await waitForPath(page, "/login");
  await page.waitForSelector("[data-password-changed]", { timeout: WAIT });

  await signInThroughForm(page, who.email, PASSWORD);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains((await textOf(page, "[role=alert]")).toLowerCase(), "invalid email or password", "the old password is refused");
  await signInThroughForm(page, who.email, chosen);
  await waitForPath(page, "/");
  await waitForApp(page);
});

scenario("a person turns on two-step sign-in and then signs in with the code from the app", async ({ page }) => {
  const who = await signUp(page, unique("twostep"), { visit: false });
  await goto(page, "/settings/profile");
  await page.waitForSelector("[data-second-factor-settings]", { timeout: WAIT });
  const { secret, codes } = await enrolThroughPage(page);
  expect.equal(codes.length, 10, "recovery codes shown once");
  await page.waitForFunction(() => document.body.innerText.includes("10 recovery codes are left"), { timeout: WAIT });
  expect.notContains(await bodyText(page), codes[0], "the recovery codes are gone once put away");

  // The password alone opens nothing; a wrong code is refused on the spot.
  const wrong = String((Number(totp(secret)) + 500_000) % 1_000_000).padStart(6, "0");
  await signInWithCode(page, who.email, wrong);
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains(await textOf(page, "[role=alert]"), "not right", "a wrong code is refused");
  expect.equal(new URL(page.url()).pathname, "/login", "still signing in");

  await page.waitForSelector("#field-code", { timeout: WAIT });
  await page.click("#field-code", { clickCount: 3 });
  await page.keyboard.sendCharacter(nextTotp(secret));
  await submit(page);
  await waitForPath(page, "/");
  await waitForApp(page);
});

scenario("a recovery code signs in once when the phone is not at hand", async ({ page }) => {
  const who = await signUp(page, unique("recovery"), { visit: false });
  const { codes } = await enrolSecondFactor(page);

  await signInWithCode(page, who.email, codes[0], { recovery: true });
  await waitForPath(page, "/");
  await waitForApp(page);

  await signInWithCode(page, who.email, codes[0], { recovery: true });
  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains(await textOf(page, "[role=alert]"), "not right", "a used recovery code is refused");
  expect.equal(new URL(page.url()).pathname, "/login", "still signing in");
});

serialScenario("an administrator requires two-step sign-in and a member is held until they set it up", async ({ page, context }) => {
  await signUp(page, unique("strict"), { visit: false });
  await enrolSecondFactor(page);
  const member = await inviteMember(page, "held");
  const putBack = await keepSecondFactorRule(page);

  // The member signs in in a browser of their own, so this one stays with the
  // administrator who puts the rule back.
  const elsewhere = await context.browser().createBrowserContext();
  try {
    await goto(page, "/settings/access");
    await page.click('[data-access-tab="Sign-in"]');
    await selectByLabel(page, "#field-require-two-step-sign-in", "For everyone");
    await page.click('[data-action="save-second-factor-rule"]');
    await page.waitForFunction(() => document.body.innerText.includes("requirement saved"), { timeout: WAIT });

    const other = await elsewhere.newPage();
    await other.setViewport(page.viewport());
    other.setDefaultTimeout(WAIT);
    await acceptInvite(other, member);
    await other.waitForSelector("[data-enrolment-required]", { timeout: WAIT });
    expect.contains(await bodyText(other), "Set it up to carry on", "the member is told why they are held");
    expect.equal(await other.$("[data-shell]"), null, "nothing of the app is offered while held");

    await enrolThroughPage(other);
    await waitForApp(other);
  } finally {
    await elsewhere.close();
    await putBack();
  }
});

scenario("the session survives a full page reload", async ({ page }) => {
  const who = await signUp(page);

  await reload(page);
  await waitForPath(page, "/");
  expect.contains(await bodyText(page), who.org, "still signed in after reload");
});

scenario("signing in is not offered to someone already signed in", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });

  await goto(page, "/login");
  await waitForPath(page, "/");
  await waitForApp(page);
});

scenario("an API token can be created and revoked", async ({ page }) => {
  await signUp(page);

  await openAccountMenu(page);
  await page.click('[data-account-place="Access tokens"]');
  await waitForPath(page, "/settings/tokens");
  // The path changes before the list has loaded.
  await page.waitForFunction(() => document.body.innerText.includes("No tokens yet"), { timeout: WAIT }).catch(() => {});
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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Palette", { visit: false });
  await createIssue(page, key, "found by key", "Task");
  await openPalette(page);
  await page.type("[data-palette] input", `${key}-1`);
  await page.waitForSelector(`[data-palette-option="key:${key}-1"]`, { timeout: WAIT });
  await page.keyboard.press("Enter");
  await waitForPath(page, `/issues/${key}-1`);
  await page.waitForFunction(() => document.body.innerText.includes("found by key"), { timeout: WAIT });
});

scenario("a question typed into the palette lands on the answer and points at it", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Guided", { visit: false });
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
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Read only", { visit: false });
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
  await signUp(page, unique("user"), { visit: false });
  const near = await createProject(page, "Near", { visit: false });
  const far = await createProject(page, "Far", { visit: false });
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
