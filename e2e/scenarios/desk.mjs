// The service desk as a customer meets it: requests, files, templates and the door.

import { writeFileSync } from "node:fs";
import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  attachFile,
  bodyText,
  clickButton,
  confirm,
  createProject,
  createTeam,
  enterDesk,
  enterOpenDesk,
  expect,
  fill,
  goto,
  inviteMember,
  mailFor,
  orgSlug,
  queueFiles,
  selectByLabel,
  signIn,
  signOut,
  signUp,
  textOf,
  toggleDoor,
  trustDomain,
  turnedAwayAtOpenDesk,
  unique,
  WAIT,
  waitForApp,
  waitForPath,
} from "../helpers.mjs";

scenario("a customer raises a request and sees only what the desk says to them", async ({ page }) => {
  const agent = await signUp(page);
  const key = await createProject(page, "Helpdesk", "service-desk");

  // A customer is invited as one, and lands in the portal rather than the app.
  const customer = await inviteMember(page, "customer", "customer");
  await acceptInvite(page, customer);
  await goto(page, "/");
  await waitForPath(page, "/portal");
  await page.waitForFunction(() => document.body.innerText.includes("Your requests"), { timeout: WAIT });
  expect.truthy(!(await bodyText(page)).includes("Workflows"), "the portal has none of the agents' chrome");

  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The printer is on fire");
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  expect.truthy(requestKey.startsWith(key + "-"), "the request is an issue in the desk's project");
  await page.waitForFunction(() => document.body.innerText.includes("The printer is on fire"), { timeout: WAIT });
  expect.contains(await bodyText(page), "Waiting for support", "and starts where the desk's workflow starts");

  // The agent finds it in the queue, with its clocks, and answers it with a
  // note for colleagues and a reply for the customer.
  await signOut(page);
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/projects/${key}/queues`);
  await page.waitForSelector(`[data-queue-row="${requestKey}"]`, { timeout: WAIT });
  const queueRow = await page.$eval(`[data-queue-row="${requestKey}"]`, (el) => el.innerText);
  expect.contains(queueRow, "Report a problem", "the queue names the request type");
  expect.contains(queueRow, "left", "and how long the desk has");

  await goto(page, `/issues/${requestKey}`);
  await page.waitForSelector('[data-testid="sla"]', { timeout: WAIT });
  await page.click('[data-action="internal-note"]');
  await page.type("#new-comment", "Fire brigade called, ETA five minutes");
  await clickButton(page, "Add note");
  await page.waitForFunction(() => document.body.innerText.includes("internal note"), { timeout: WAIT });
  await page.click('[data-action="internal-note"]');
  await page.type("#new-comment", "Help is on the way, stay clear of the printer.");
  await clickButton(page, "Comment");
  await page.waitForFunction(() => document.body.innerText.includes("stay clear of the printer"), { timeout: WAIT });
  await page.waitForFunction(
    () => document.querySelector('[data-sla="first_response"]')?.getAttribute("data-sla-state") === "completed",
    { timeout: WAIT },
  );

  // The customer sees the reply, not the note.
  await signOut(page);
  await signIn(page, customer.email);
  await waitForPath(page, "/portal");
  await goto(page, `/portal/requests/${requestKey}`);
  await page.waitForFunction(() => document.body.innerText.includes("stay clear of the printer"), { timeout: WAIT });
  const conversation = await bodyText(page);
  expect.truthy(!conversation.includes("Fire brigade called"), "the internal note stays internal");
  expect.truthy(!conversation.includes("internal note"), "and is not even hinted at");
});

scenario("a customer attaches files to a request, sees the desk's, and takes back only their own", async ({ page }) => {
  const agent = await signUp(page);
  const key = await createProject(page, "Filedesk", "service-desk");
  const customer = await inviteMember(page, "filer", "customer");
  await acceptInvite(page, customer);

  // One file goes with the request, another follows it.
  writeFileSync("/tmp/e2e-portal-a.txt", "The screen went black at 9:12.\n");
  writeFileSync("/tmp/e2e-portal-b.txt", "It came back after a restart.\n");
  const answer = "Hold the power button for ten seconds.\n";
  writeFileSync("/tmp/e2e-agent.txt", answer);
  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The screen went black");
  await queueFiles(page, ["/tmp/e2e-portal-a.txt"]);
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  await page.waitForSelector('[data-attachment="e2e-portal-a.txt"]', { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  expect.truthy(requestKey.startsWith(key + "-"), "the request is the desk's");
  await attachFile(page, "/tmp/e2e-portal-b.txt");

  // The desk sees both, is told the customer sees what it attaches, and answers with a file.
  await signOut(page);
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/issues/${requestKey}`);
  await page.waitForSelector('[data-attachment="e2e-portal-b.txt"]', { timeout: WAIT });
  expect.contains(await textOf(page, "[data-attachments-note]"), "customer sees", "the desk is told the files are shared");
  await attachFile(page, "/tmp/e2e-agent.txt");

  // The customer reads the desk's file and takes back only their own.
  await signOut(page);
  await signIn(page, customer.email);
  await waitForPath(page, "/portal");
  await goto(page, `/portal/requests/${requestKey}`);
  await page.waitForSelector('[data-attachment="e2e-agent.txt"]', { timeout: WAIT });
  expect.equal((await page.$$("[data-attachment]")).length, 3, "every file on the request is listed");
  expect.equal(await page.$('[aria-label="Remove e2e-agent.txt"]'), null, "the desk's file is not theirs to remove");
  const fetched = await page.evaluate(async () => {
    const link = document.querySelector('[data-attachment="e2e-agent.txt"] a');
    const response = await fetch(link.getAttribute("href"));
    return { status: response.status, text: await response.text() };
  });
  expect.equal(fetched.status, 200, "the desk's file downloads");
  expect.equal(fetched.text, answer, "with the bytes the desk sent");
  await page.click('[aria-label="Remove e2e-portal-a.txt"]');
  await confirm(page);
  await page.waitForFunction(() => document.querySelector('[data-attachment="e2e-portal-a.txt"]') === null, { timeout: WAIT });
  expect.equal((await page.$$("[data-attachment]")).length, 2, "and their own file is gone");
});

