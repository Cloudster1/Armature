// Where the menus put things (#308, #462): the person's own pages under the
// avatar and in a settings area of their own, the installation's on the Admin
// page, the work in the sidebar, and all of it in a drawer on a phone.

import { scenario } from "../runner.mjs";
import { expect, goto, openAccountMenu, openDrawer, openPalette, signUp, unique, WAIT, waitForApp, waitForPath, withViewport } from "../helpers.mjs";

const PERSONAL = ["Your profile", "Notification settings", "Watching", "Themes", "Access tokens"];
const ADMIN = [
  "Installation",
  "Access",
  "Users",
  "Holidays",
  "Workflows",
  "Issue types",
  "Project templates",
  "Labels",
  "Automation",
  "Fields",
  "Issue view",
  "Connected apps",
  "Webhooks",
  "Audit log",
];

/** The words of the open avatar menu's entries, in order. */
async function accountEntries(page) {
  return page.$$eval('[role="menu"][aria-label="Your account"] [role="menuitem"]', (items) => items.map((item) => item.textContent.trim()));
}

/** The words of a sidebar group's links, by its id. */
async function groupLinks(page, id) {
  return page.$$eval(`[data-sidebar-group="${id}"] [data-place]`, (links) => links.map((a) => a.getAttribute("data-place")));
}

/** What the palette offers for some words, by each option's words. */
async function paletteOffers(page, words) {
  await openPalette(page);
  await page.type("[data-palette] input", words);
  // The pages are matched as the words are typed; issues arrive later and are not asked about here.
  await page.waitForFunction((w) => document.querySelector("[data-palette] input")?.value === w, { timeout: WAIT }, words);
  const offered = await page.$$eval("[data-palette-option]", (options) => options.map((o) => o.textContent.trim()));
  await page.keyboard.press("Escape");
  return offered;
}

scenario(
  "an administrator's avatar menu holds their pages, Admin and the way out, and Admin opens the page of the installation's settings",
  async ({ page }) => {
    const who = await signUp(page, unique("menuadmin"));
    await goto(page, "/projects");
    await waitForApp(page);

    await openAccountMenu(page);
    const header = await page.$eval("[data-account-header]", (el) => el.textContent);
    expect.contains(header, who.name, "the header names the person");
    expect.contains(header, who.email, "and their address");
    expect.contains(header, "Admin", "and their seat");
    const placed = await page.evaluate(() => {
      const menu = document.querySelector('[role="menu"][aria-label="Your account"]');
      const rail = document.querySelector("[data-rail]");
      return { inRail: Boolean(rail?.contains(menu)), right: menu.getBoundingClientRect().left >= rail.getBoundingClientRect().right };
    });
    expect.truthy(placed.inRail, "the account menu is drawn outside the rail's landmark");
    expect.truthy(placed.right, "the account menu does not open to the right of the rail");
    const entries = await accountEntries(page);
    expect.equal(JSON.stringify(entries.slice(0, PERSONAL.length)), JSON.stringify(PERSONAL), "the personal pages come first, in order");
    expect.equal(JSON.stringify(entries.slice(-2)), JSON.stringify(["Admin", "Sign out"]), "then Admin and Sign out");

    await page.click('[role="menu"] [data-action="admin"]');
    await waitForPath(page, "/admin");
    expect.equal(await page.$('[data-sidebar-area="settings"]'), null, "the work sidebar stays");
    expect.truthy(await page.$('[data-sidebar-area="work"]'), "on the Admin page");
    const cards = await page.$$eval("[data-admin-card]", (links) => links.map((a) => a.getAttribute("data-admin-card")));
    expect.equal(JSON.stringify(cards), JSON.stringify(ADMIN), "the index lists the installation's pages in order");

    await page.click('[data-admin-nav] [data-admin-link="Audit log"]');
    await waitForPath(page, "/admin/audit");

    await goto(page, "/settings");
    await page.waitForSelector('[data-sidebar-area="settings"]', { timeout: WAIT });
    expect.equal(await page.$('[data-sidebar-group="work"]'), null, "the work's groups give way to the settings'");
    expect.equal(JSON.stringify(await groupLinks(page, "you")), JSON.stringify(PERSONAL), "You lists the personal pages");
    expect.equal(await page.$('[data-sidebar-group="settings"]'), null, "and the installation's pages are not among them");
    const hub = await page.$$eval("[data-settings-link]", (links) => links.map((a) => a.getAttribute("data-settings-link")));
    expect.equal(JSON.stringify(hub), JSON.stringify(PERSONAL), "the hub lists the personal pages only");
    await page.click('[data-action="back-from-settings"]');
    await waitForPath(page, "/admin/audit");

    const offered = await paletteOffers(page, "audit");
    expect.truthy(
      offered.some((o) => o.includes("Admin / Audit log")),
      `the palette reaches the audit log: ${offered.join(", ")}`,
    );
  },
);

