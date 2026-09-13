// Roles, access and single sign-on.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  bodyText,
  clickButton,
  createIssue,
  createProject,
  expect,
  goto,
  grantRole,
  inviteMember,
  revokeAll,
  selectByLabel,
  signIn,
  signUp,
  WAIT,
  waitForApp,
} from "../helpers.mjs";

scenario("a role in one project hides the others", async ({ page }) => {
  const owner = await signUp(page);
  const mine = await createProject(page, "Mine");
  const theirs = await createProject(page, "Theirs");
  const secret = await createIssue(page, theirs, "the other project's secret", "Task");

  const who = await inviteMember(page, "walled");
  await acceptInvite(page, who);
  await signIn(page, owner.email);
  await waitForApp(page);
  await revokeAll(page, who.name);
  await grantRole(page, { who: who.name, role: "User", projectKey: mine });

  await signIn(page, who.email);
  await waitForApp(page);
  await goto(page, "/projects");
  await page.waitForSelector(`[data-project="${mine}"]`, { timeout: WAIT });
  expect.equal(await page.$(`[data-project="${theirs}"]`), null, "the other project is not listed");

  // Its pages and its work are not there for them, rather than refused.
  await goto(page, `/projects/${theirs}`);
  await page.waitForFunction((key) => document.body.innerText.includes(`There is no project ${key}`), { timeout: WAIT }, theirs);
  await goto(page, `/issues/${secret}`);
  await page.waitForFunction(() => document.body.innerText.includes("was not found"), { timeout: WAIT });
  expect.truthy(!(await bodyText(page)).includes("the other project's secret"), "and its summary is nowhere");
});

scenario("a reader sees a project and changes nothing in it", async ({ page }) => {
  const owner = await signUp(page);
  const key = await createProject(page, "Read only");
  await createIssue(page, key, "something they cannot touch", "Task");

  const who = await inviteMember(page, "reader");
  await acceptInvite(page, who);

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
  await signUp(page);
  const key = await createProject(page, "Granting");

  const who = await inviteMember(page, "scrum");

  await goto(page, "/settings/access");
  await page.click('[data-access-tab="Groups"]');
  await page.waitForSelector("#field-new-group", { timeout: WAIT });
  await page.type("#field-new-group", "Release managers");
  await clickButton(page, "Create group");
  await page.waitForSelector('[data-group="Release managers"]', { timeout: WAIT });

  await page.click('[data-access-tab="Roles"]');
  await grantRole(page, { who: "Release managers", role: "Scrum master", projectKey: key });

  const table = await page.$eval('[data-testid="role-assignments"]', (el) => el.innerText);
  expect.contains(table, "Release managers", "the group holds the role");
  expect.contains(table, "Scrum master", "and it is the one that was granted");
  expect.contains(table, key, "over the project it was granted on");
  expect.truthy(who.email.length > 0, "the invitation was issued");
});

scenario("global administration cannot be scoped to one project", async ({ page }) => {
  await signUp(page);
  await createProject(page, "Whole tenant");

  await goto(page, "/settings/access");
  await page.waitForSelector('select[aria-label="Role"]', { timeout: WAIT });
  await selectByLabel(page, 'select[aria-label="Role"]', "Global administrator");

  // The scope picker is disabled rather than offering something the server
  // would refuse.
  const disabled = await page.$eval(
    'select[aria-label="Where the role applies"]',
    (el) => el.disabled,
  );
  expect.truthy(disabled, "global administration can be pointed at one project");
});

scenario("the access page explains what each role grants", async ({ page }) => {
  await signUp(page);
  await goto(page, "/settings/access");
  await page.waitForSelector('select[aria-label="Role"]', { timeout: WAIT });

  await selectByLabel(page, 'select[aria-label="Role"]', "Reader");
  await page.waitForFunction(
    () => document.body.innerText.includes("Reader can: read"),
    { timeout: WAIT },
  );

  await selectByLabel(page, 'select[aria-label="Role"]', "Scrum master");
  await page.waitForFunction(
    () => document.body.innerText.includes("sprint.manage"),
    { timeout: WAIT },
  );
});

scenario("single sign-on is configured per organization", async ({ page }) => {
  await signUp(page);
  await goto(page, "/settings/access");
  await page.click('[data-access-tab="Single sign-on"]');
  await page.waitForSelector("#field-issuer", { timeout: WAIT });

  await page.type("#field-issuer", "https://id.example.test");
  await page.type("#field-client-id", "armature");
  await page.type("#field-client-secret", "a-secret");
  await clickButton(page, "Set up single sign-on");

  await page.waitForFunction(
    () => document.body.innerText.includes("People can sign in at"),
    { timeout: WAIT },
  );

  // The secret is never sent back, only the fact that one is stored.
  await goto(page, "/settings/access");
  await page.click('[data-access-tab="Single sign-on"]');
  await page.waitForSelector("#field-client-secret", { timeout: WAIT });

  const shown = await page.$eval("#field-client-secret", (el) => ({
    value: el.value,
    placeholder: el.placeholder,
  }));
  expect.equal(shown.value, "", "the stored secret was sent back to the browser");
  expect.contains(shown.placeholder, "Stored", "the form does not say a secret is stored");
});

scenario("the sign-in page offers single sign-on", async ({ page }) => {
  await goto(page, "/login");
  await clickButton(page, "Sign in with single sign-on");
  await page.waitForSelector("#field-organization", { timeout: WAIT });

  await page.type("#field-organization", "some-company");
  const enabled = await page.evaluate(
    () =>
      ![...document.querySelectorAll("button")].find(
        (b) => b.textContent.trim() === "Continue to your provider",
      )?.disabled,
  );
  expect.truthy(enabled, "the button to continue is not usable once an organization is named");
});