scenario("a request raised through a template lands with its team", async ({ page }) => {
  const agent = await signUp(page);
  const key = await createProject(page, "Helpdesk", "service-desk");
  await createTeam(page, key, "Field support");

  // The administrator offers a template under a category, routed to a team.
  await goto(page, `/projects/${key}/service-desk`);
  await fill(page, "Request type", "Laptop replacement");
  await fill(page, "Category", "Hardware");
  await selectByLabel(page, "#rt-team", "Field support");
  await page.type("#rt-template", "Device:\nWhat happened:");
  await clickButton(page, "Add request type");
  await page.waitForSelector('[data-request-type="Laptop replacement"]', { timeout: WAIT });
  expect.truthy(await page.$('[data-request-category="Hardware"]'), "the type is listed under its category");
  expect.equal(await textOf(page, '[data-request-type="Laptop replacement"] [data-request-type-team]'), "Field support", "and names its team");

  // A customer finds it under the heading and starts from the template.
  const customer = await inviteMember(page, "customer", "customer");
  await acceptInvite(page, customer);
  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-category="Hardware"] [data-request-type="Laptop replacement"]', { timeout: WAIT });
  await page.click('[data-request-type="Laptop replacement"]');
  await page.waitForFunction(() => document.querySelector("#portal-description")?.value === "Device:\nWhat happened:", { timeout: WAIT });
  await page.type("#portal-description", " the screen cracked");
  await fill(page, "Summary", "Cracked laptop screen");
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  await page.waitForSelector('[data-request-team="Field support"]', { timeout: WAIT });
  expect.contains(await bodyText(page), "Not picked up yet", "the customer sees nobody has it yet");

  // The agent's queue shows where it landed.
  await signOut(page);
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/projects/${key}/queues`);
  await page.waitForSelector(`[data-queue-row="${requestKey}"]`, { timeout: WAIT });
  expect.equal(await textOf(page, `[data-queue-row="${requestKey}"] [data-queue-team]`), "Field support", "the queue names the team");
});

scenario("somebody with only a mail address raises a request and is mailed about it", async ({ page }) => {
  const agent = await signUp(page);
  const key = await createProject(page, "Helpdesk", "service-desk");
  const slug = await orgSlug(page);
  await signOut(page);

  // The door: an address, a code, and in.
  const address = unique("ada").email;
  await enterDesk(page, slug, address);
  await page.waitForFunction(() => document.body.innerText.includes("Your requests"), { timeout: WAIT });
  expect.truthy(!(await bodyText(page)).includes("Workflows"), "a requester lands in the portal");

  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The badge reader is dead");
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  expect.truthy(requestKey.startsWith(key + "-"), "the request is an issue in the desk's project");

  // The receipt names the request and carries the way back.
  const receipt = await mailFor(address, requestKey);
  expect.contains(receipt.subject, "we have your request", "the receipt says so");
  const back = receipt.text.match(/(\/desk\/[^\s]+)/)?.[1];
  expect.truthy(back && back.includes(`/desk/${slug}?next=`), "and links to the door with the request behind it");

  // Following the link later means another code, then the request itself.
  await signOut(page);
  await enterDesk(page, slug, address, `/portal/requests/${requestKey}`);
  await page.waitForFunction(() => document.body.innerText.includes("The badge reader is dead"), { timeout: WAIT });

  // An agent's reply is mailed too.
  await signOut(page);
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/issues/${requestKey}`);
  await page.waitForSelector("#new-comment", { timeout: WAIT });
  await page.type("#new-comment", "A new reader is on its way.");
  await clickButton(page, "Comment");
  const reply = await mailFor(address, "replied");
  expect.contains(reply.text, "A new reader is on its way.", "the reply reaches the address");
});

