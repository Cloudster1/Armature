// Roles, access and single sign-on.

import { scenario, serialScenario } from "../runner.mjs";
import {
  acceptInvite,
  bodyText,
  clickButton,
  createIssue,
  createProject,
  expect,
  fill,
  goto,
  openProjectRow,
  grantRole,
  inviteMember,
  keepProvider,
  press,
  reload,
  removeRole,
  revokeAll,
  selectByLabel,
  signIn,
  signUp,
  textOf,
  WAIT,
  waitForApp,
  inviteFromMembersPage,
  joinThroughLink,
  unique,
  uniqueName,
} from "../helpers.mjs";

scenario("a role in one project hides the others", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const mine = await createProject(page, "Mine", { visit: false });
  const theirs = await createProject(page, "Theirs", { visit: false });
  const secret = await createIssue(page, theirs, "the other project's secret", { type: "Task", visit: false });

  const who = await inviteMember(page, "walled");
  await acceptInvite(page, who, { visit: false });
  await signIn(page, owner.email);
  await waitForApp(page);
  await revokeAll(page, who.name);
  await grantRole(page, { who: who.name, role: "User", projectKey: mine });

  await signIn(page, who.email);
  await waitForApp(page);
  await openProjectRow(page, mine);
  await goto(page, `/projects?q=${theirs}`);
  await page.waitForFunction(() => document.body.innerText.includes("No project matches that"), { timeout: WAIT });
  expect.equal(await page.$(`[data-project="${theirs}"]`), null, "the other project is not listed");

  // Its pages and its work are not there for them, rather than refused.
  await goto(page, `/projects/${theirs}`);
  await page.waitForFunction((key) => document.body.innerText.includes(`There is no project ${key}`), { timeout: WAIT }, theirs);
  await goto(page, `/issues/${secret}`);
  await page.waitForFunction(() => document.body.innerText.includes("was not found"), { timeout: WAIT });
  expect.truthy(!(await bodyText(page)).includes("the other project's secret"), "and its summary is nowhere");
});

scenario("a reader sees a project and changes nothing in it", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Read only", { visit: false });
  await createIssue(page, key, "something they cannot touch", { type: "Task", visit: false });

  const who = await inviteMember(page, "reader");
  await acceptInvite(page, who, { visit: false });

  // Back as the owner to set the roles. Joining grants an ordinary user role,
  // so it is taken away first: this is a test about what a reader alone can do.
  await signIn(page, owner.email);
  await waitForApp(page);
  await revokeAll(page, who.name);
  await grantRole(page, { who: who.name, role: "Reader", projectKey: key });

  await signIn(page, who.email);
  await waitForApp(page);

  // They can see the project.
  await goto(page, `/projects/${key}`);
  expect.contains(await bodyText(page), "Read only", "a reader cannot see the project");

  // And the server refuses everything else, whatever the page offers.
  const refused = await page.evaluate(async (projectKey) => {
    const response = await fetch(`/api/v1/projects/${projectKey}/issues`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ summary: "a reader should not manage this" }),
    });
    return { status: response.status, body: await response.text() };
  }, key);

  expect.equal(refused.status, 403, "a reader was allowed to file an issue");
  expect.contains(refused.body, "issue.write", "the refusal does not name the permission needed");
});

scenario("roles are granted to people and to groups", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Granting", { visit: false });

  const who = await inviteMember(page, "scrum");
  // Groups are everybody's, so this one has a name nobody else took.
  const managers = uniqueName("Release managers");

  await goto(page, "/admin/access");
  await page.click('[data-access-tab="Groups"]');
  await page.waitForSelector("#field-new-group", { timeout: WAIT });
  await page.type("#field-new-group", managers);
  await clickButton(page, "Create group");
  await page.waitForSelector(`[data-group="${managers}"]`, { timeout: WAIT });

  await page.click('[data-access-tab="Roles"]');
  await grantRole(page, { who: managers, role: "Scrum master", projectKey: key });

  const table = await page.$eval('[data-testid="role-assignments"]', (el) => el.innerText);
  expect.contains(table, managers, "the group holds the role");
  expect.contains(table, "Scrum master", "and it is the one that was granted");
  expect.contains(table, key, "over the project it was granted on");
  expect.truthy(who.email.length > 0, "the invitation was issued");
});