scenario("a member sees no Admin in the avatar menu, the sidebar or the palette", async ({ page }) => {
  await signUp(page, unique("menumember"), { role: "member" });
  await openAccountMenu(page);
  const entries = await accountEntries(page);
  expect.equal(entries.includes("Admin"), false, `no Admin entry: ${entries.join(", ")}`);
  expect.equal(JSON.stringify(entries.slice(0, PERSONAL.length)), JSON.stringify(PERSONAL), "the personal pages are there");
  await page.keyboard.press("Escape");

  await goto(page, "/settings");
  await page.waitForSelector('[data-sidebar-area="settings"]', { timeout: WAIT });
  expect.equal(await page.$('[data-sidebar-group="settings"]'), null, "the settings area has no installation group");
  expect.equal(await page.$('[data-settings-group="Settings"]'), null, "the hub has no installation card");
  expect.equal(await page.$('a[href^="/admin"]'), null, "and nothing leads to the Admin page");
  expect.equal(JSON.stringify(await groupLinks(page, "you")), JSON.stringify(PERSONAL), "You is all there is");

  for (const words of ["audit", "webhooks", "users"]) {
    const offered = await paletteOffers(page, words);
    expect.equal(offered.filter((o) => o.startsWith("Admin")).length, 0, `the palette offers no admin page for ${words}: ${offered.join(", ")}`);
  }
  const offered = await paletteOffers(page, "tokens");
  expect.truthy(offered.includes("Access tokens"), `the palette reaches the member's own pages: ${offered.join(", ")}`);
});

scenario("the bell opens the latest notifications with the way to all of them and their settings", async ({ page }) => {
  await signUp(page, unique("bell"));
  await page.click('[data-action="notifications"]');
  await page.waitForSelector("[data-notifications-panel]", { timeout: WAIT });
  expect.truthy(await page.$('[data-notifications-panel] [data-action="mark-all-read"]'), "it offers to mark them all read");
  expect.truthy(await page.$('[data-notifications-panel] [data-action="notification-settings"]'), "and their settings");
  await page.click('[data-action="see-all-notifications"]');
  await waitForPath(page, "/notifications");
  expect.equal(await page.$("[data-notifications-panel]"), null, "following a link closes the panel");
  const sidebar = await page.$eval('[data-sidebar-group="work"]', (el) => el.innerText);
  expect.contains(sidebar, "Notifications", "the sidebar calls it Notifications");
  expect.equal(sidebar.includes("Inbox"), false, "and no longer Inbox");
});

scenario("at phone width a drawer holds every group and closes on following a link", async ({ page }) => {
  await signUp(page, unique("phone"));
  await withViewport(page, 375, async () => {
    await goto(page, "/");
    await page.waitForSelector('[data-action="open-drawer"]', { timeout: WAIT });
    expect.equal(await page.$("[data-rail]"), null, "the rail gives way");
    expect.equal(await page.$('[data-sidebar="open"], [data-sidebar="rail"]'), null, "and so does the sidebar");

    await openDrawer(page);
    for (const group of ["work", "project"])
      expect.truthy(await page.$(`[data-sidebar="drawer"] [data-sidebar-group="${group}"]`), `the drawer holds the ${group} group`);
    const foot = await page.$eval("[data-drawer-foot]", (el) => el.innerText);
    for (const word of [...PERSONAL, "Admin", "Sign out"]) expect.contains(foot, word, `the drawer's foot offers ${word}`);
    expect.truthy(await page.$('[data-drawer-foot] [data-action="theme"]'), "and the theme switch");
    // The stamp waits on /meta, which may answer after the drawer opens.
    expect.truthy(await page.waitForSelector("[data-drawer-foot] [data-build]", { timeout: WAIT }).catch(() => null), "and the build stamp");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect.equal(overflow, 0, "nothing widens the page");

    await page.click('[data-sidebar="drawer"] [data-place="Notifications"]');
    await waitForPath(page, "/notifications");
    await page.waitForFunction(() => !document.querySelector('[data-sidebar="drawer"]'), { timeout: WAIT });

    await openDrawer(page);
    await page.click('[data-drawer-foot] [data-action="admin"]');
    await waitForPath(page, "/admin");
    await page.waitForFunction(() => !document.querySelector('[data-sidebar="drawer"]'), { timeout: WAIT });
    await goto(page, "/settings");
    await openDrawer(page, '[data-sidebar="drawer"][data-sidebar-area="settings"] [data-sidebar-group="you"]');
    expect.truthy(await page.$('[data-sidebar="drawer"] [data-action="back-from-settings"]'), "in the settings area the drawer holds the settings sidebar");
  });
});
