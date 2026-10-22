// What the application does before anybody is anywhere in particular.

import { scenario, serialScenario } from "../runner.mjs";
import {
  bodyText,
  connectTool,
  disconnectTool,
  expect,
  fill,
  goto,
  PASSWORD,
  reload,
  signUp,
  submit,
  textOf,
  unique,
  WAIT,
  waitForPath,
} from "../helpers.mjs";

scenario("an anonymous visitor is sent to sign in, not shown the app", async ({ page }) => {
  await goto(page, "/");
  await waitForPath(page, "/login");

  await goto(page, "/settings/tokens");
  await waitForPath(page, "/login");
});

scenario("an unknown path shows the not-found page rather than a blank screen", async ({ page }) => {
  await goto(page, "/no-such-page");
  expect.contains(await bodyText(page), "Page not found", "not-found page");
});

serialScenario("form validation is reported against the field, not swallowed", async ({ page }) => {
  const who = unique("dupe");
  await signUp(page, who);
  await page.click('[data-action="sign-out"]');
  await waitForPath(page, "/login");

  // The same address a second time must be reported, not silently accepted.
  await goto(page, "/signup");
  await fill(page, "Your name", "Someone Else");
  await fill(page, "Work email", who.email);
  await fill(page, "Password", PASSWORD);
  await fill(page, "Repeat password", PASSWORD);
  await submit(page);

  await page.waitForSelector("[role=alert]", { timeout: WAIT });
  expect.contains((await textOf(page, "[role=alert]")).toLowerCase(), "already exists", "duplicate email message");
});

serialScenario("a password typed differently the second time creates no account", async ({ page }) => {
  const who = unique("typo");
  await goto(page, "/signup");
  await fill(page, "Your name", who.name);
  await fill(page, "Work email", who.email);
  await fill(page, "Password", PASSWORD);
  await fill(page, "Repeat password", `${PASSWORD}x`);
  await submit(page);

  await page.waitForFunction(() => document.body.innerText.includes("The two passwords are not the same"), { timeout: WAIT });
  expect.equal(new URL(page.url()).pathname, "/signup", "still on the sign-up page");

  // Nothing was sent, so the address is still free for the corrected attempt.
  await fill(page, "Repeat password", PASSWORD);
  await submit(page);
  await waitForPath(page, "/");
});

scenario("the theme choice persists across a reload", async ({ page }) => {
  await signUp(page);

  const themeButton = '[data-action="theme"]';
  expect.equal(await textOf(page, themeButton), "Auto", "starts following the system");

  await page.click(themeButton);
  expect.equal(await textOf(page, themeButton), "Light", "cycles to light");
  await page.click(themeButton);
  expect.equal(await textOf(page, themeButton), "Dark", "cycles to dark");
  expect.equal(
    await page.evaluate(() => document.documentElement.getAttribute("data-theme")),
    "dark",
    "the document is stamped with the theme",
  );

  await reload(page);
  await page.waitForSelector(themeButton, { timeout: 10_000 });
  expect.equal(await textOf(page, themeButton), "Dark", "the choice survived the reload");
});

scenario("nothing in the page reports a script error", async ({ page }) => {
  const problems = [];
  page.on("console", (message) => {
    if (message.type() === "error") problems.push(message.text());
  });
  page.on("pageerror", (error) => problems.push(error.message));

  await signUp(page);
  await page.click('a[href="/settings/tokens"]');
  await waitForPath(page, "/settings/tokens");
  await goto(page, "/");

  // A 401 during the anonymous phase of loading is expected and logged by the
  // browser itself, so only genuine script failures count.
  const real = problems.filter((p) => !p.includes("401") && !p.includes("Failed to load resource"));
  expect.equal(real.length, 0, `console errors: ${real.join(" | ")}`);
});

// The build is named where a problem report can read it out.
scenario("the foot of the sidebar names the running build", async ({ page }) => {
  await signUp(page);
  await page.waitForSelector("[data-build]", { timeout: WAIT });
  const stamp = await textOf(page, "[data-build]");
  expect.truthy(/\S/.test(stamp), "the foot names a build");
  const title = await page.$eval("[data-build]", (el) => el.getAttribute("title") ?? "");
  expect.truthy(stamp.startsWith("dev") || /\(/.test(stamp) || title.includes("commit"), `the stamp reads as a build: ${stamp}`);
});

/** Opens the avatar menu and reads its Switch to group: each tool's name, address and how it opens. */
async function switchTo(page) {
  await page.click('[data-action="account"]');
  await page.waitForSelector('[role="menu"]', { timeout: WAIT });
  const tools = await page.$$eval("[data-switch-to]", (links) =>
    links.map((a) => ({ product: a.getAttribute("data-switch-to"), tag: a.tagName, href: a.getAttribute("href"), target: a.getAttribute("target"), text: a.textContent })),
  );
  const heading = await page.$('[data-menu-section="Switch to"]');
  await page.keyboard.press("Escape");
  return { tools, heading: heading !== null };
}

serialScenario("the avatar menu links the connected tools, only those connected, and drops the group with none", async ({ page }) => {
  await signUp(page, unique("switcher"), { role: "member" });
  const stator = `https://${unique("stator").email.split("@")[0]}.example.test`;
  const exciter = `https://${unique("exciter").email.split("@")[0]}.example.test`;
  const made = [];
  try {
    made.push(await connectTool("stator", stator));
    await reload(page);
    let menu = await switchTo(page);
    expect.equal(menu.heading, true, "one connected tool brings the Switch to group");
    expect.equal(JSON.stringify(menu.tools.map((t) => t.product)), JSON.stringify(["stator"]), "only Stator is listed");
    expect.equal(menu.tools[0].href, stator, "Stator opens at its connection's address");
    expect.equal(menu.tools[0].tag, "A", "it is a plain link, which the browser can open in a new tab");

    made.push(await connectTool("exciter", exciter));
    await reload(page);
    menu = await switchTo(page);
    expect.equal(JSON.stringify(menu.tools.map((t) => [t.product, t.href])), JSON.stringify([["stator", stator], ["exciter", exciter]]), "both are listed, Stator first");

    while (made.length) await disconnectTool(made.pop());
    await reload(page);
    menu = await switchTo(page);
    expect.equal(menu.tools.length, 0, "with none connected nothing is listed");
    expect.equal(menu.heading, false, "and the group is gone");
  } finally {
    for (const id of made) await disconnectTool(id).catch(() => {});
  }
});