// Every scenario's grants share one installation, so the Roles tab shows one
// page of them, and the one a scenario made is found by typing.
scenario("the roles tab shows one page of grants and finds one by typing", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Found grant", { visit: false });
  const crew = uniqueName("Found crew");
  // Made through the API as this person: the grant, not the form, is what the tab is asked to find.
  await goto(page, "/");
  await page.evaluate(
    async (name, projectKey) => {
      const send = async (path, body) => {
        const response = await fetch(`/api/v1${path}`, {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        if (!response.ok) throw new Error(`POST ${path} was refused with ${response.status}`);
        return response.json();
      };
      const { group } = await send("/groups", { name });
      await send("/role-assignments", { role: "user", projectKey, groupId: group.id });
    },
    crew,
    key,
  );

  await goto(page, "/admin/access");
  await page.waitForSelector('[data-testid="role-assignments"] tbody tr', { timeout: WAIT });
  const shown = await page.$$eval('[data-testid="role-assignments"] tbody tr', (rows) => rows.length);
  expect.truthy(shown <= 50, `the tab shows one page of grants, not ${shown}`);

  await page.type("[data-assignment-search]", crew);
  await page.waitForFunction(
    (name) => {
      const rows = [...document.querySelectorAll('[data-testid="role-assignments"] tbody tr')];
      return rows.length === 1 && rows[0].innerText.includes(name);
    },
    { timeout: WAIT },
    crew,
  );
  const row = await page.$eval('[data-testid="role-assignments"] tbody tr', (el) => el.innerText);
  expect.contains(row, key, "the grant found is the one over the scenario's project");
  expect.contains(await bodyText(page), "1 grant", "and the count says it is the only match");
});

scenario("global administration cannot be scoped to one project", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  await createProject(page, "Whole tenant", { visit: false });

  await goto(page, "/admin/access");
  await page.waitForSelector('select[aria-label="Role"]', { timeout: WAIT });
  await selectByLabel(page, 'select[aria-label="Role"]', "Global administrator");

  // The scope picker is disabled rather than offering something the server
  // would refuse.
  const disabled = await page.$eval('select[aria-label="Where the role applies"]', (el) => el.disabled);
  expect.truthy(disabled, "global administration can be pointed at one project");
});

scenario("the access page explains what each role grants", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  await goto(page, "/admin/access");
  await page.waitForSelector('select[aria-label="Role"]', { timeout: WAIT });

  await selectByLabel(page, 'select[aria-label="Role"]', "Reader");
  await page.waitForFunction(() => document.body.innerText.includes("Reader can see projects."), { timeout: WAIT });

  await selectByLabel(page, 'select[aria-label="Role"]', "Scrum master");
  await page.waitForFunction(() => document.body.innerText.includes("plan sprints"), { timeout: WAIT });
});

// Serial: there is one provider for everybody, and no taking it away again.
serialScenario("single sign-on is configured once, and people sign in at one address", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const putBack = await keepProvider();
  try {
    await goto(page, "/admin/access");
    await page.click('[data-access-tab="Single sign-on"]');
    await page.waitForSelector("#field-issuer", { timeout: WAIT });

    await fill(page, "Issuer", "https://id.example.test");
    await fill(page, "Client ID", "armature");
    await fill(page, "Client secret", "a-secret");
    // An installation an earlier run set one up in calls the button Save, and
    // so does the two-step section above it: the provider's is the last one.
    await page.evaluate(() =>
      [...document.querySelectorAll("button")]
        .filter((b) => ["Set up single sign-on", "Save"].includes(b.textContent.trim()))
        .pop()
        ?.click(),
    );

    await page.waitForFunction(() => document.body.innerText.includes("People can sign in at"), { timeout: WAIT });
    expect.equal(await textOf(page, "[data-sso-start]"), "/api/v1/auth/oidc/start", "the address names no organization");

    // The secret is never sent back, only the fact that one is stored.
    await goto(page, "/admin/access");
    await page.click('[data-access-tab="Single sign-on"]');
    await page.waitForSelector("#field-client-secret", { timeout: WAIT });

    const shown = await page.$eval("#field-client-secret", (el) => ({
      value: el.value,
      placeholder: el.placeholder,
    }));
    expect.equal(shown.value, "", "the stored secret was sent back to the browser");
    expect.contains(shown.placeholder, "Stored", "the form does not say a secret is stored");
  } finally {
    await putBack();
  }
});

scenario("the sign-in page offers single sign-on without asking for an organization", async ({ page }) => {
  await goto(page, "/login?next=/projects");
  await page.waitForSelector('[data-action="single-sign-on"]', { timeout: WAIT });
  const href = await page.$eval('[data-action="single-sign-on"]', (a) => a.getAttribute("href"));
  expect.equal(href, "/api/v1/auth/oidc/start?next=%2Fprojects", "single sign-on starts at the installation's one address");
  expect.equal(await page.$("#field-organization"), null, "nobody is asked for an organization");
});

