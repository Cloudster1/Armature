// What the application does before anybody is anywhere in particular.

import { scenario, serialScenario } from "../runner.mjs";
import {
  bodyText,
  clickButton,
  createProject,
  enterOpenDesk,
  signOut,
  toggleDoor,
  withViewport,
  connectTool,
  openAccountMenu,
  signOutThroughMenu,
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
  await signOutThroughMenu(page);
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
  expect.equal(await page.evaluate(() => document.documentElement.getAttribute("data-theme")), "dark", "the document is stamped with the theme");

  await reload(page);
  await page.waitForSelector(themeButton, { timeout: 10_000 });
  expect.equal(await textOf(page, themeButton), "Dark", "the choice survived the reload");
});

// A missing file would come back as the app's index.html, so the type is what proves it is there.
scenario("the logo and the icons are served from the site's root, each with its own type", async ({ page }) => {
  await goto(page, "/login");
  const served = await page.evaluate(
    async (paths) => {
      const out = {};
      for (const path of paths) {
        const response = await fetch(path);
        out[path] = `${response.status} ${response.headers.get("content-type")}`;
      }
      return out;
    },
    ["/favicon.ico", "/favicon-32.png", "/apple-touch-icon.png", "/icon-512.png", "/logo-256.webp", "/mark-96.webp", "/site.webmanifest"],
  );
  for (const [path, type] of Object.entries({
    "/favicon.ico": /image\/(x-icon|vnd\.microsoft\.icon)/,
    "/favicon-32.png": /image\/png/,
    "/apple-touch-icon.png": /image\/png/,
    "/icon-512.png": /image\/png/,
    "/logo-256.webp": /image\/webp/,
    "/mark-96.webp": /image\/webp/,
    "/site.webmanifest": /application\/manifest\+json/,
  })) {
    expect.truthy(served[path].startsWith("200 ") && type.test(served[path]), `${path} answered ${served[path]}`);
  }
});

scenario("the sign-in page shows the logo and the sidebar the mark, which the content security policy lets load", async ({ page }) => {
  const refused = [];
  page.on("console", (message) => {
    if (/Content Security Policy|Refused to load/i.test(message.text())) refused.push(message.text());
  });
  const loaded = (selector) => page.waitForFunction((s) => document.querySelector(s)?.naturalWidth > 0, { timeout: WAIT }, selector);

  await goto(page, "/login");
  await loaded("[data-door] img[data-logo]");
  expect.equal(await textOf(page, "[data-logo-head]"), "Armature", "the name under the logo");

  await signUp(page);
  await loaded("[data-sidebar] img[data-logo-mark]");
  expect.equal(refused.length, 0, `the policy refused: ${refused.join(" | ")}`);
});

scenario("the keyboard reaches the content from the top of a page through the skip link", async ({ page }) => {
  await signUp(page);
  await goto(page, "/projects");
  await page.waitForSelector("main#main", { timeout: WAIT });
  await page.evaluate(() => document.activeElement?.blur());

  await page.keyboard.press("Tab");
  const first = await page.evaluate(() => {
    const el = document.activeElement;
    const box = el.getBoundingClientRect();
    return { skip: el.hasAttribute("data-skip-link"), text: el.textContent.trim(), shown: box.width > 1 && box.height > 1 };
  });
  expect.truthy(first.skip, "the first Tab lands on the skip link");
  expect.equal(first.text, "Skip to the content", "the skip link's words");
  expect.truthy(first.shown, "the skip link shows once focused");

  await page.keyboard.press("Enter");
  expect.equal(await page.evaluate(() => document.activeElement?.id), "main", "focus after the skip link");
  expect.equal(await page.evaluate(() => location.pathname), "/projects", "the skip link left the address alone");

  await page.keyboard.press("Tab");
  expect.truthy(
    await page.evaluate(() => document.querySelector("main#main").contains(document.activeElement)),
    "the next Tab stays in the content, past the navigation",
  );
});

// The padding follows the stylesheet's shell breakpoint, not a branch in code, so both shells get it.
scenario("at a phone's width each shell's pages start one rem from the edge, as Stator's do, and do not scroll sideways", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const desk = await createProject(page, "Phone desk", { template: "service-desk", visit: false });
  await toggleDoor(page, desk);
  const fits = async (shell) => {
    await page.waitForSelector("main#main [data-shell-content]", { timeout: WAIT });
    const got = await page.evaluate(() => {
      const content = document.querySelector("main#main [data-shell-content]");
      const main = document.getElementById("main");
      return {
        start: content.getBoundingClientRect().left + parseFloat(getComputedStyle(content).paddingLeft),
        rem: parseFloat(getComputedStyle(document.documentElement).fontSize),
        page: document.documentElement.scrollWidth - window.innerWidth,
        main: main.scrollWidth - main.clientWidth,
      };
    });
    expect.equal(got.start, got.rem, `where the ${shell}'s content starts, in px`);
    expect.truthy(got.page <= 0 && got.main <= 0, `the ${shell} scrolls sideways by ${Math.max(got.page, got.main)}px`);
  };

  await withViewport(page, 375, async () => {
    await goto(page, "/inbox");
    await fits("app");
    await signOut(page, { visit: false });
    await enterOpenDesk(page, desk, unique("walkin"));
    await fits("portal");
  });
});

// The profile's Language changes the interface as well as the dates, and a reload keeps it.
scenario("a person who picks Deutsch on their profile sees the sidebar in German, and English again after", async ({ page }) => {
  await signUp(page);
  const languageSelect = 'select:has(option[value="de-DE"])';
  const sidebarSays = (word) => page.waitForSelector(`[data-sidebar] [data-place="${word}"]`, { timeout: WAIT });

  await goto(page, "/settings/profile");
  await page.waitForSelector(languageSelect, { timeout: WAIT });
  await page.select(languageSelect, "de-DE");
  await clickButton(page, "Save changes");
  await sidebarSays("Ihr Profil");
  expect.equal(await page.evaluate(() => document.documentElement.lang), "de", "the page's language");

  await goto(page, "/");
  await sidebarSays("Benachrichtigungen");
  await reload(page);
  await sidebarSays("Gespeicherte Suchen");

  await goto(page, "/settings/profile");
  await page.waitForSelector(languageSelect, { timeout: WAIT });
  await page.select(languageSelect, "");
  await clickButton(page, "Änderungen speichern");
  await sidebarSays("Your profile");
  expect.equal(await page.evaluate(() => document.documentElement.lang), "en", "the page's language once back");
});

scenario("nothing in the page reports a script error", async ({ page }) => {
  const problems = [];
  page.on("console", (message) => {
    if (message.type() === "error") problems.push(message.text());
  });
  page.on("pageerror", (error) => problems.push(error.message));

  await signUp(page);
  await openAccountMenu(page);
  await page.click('[data-account-place="Access tokens"]');
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
    links.map((a) => ({
      product: a.getAttribute("data-switch-to"),
      tag: a.tagName,
      href: a.getAttribute("href"),
      target: a.getAttribute("target"),
      text: a.textContent,
    })),
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
    expect.equal(
      JSON.stringify(menu.tools.map((t) => [t.product, t.href])),
      JSON.stringify([
        ["stator", stator],
        ["exciter", exciter],
      ]),
      "both are listed, Stator first",
    );

    while (made.length) await disconnectTool(made.pop());
    await reload(page);
    menu = await switchTo(page);
    expect.equal(menu.tools.length, 0, "with none connected nothing is listed");
    expect.equal(menu.heading, false, "and the group is gone");
  } finally {
    for (const id of made) await disconnectTool(id).catch(() => {});
  }
});