scenario("a desk that trusts its callers lets one in without a code, and only into itself", async ({ page }) => {
  const owner = await signUp(page);
  const trusting = await createProject(page, "IT desk", "service-desk");
  const careful = await createProject(page, "HR desk", "service-desk");
  const slug = await orgSlug(page);

  // The door starts asking for a code; the owner opens it and reads the address.
  const address = await toggleDoor(page, trusting);
  expect.contains(address, `/desk/${slug}?desk=${trusting}`, "the address names the desk");
  await signOut(page);

  // In with a name and an address, and the portal is this desk alone.
  const who = unique("walkin");
  await enterOpenDesk(page, slug, trusting, who);
  await page.waitForFunction((k) => document.body.innerText.includes(`Your requests at ${k}`), { timeout: WAIT }, trusting);
  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  expect.notContains(await bodyText(page), "HR desk", "the careful desk is not offered");
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The printer is on fire");
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  expect.truthy(requestKey.startsWith(trusting + "-"), "the request is the trusting desk's");
  expect.truthy(!requestKey.startsWith(careful + "-"), "and not the careful one's");

  // The careful desk's door still asks for a code.
  await signOut(page);
  await goto(page, `/desk/${slug}?desk=${careful}`);
  await page.waitForFunction(() => document.body.innerText.includes("Send me a code"), { timeout: WAIT });

  // Turning the code back on closes the trusting desk's door too.
  await signIn(page, owner.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await toggleDoor(page, trusting);
  await signOut(page);
  await goto(page, `/desk/${slug}?desk=${trusting}`);
  await page.waitForFunction(() => document.body.innerText.includes("Send me a code"), { timeout: WAIT });
});

scenario("a desk that trusts a domain turns other addresses away at the door", async ({ page }) => {
  const owner = await signUp(page);
  const key = await createProject(page, "IT desk", "service-desk");
  const slug = await orgSlug(page);
  await toggleDoor(page, key);
  await trustDomain(page, key, "armature.test");
  expect.contains(await bodyText(page), "Empty means any address", "the card says what the list does");
  await signOut(page);

  // Somebody who works here is sent to the code, however open the door.
  const agentRefusal = await turnedAwayAtOpenDesk(page, slug, key, { name: owner.name, email: owner.email });
  expect.contains(agentRefusal, "asks for a code", "an agent's address is asked for a code");

  // An address at another domain is told which domains the desk takes.
  const refusal = await turnedAwayAtOpenDesk(page, slug, key, { name: "Else Where", email: `else-${Date.now().toString(36)}@elsewhere.test` });
  expect.contains(refusal, "armature.test", "the refusal names the trusted domain");

  // One at the trusted domain walks in as before.
  await enterOpenDesk(page, slug, key, unique("walkin"));
  await page.waitForFunction((k) => document.body.innerText.includes(`Your requests at ${k}`), { timeout: WAIT }, key);
});
