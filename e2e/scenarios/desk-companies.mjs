// A desk's companies: customers who read what a colleague shared with them.

import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  chooseDesk,
  clickButton,
  createProject,
  expect,
  fill,
  goto,
  inviteMember,
  signIn,
  signOut,
  signUp,
  textOf,
  unique,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("a colleague reads in the portal what a customer shared with their company", async ({ page }) => {
  const agent = await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Globex desk", { template: "service-desk", visit: false });
  const reporter = await inviteMember(page, "ada", "customer");
  const colleague = await inviteMember(page, "bob", "customer");
  await acceptInvite(page, reporter, { visit: false });
  await acceptInvite(page, colleague, { visit: false });

  // The desk's administrator makes the company and puts both in it by address.
  await signIn(page, agent.email);
  await waitForApp(page);
  await goto(page, `/projects/${key}/service-desk`);
  await page.waitForSelector('[data-guide="desk-companies"]', { timeout: WAIT });
  await fill(page, "Company", "Globex");
  await clickButton(page, "Add company");
  await page.waitForSelector('[data-company="Globex"]', { timeout: WAIT });
  for (const who of [reporter, colleague]) {
    await page.type('[data-company="Globex"] input[type="email"]', who.email);
    await clickButton(page, "Add customer");
    await page.waitForSelector(`[data-company="Globex"] [data-company-member="${who.email}"]`, { timeout: WAIT });
  }

  // The reporter raises a request and shares it with Globex.
  await signIn(page, reporter.email);
  await waitForPath(page, "/portal");
  await goto(page, "/portal/new");
  await chooseDesk(page, key);
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The VPN drops every hour");
  await page.waitForSelector('[data-share-with-company="Globex"]', { timeout: WAIT });
  await page.click('[data-share-with-company="Globex"]');
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  await page.waitForFunction(() => document.body.innerText.includes("Shared with Globex"), { timeout: WAIT });

  // The colleague finds it under the company, reads who raised it, and answers.
  await signIn(page, colleague.email);
  await waitForPath(page, "/portal");
  await page.waitForSelector(`[data-shared-by="Globex"] [data-request="${requestKey}"]`, { timeout: WAIT });
  expect.contains(await textOf(page, `[data-shared-by="Globex"] [data-request="${requestKey}"]`), reporter.name, "the list says who raised it");
  await goto(page, `/portal/requests/${requestKey}`);
  await page.waitForSelector("[data-request-raised-by]", { timeout: WAIT });
  expect.contains(await textOf(page, "[data-request-raised-by]"), "shared with Globex", "the request says whose it is and whom it is shared with");
  expect.truthy(!(await page.$('[data-testid="followers"]')), "a colleague is not shown who follows it");
  await page.type("#portal-reply", "Same here since nine.");
  await clickButton(page, "Reply");
  await page.waitForFunction(() => document.body.innerText.includes("Same here since nine."), { timeout: WAIT });

  // The agent sees where it came from, on the request and in the queue.
  await signIn(page, agent.email);
  await waitForApp(page);
  await goto(page, `/issues/${requestKey}`);
  expect.equal(await textOf(page, "[data-request-company]"), "Globex, shared", "the request names the company it is shared with");
  await goto(page, `/projects/${key}/queues`);
  expect.equal(await textOf(page, `[data-queue-row="${requestKey}"] [data-queue-company]`), "Globex, shared", "the queue names it too");
});