// Reported as the page looking broken: on a narrow screen a table widened the
// whole page and a member's name vanished beside the removal button.
scenario("every tab of the access page fits a phone", async ({ page }) => {
  const owner = await signUp(page);
  const who = unique("longname");
  who.name = uniqueName("Katherine Johnson with a rather long name");
  const { token } = await inviteFromMembersPage(page, who.email);
  await joinThroughLink(page, token, who);
  await signIn(page, owner.email);
  await waitForApp(page);

  await page.setViewport({ width: 400, height: 860 });
  await goto(page, "/admin/access");
  for (const tab of ["Roles", "Members", "Groups", "Single sign-on"]) {
    await page.waitForSelector(`[data-access-tab="${tab}"]`, { timeout: WAIT });
    await page.click(`[data-access-tab="${tab}"]`);
    await page.waitForFunction((t) => document.querySelector(`[data-access-tab="${t}"]`)?.getAttribute("aria-selected") === "true", { timeout: WAIT }, tab);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect.equal(overflow, 0, `the ${tab} tab does not widen the page`);
  }

  await page.click('[data-access-tab="Members"]');
  await page.waitForSelector("[data-people-search]", { timeout: WAIT });
  await page.click("[data-people-search]");
  await page.keyboard.sendCharacter(who.name);
  await page.waitForSelector(`[data-member="${who.name}"]`, { timeout: WAIT });
  const nameWidth = await page.$eval(`[data-member="${who.name}"] .truncate`, (el) => el.getBoundingClientRect().width);
  expect.truthy(nameWidth > 40, `the member's name is still readable, ${nameWidth}px wide`);
});

// Reported from a deployment: a member made a global administrator on the
// Access page could not author workflows, because the library page looked at
// the seat they held rather than at what they had been granted.
scenario("a member granted global administration can author workflows", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const who = await inviteMember(page, "granted");
  await acceptInvite(page, who, { visit: false });

  await signIn(page, owner.email);
  await waitForApp(page);
  await grantRole(page, { who: who.name, role: "Global administrator" });

  await signIn(page, who.email);
  await waitForApp(page);
  await goto(page, "/admin/workflows");
  await page.waitForSelector('[data-workflow-tab="Workflows"]', { timeout: WAIT });
  const offered = await page.evaluate(() => [...document.querySelectorAll("button")].some((b) => b.textContent.trim() === "New workflow"));
  expect.truthy(offered, "a granted administrator is offered a new workflow");
});

// The matrix: a role of the organization's own is added, given a permission
// with one tick, and offered on the Roles tab with what it grants. Serial: the
// roles are everybody's, and the role goes again at the end.
serialScenario("the role matrix adds a role and decides what it grants", async ({ page }) => {
  const owner = await signUp(page, unique("user"), { visit: false });
  const planner = uniqueName("Sprint planner");
  await goto(page, "/admin/access");
  await page.click('[data-access-tab="Matrix"]');
  await page.waitForSelector("[data-role-matrix]", { timeout: WAIT });
  expect.truthy(await page.$('[data-role-column="global_administrator"]'), "the built-in roles are columns");
  const fixed = await page.$eval('[data-permission-cell="global_administrator:org.administer"]', (el) => el.disabled && el.checked);
  expect.truthy(fixed, "the global administrator's administration cannot be unticked");
  const columns = () => page.$$eval("[data-role-column]", (els) => els.map((el) => el.getAttribute("data-role-column")));
  const before = await columns();

  let roleKey = "";
  try {
    await page.click('[data-action="new-role"]');
    await page.waitForSelector("#field-role-name", { timeout: WAIT });
    await page.type("#field-role-name", planner);
    await clickButton(page, "Add role");
    await page.waitForFunction((n) => document.querySelectorAll("[data-role-column]").length > n, { timeout: WAIT }, before.length);
    roleKey = (await columns()).find((key) => !before.includes(key));
    expect.truthy(roleKey.startsWith("sprint_planner"), `the role's key comes from its name: ${roleKey}`);

    // Pressed through the DOM: the dialog that made the role may still be leaving.
    const box = `[data-permission-cell="${roleKey}:sprint.manage"]`;
    await press(page, box);
    await page.waitForFunction((s) => document.querySelector(s)?.checked === true, { timeout: WAIT }, box);
    await reload(page);
    await page.click('[data-access-tab="Matrix"]');
    await page.waitForSelector(box, { timeout: WAIT });
    expect.truthy(await page.$eval(box, (el) => el.checked), "the tick survived a reload");

    await page.click('[data-access-tab="Roles"]');
    await page.waitForSelector('select[aria-label="Role"]', { timeout: WAIT });
    await selectByLabel(page, 'select[aria-label="Role"]', planner);
    await page.waitForFunction((n) => document.body.innerText.includes(`${n} can see projects, plan sprints.`), { timeout: WAIT }, planner);

    // And it is granted like any other, from the same form.
    await grantRole(page, { who: owner.name, role: planner });
    await page.waitForFunction((n) => document.querySelector('[data-testid="role-assignments"]')?.innerText.includes(n), { timeout: WAIT }, planner);
  } finally {
    if (roleKey) await removeRole(roleKey);
  }
});
