// The Admin page (#462): everything an administrator sets in one place, for
// them alone, with the old /settings addresses leading to it.

import { scenario } from "../runner.mjs";
import { expect, goto, signUp, unique, WAIT, waitForApp, waitForPath, withViewport } from "../helpers.mjs";

const SECTIONS = [
  ["Installation", "/admin/installation"],
  ["Access", "/admin/access"],
  ["Users", "/admin/users"],
  ["Holidays", "/admin/holidays"],
  ["Workflows", "/admin/workflows"],
  ["Issue types", "/admin/issue-types"],
  ["Labels", "/admin/labels"],
  ["Automation", "/admin/automation"],
  ["Fields", "/admin/fields"],
  ["Issue view", "/admin/issue-view"],
  ["Connected apps", "/admin/connected-apps"],
  ["Webhooks", "/admin/webhooks"],
  ["Audit log", "/admin/audit"],
];

scenario("an administrator reaches every section from the Admin page, by its cards and by its navigation", async ({ page }) => {
  await signUp(page, unique("adminpage"));
  await goto(page, "/admin");
  await page.waitForSelector("[data-admin-card]", { timeout: WAIT });
  for (const [label, path] of SECTIONS) {
    await page.click(`[data-admin-card="${label}"]`);
    await waitForPath(page, path);
    expect.truthy(await page.$(`[data-admin-nav] [data-admin-link="${label}"]`), `${label} is in the navigation`);
    await page.click('[data-admin-nav] [data-admin-link="Admin"]');
    await waitForPath(page, "/admin");
  }
});

scenario("on a phone the Admin page's sections are a menu", async ({ page }) => {
  await signUp(page, unique("adminphone"));
  await withViewport(page, 375, async () => {
    await goto(page, "/admin");
    await page.waitForSelector("[data-admin-menu]", { timeout: WAIT });
    expect.equal(await page.$("[data-admin-nav]"), null, "the list gives way to the menu");
    await page.click('[data-action="admin-sections"]');
    await page.click('[role="menu"] [data-admin-link="Webhooks"]');
    await waitForPath(page, "/admin/webhooks");
  });
});

scenario("a member is refused the Admin page, sees no Admin entry, and nothing of the page is fetched", async ({ page }) => {
  await signUp(page, unique("adminmember"), { role: "member" });
  expect.equal(await page.$('[data-place="Admin"]'), null, "the sidebar has no Admin entry");
  const asked = [];
  page.on("request", (request) => asked.push(new URL(request.url()).pathname));
  for (const path of ["/admin", "/admin/users", "/admin/access"]) {
    await goto(page, path);
    await waitForApp(page);
    await page.waitForFunction(() => document.body.innerText.includes("Only administrators can open this page"), { timeout: WAIT });
    expect.equal(await page.$("[data-admin-nav]"), null, `${path} shows no navigation`);
  }
  expect.equal(asked.filter((p) => /\/api\/v1\/(users|roles|permissions|audit|webhooks)/.test(p)).length, 0, "no admin request went out");
});

scenario("an old settings address leads to the Admin page, keeping its search", async ({ page }) => {
  await signUp(page, unique("adminmoved"));
  await goto(page, "/settings/access?tab=members");
  await waitForPath(page, "/admin/access");
  expect.equal(new URL(page.url()).searchParams.get("tab"), "members", "the tab is kept");
  await goto(page, "/settings/workflows/schemes");
  await waitForPath(page, "/admin/workflows/schemes");
  await goto(page, "/settings/organization");
  await waitForPath(page, "/admin/installation");
  await goto(page, "/settings/profile");
  await waitForPath(page, "/settings/profile");
});
