// Connected apps: a partner tool asks a person for a token on the consent page.

import { createHash, randomBytes } from "node:crypto";
import { scenario, serialScenario } from "../runner.mjs";
import { BASE_URL, bodyText, clickButton, confirm, expect, fill, goto, makeAccount, PASSWORD, signIn, signUp, textOf, uniqueName, WAIT } from "../helpers.mjs";

/** Where the partner is sent back to: a page of this origin, so the browser lands somewhere it can read. */
const RETURN = `${BASE_URL}/partner-callback`;

/** Registers an app the way a partner does, pending an administrator. */
async function register(name, scope) {
  const response = await fetch(`${BASE_URL}/api/v1/oauth/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ client_name: name, redirect_uris: [RETURN], scope }),
  });
  expect.equal(response.status, 201, "registering the app");
  return response.json();
}

/** The address a partner sends the browser to, and the verifier it keeps. */
function authorize(app, scope, extra = {}) {
  const verifier = randomBytes(32).toString("base64url");
  const challenge = createHash("sha256").update(verifier).digest("base64url");
  const query = new URLSearchParams({
    response_type: "code",
    client_id: app.client_id,
    redirect_uri: RETURN,
    scope,
    state: "partner-state",
    code_challenge: challenge,
    code_challenge_method: "S256",
    ...extra,
  });
  return { path: `/oauth/authorize?${query}`, verifier };
}

/**
 * Waits for the browser to be sent back to the partner and reads what it carries.
 * The partner's page is loaded in full first, since a load started over one still
 * under way is one Chromium sometimes never reports finished.
 */
async function answered(page) {
  await page.waitForFunction(() => location.pathname === "/partner-callback" && document.readyState === "complete", { timeout: WAIT });
  const back = new URL(page.url());
  expect.equal(back.searchParams.get("state"), "partner-state", "the partner's state comes back");
  return back.searchParams;
}

/** Trades a code at the token endpoint as the partner. */
async function trade(app, code, verifier) {
  const response = await fetch(`${BASE_URL}/api/v1/oauth/token`, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      Authorization: `Basic ${Buffer.from(`${encodeURIComponent(app.client_id)}:${encodeURIComponent(app.client_secret)}`).toString("base64")}`,
    },
    body: new URLSearchParams({ grant_type: "authorization_code", code, redirect_uri: RETURN, code_verifier: verifier }),
  });
  expect.equal(response.status, 200, "trading the code");
  return response.json();
}

scenario("an administrator allows a registering app and connects in one step, and a member disconnects it", async ({ page }) => {
  const admin = await signUp(page);
  const app = await register(uniqueName("Stator"), "read issue.write");

  const metadata = await (await fetch(`${BASE_URL}/.well-known/oauth-authorization-server`)).json();
  expect.equal(metadata.code_challenge_methods_supported?.[0], "S256", "the metadata answers through nginx");

  const first = authorize(app, "read issue.write");
  await goto(page, first.path);
  await page.waitForSelector('[data-consent-pending="true"]', { timeout: WAIT });
  expect.contains(await bodyText(page), "asks to be connected to Armature", "the administrator is asked to let it in");
  expect.contains(await bodyText(page), admin.name, "the page names whom the app acts as");
  await page.click('[data-action="consent-allow"]');
  const code = (await answered(page)).get("code");
  const tokens = await trade(app, code, first.verifier);
  expect.equal(tokens.scope, "read issue.write", "the token holds what was allowed");

  const member = await makeAccount(undefined, "member");
  await signIn(page, member.email, PASSWORD, { visit: false });
  const asked = authorize(app, "read");
  await goto(page, asked.path);
  await page.waitForSelector('[data-consent-pending="false"]', { timeout: WAIT });
  expect.contains(await textOf(page, '[data-consent-scope="read"]'), "See the projects and issues you can see", "the scope in words");
  await page.click('[data-action="consent-cancel"]');
  expect.equal((await answered(page)).get("error"), "access_denied", "Cancel tells the app");

  await goto(page, asked.path);
  await page.waitForSelector('[data-action="consent-allow"]', { timeout: WAIT });
  await page.click('[data-action="consent-allow"]');
  await trade(app, (await answered(page)).get("code"), asked.verifier);

  await goto(page, "/settings/tokens");
  await page.waitForSelector(`[data-connected-app-grant="${app.client_name}"]`, { timeout: WAIT });
  await page.click(`[data-connected-app-grant="${app.client_name}"] [data-action="disconnect-app"]`);
  await confirm(page);
  await page.waitForFunction((name) => !document.querySelector(`[data-connected-app-grant="${name}"]`), { timeout: WAIT }, app.client_name);
});

scenario("an app Armature does not know is never sent the browser", async ({ page }) => {
  await signUp(page);
  const app = await register(uniqueName("Exciter"), "read");
  const elsewhere = authorize(app, "read", { redirect_uri: "https://elsewhere.example.test/cb" });
  await goto(page, elsewhere.path);
  await page.waitForSelector("[data-consent-refused]", { timeout: WAIT });
  expect.contains(await bodyText(page), "Ask an Armature administrator to connect it", "the page says who connects it");
  expect.equal(new URL(page.url()).pathname, "/oauth/authorize", "the browser stays");
});

scenario("an administrator adds an app by hand and sees its secret once", async ({ page }) => {
  await signUp(page);
  const name = uniqueName("Rotor");
  await goto(page, "/settings/connected-apps");
  await page.click('[data-action="new-connected-app"]');
  await fill(page, "App name", name);
  await fill(page, "Return addresses", RETURN);
  await page.click('input[data-scope="comment.write"]');
  await page.click('[data-action="save-connected-app"]');
  await page.waitForSelector("[data-client-secret]", { timeout: WAIT });
  expect.contains(await textOf(page, "[data-client-secret]"), "armature_ocs_", "the secret is shown once");
  await clickButton(page, "Done");
  await page.waitForSelector(`[data-connected-app="${name}"][data-connected-app-status="active"]`, { timeout: WAIT });

  await page.click(`[data-connected-app-menu="${name}"]`);
  await page.waitForSelector('[data-action="connected-app-delete"]', { timeout: WAIT });
  await page.click('[data-action="connected-app-delete"]');
  await confirm(page);
  await page.waitForFunction((n) => !document.querySelector(`[data-connected-app="${n}"]`), { timeout: WAIT }, name);
});

/** Calls the API as the partner, with the token it was given. */
async function asPartner(tokens, method, path, body) {
  const response = await fetch(`${BASE_URL}/api/v1${path}`, {
    method,
    headers: { Authorization: `Bearer ${tokens.access_token}`, ...(body ? { "Content-Type": "application/json" } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  return { status: response.status, body: response.status === 204 ? null : await response.json() };
}

// Serial: a partner's webhook hears every project, and goes with its app in the finally.
serialScenario("a partner sets up its own webhook, which the administrator sees and may only switch or remove", async ({ page }) => {
  await signUp(page);
  const app = await register(uniqueName("Stator"), "read webhooks.partner");
  try {
    const asked = authorize(app, "read webhooks.partner");
    await goto(page, asked.path);
    await page.waitForSelector('[data-consent-scope="webhooks.partner"]', { timeout: WAIT });
    await page.click('[data-action="consent-allow"]');
    const tokens = await trade(app, (await answered(page)).get("code"), asked.verifier);

    const made = await asPartner(tokens, "POST", "/webhooks/partner", { url: `${BASE_URL}/partner-inbox`, topics: ["issue.updated"] });
    expect.equal(made.status, 201, "the partner makes its webhook");
    expect.contains(made.body.webhook.secret, "armature_whs_", "its secret comes back once");

    await goto(page, "/settings/webhooks");
    const row = `[data-webhook-partner="${app.client_name}"]`;
    await page.waitForSelector(row, { timeout: WAIT });
    expect.contains(await textOf(page, row), `Managed by ${app.client_name}`, "the list says who manages it");
    const name = await page.$eval(row, (el) => el.getAttribute("data-webhook"));
    await page.click(`[data-webhook-menu="${name}"]`);
    await page.waitForSelector('[data-action="webhook-toggle"]', { timeout: WAIT });
    expect.equal(await page.$('[data-action="webhook-rotate"]'), null, "its secret is the app's to rotate");
    await page.click('[data-action="webhook-toggle"]');
    await page.waitForSelector(`${row}[data-webhook-enabled="false"]`, { timeout: WAIT });

    const read = await asPartner(tokens, "GET", "/webhooks/partner");
    expect.equal(read.body.webhook.enabled, false, "the partner reads it switched off");
  } finally {
    await page.goto(`${BASE_URL}/settings/connected-apps`).catch(() => {});
    await removeApp(page, app.client_name).catch(() => {});
  }
});

/** Removes an app on the settings page, and its webhook with it. */
async function removeApp(page, name) {
  await page.waitForSelector(`[data-connected-app-menu="${name}"]`, { timeout: WAIT });
  await page.click(`[data-connected-app-menu="${name}"]`);
  await page.waitForSelector('[data-action="connected-app-delete"]', { timeout: WAIT });
  await page.click('[data-action="connected-app-delete"]');
  await confirm(page);
}
