// The desk by mail: followers, replies, articles, ratings and a person's profile.

import { writeFileSync } from "node:fs";
import { scenario } from "../runner.mjs";
import {
  acceptInvite,
  articlesOffered,
  bodyText,
  clickButton,
  createProject,
  enterDesk,
  expect,
  fill,
  goto,
  inviteMember,
  mailFor,
  orgSlug,
  rateRequest,
  ratingLinkFor,
  selectByLabel,
  sendMail,
  signIn,
  signOut,
  signUp,
  textOf,
  unique,
  WAIT,
  editorText,
  waitForApp,
  waitForPath,
  writeArticle,
  writeCannedResponse,
} from "../helpers.mjs";

scenario("a request is followed by somebody who was only named", async ({ page }) => {
  const agent = await signUp(page);
  await createProject(page, "Helpdesk", "service-desk");
  const slug = await orgSlug(page);
  await signOut(page);

  // The requester names a follower by address on their request.
  const address = unique("ada").email;
  const follower = unique("ben").email;
  await enterDesk(page, slug, address);
  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The lift is stuck");
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  const since = Date.now() - 1000;
  await page.waitForSelector("[data-follower-address]", { timeout: WAIT });
  await page.type("[data-follower-address]", follower);
  await clickButton(page, "Add follower");
  await page.waitForSelector(`[data-request-watcher="${follower}"]`, { timeout: WAIT });

  // The follower is told, and the mail carries the way out.
  const told = await mailFor(follower, requestKey, { since });
  expect.contains(told.text, "added you", "the mail says who did it");
  const stop = told.text.match(/(\/unwatch\?token=[^\s]+)/)?.[1];
  expect.truthy(stop, "and carries a stop link");
  await goto(page, stop);
  await clickButton(page, "Stop following");
  await page.waitForSelector("[data-unwatched]", { timeout: WAIT });

  // The agent's panel agrees, and the agent can watch it themselves.
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/issues/${requestKey}`);
  await page.waitForSelector('[data-testid="watchers-panel"]', { timeout: WAIT });
  expect.truthy(!(await bodyText(page)).includes(follower), "the follower who stopped is gone");
  await page.click('[data-action="watch"]');
  await page.waitForSelector(`[data-issue-watcher="${agent.email}"]`, { timeout: WAIT });
});

scenario("a reply by mail lands on the request", async ({ page }) => {
  const agent = await signUp(page);
  await createProject(page, "Helpdesk", "service-desk");
  const slug = await orgSlug(page);
  await signOut(page);

  const address = unique("ada").email;
  await enterDesk(page, slug, address);
  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The coffee machine leaks");
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());
  await page.waitForFunction(() => document.body.innerText.includes("The coffee machine leaks"), { timeout: WAIT });
  expect.contains(await bodyText(page), "answer the mail", "the portal says a mail reply works too");

  // The agent replies; the mail carries a thread id to answer.
  await signOut(page);
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/issues/${requestKey}`);
  await page.waitForSelector("#new-comment", { timeout: WAIT });
  await page.type("#new-comment", "Is it the tray or the tank?");
  await clickButton(page, "Comment");
  const reply = await mailFor(address, "replied");
  expect.truthy(reply.messageId.startsWith(requestKey + "."), "the mail's id names the request");

  // The requester answers from their mail client, quoting the desk's words.
  await sendMail({
    from: address,
    to: "support@armature.test",
    subject: reply.subject.startsWith("Re:") ? reply.subject : `Re: ${reply.subject}`,
    inReplyTo: reply.messageId,
    body: "The tank, and it is getting worse.\n\nOn Monday Armature wrote:\n> Is it the tray or the tank?",
  });
  // The worker reads the box every few seconds; the page is reloaded until
  // the words are on it, since an open issue page does not poll.
  const deadline = Date.now() + 40_000;
  while (!(await bodyText(page)).includes("The tank, and it is getting worse.")) {
    if (Date.now() > deadline) throw new Error("the mailed reply never reached the request");
    await new Promise((resolve) => setTimeout(resolve, 3000));
    await goto(page, `/issues/${requestKey}`);
  }
  const thread = await bodyText(page);
  expect.truthy(!thread.includes("> Is it the tray"), "the quoted words stay out");
  expect.truthy(!thread.includes("internal note"), "a mailed reply is public");
});

scenario("a person changes their name and time zone and puts a face to it", async ({ page }) => {
  await signUp(page);
  await goto(page, "/settings/profile");
  await page.waitForSelector("#field-name", { timeout: WAIT });
  await fill(page, "Name", "Ada Lovelace");
  await selectByLabel(page, "#field-time-zone", "Europe/Berlin");
  await selectByLabel(page, "#field-language", "Deutsch (Deutschland)");
  await clickButton(page, "Save changes");
  await page.waitForFunction(() => document.body.innerText.includes("Profile saved"), { timeout: WAIT });
  await goto(page, "/settings/profile");
  await page.waitForFunction(() => document.querySelector("#field-name")?.value === "Ada Lovelace", { timeout: WAIT });
  expect.contains(await textOf(page, "[data-format-preview]"), ".2026", "dates are written the German way");

  // A picture made in the browser goes up and is shown in the foot.
  const input = await page.$("[data-avatar-input]");
  await page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = 32;
    canvas.height = 32;
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "#2f5fd0";
    ctx.fillRect(0, 0, 32, 32);
    window.__pictureDataUrl = canvas.toDataURL("image/png");
  });
  const dataUrl = await page.evaluate(() => window.__pictureDataUrl);
  const bytes = Buffer.from(dataUrl.split(",")[1], "base64");
  const dir = process.env.E2E_ARTIFACTS ?? "/tmp";
  const path = `${dir}/face-${Date.now()}.png`;
  writeFileSync(path, bytes);
  await input.uploadFile(path);
  await page.waitForFunction(() => document.body.innerText.includes("Picture changed"), { timeout: WAIT });
  await page.waitForSelector('aside img[alt="Ada Lovelace"]', { timeout: WAIT });
  const src = await page.$eval('aside img[alt="Ada Lovelace"]', (el) => el.getAttribute("src"));
  expect.truthy(src.includes("/api/v1/users/") && src.includes("/avatar?v="), "the foot shows the picture from the API");

  await clickButton(page, "Remove");
  await page.waitForFunction(() => document.body.innerText.includes("Picture removed"), { timeout: WAIT });
  await page.waitForFunction(() => !document.querySelector('aside img[alt="Ada Lovelace"]'), { timeout: WAIT });
});

scenario("a customer finds an article before raising a request", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Helpdesk", "service-desk");

  // The desk writes two articles and publishes one; the draft stays its own.
  await writeArticle(page, key, "Resetting your password", "Open the sign-in page, choose Forgot password and follow the mail. The link works for an hour.");
  await writeArticle(page, key, "Replacing a badge", "Ask at reception with a photo id.", { publish: false });

  // A customer at the door is offered what was published, and only that.
  const customer = await inviteMember(page, "customer", "customer");
  await acceptInvite(page, customer);
  await goto(page, "/portal/new");
  await page.waitForSelector("[data-article-search] [data-article]", { timeout: WAIT });
  const offered = await page.$$eval("[data-article-search] [data-article]", (els) => els.map((el) => el.getAttribute("data-article")));
  expect.truthy(offered.includes("Resetting your password"), "the published article is offered before the request types");
  expect.truthy(!offered.includes("Replacing a badge"), "the draft is not");

  // A word from the body finds it, and the article reads in full.
  const found = await articlesOffered(page, "forgot");
  expect.equal(found.join(","), "Resetting your password", "a word from the body finds the article");
  await page.click('[data-article="Resetting your password"]');
  await page.waitForSelector('[data-portal-article="Resetting your password"]', { timeout: WAIT });
  expect.contains(await bodyText(page), "The link works for an hour", "and the article reads in full");
});

scenario("a resolved request is rated from the mail and the queue shows the score", async ({ page }) => {
  const agent = await signUp(page);
  const key = await createProject(page, "Helpdesk", "service-desk");
  await writeCannedResponse(page, key, "Wrap up", "Hello {{customer.name}}, {{issue.key}} is sorted. Thanks for telling us.");

  const customer = await inviteMember(page, "customer", "customer");
  await acceptInvite(page, customer);
  await goto(page, "/portal/new");
  await page.waitForSelector('[data-request-type="Report a problem"]', { timeout: WAIT });
  await page.click('[data-request-type="Report a problem"]');
  await fill(page, "Summary", "The coffee machine leaks");
  await clickButton(page, "Send request");
  await page.waitForFunction(() => location.pathname.startsWith("/portal/requests/"), { timeout: WAIT });
  const requestKey = await page.evaluate(() => location.pathname.split("/").pop());

  // The agent answers with a canned response, filled in for this customer, and resolves.
  await signOut(page);
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/issues/${requestKey}`);
  await page.waitForSelector('[data-action="canned-responses"]', { timeout: WAIT });
  await page.click('[data-action="canned-responses"]');
  await page.waitForSelector('[data-canned-option="Wrap up"]', { timeout: WAIT });
  await page.click('[data-canned-option="Wrap up"]');
  await page.waitForFunction((k) => document.querySelector("#new-comment")?.textContent.includes(`${k} is sorted`), { timeout: WAIT }, requestKey);
  expect.contains(await editorText(page, "#new-comment"), `Hello ${customer.name},`, "the placeholders are filled in for this customer");
  await clickButton(page, "Comment");
  await page.waitForFunction(() => document.body.innerText.includes("is sorted. Thanks for telling us."), { timeout: WAIT });
  const before = Date.now() - 1000;
  await clickButton(page, "Resolve");
  await page.waitForFunction(() => document.body.innerText.includes("Reopen"), { timeout: WAIT });

  // The resolution mail carries a one-click rating, which needs no sign-in.
  const path = await ratingLinkFor(customer.email, requestKey, { since: before });
  await signOut(page);
  await rateRequest(page, path, 5, "Quick and kind.");
  expect.contains(await bodyText(page), "Thank you", "the requester is thanked");

  // The queue shows the score beside the request.
  await signIn(page, agent.email);
  await waitForPath(page, "/");
  await waitForApp(page);
  await goto(page, `/projects/${key}/queues`);
  await clickButton(page, "All");
  await page.waitForSelector(`[data-queue-row="${requestKey}"] [data-queue-csat="5"]`, { timeout: WAIT });
  expect.contains(await textOf(page, `[data-queue-row="${requestKey}"] [data-queue-csat]`), "5 / 5", "the queue shows the score");
});
