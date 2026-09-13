// Commits, branches and pull requests coming back from the hosts.

import { createHmac } from "node:crypto";
import { scenario } from "../runner.mjs";
import {
  bodyText,
  clickButton,
  createIssue,
  createProject,
  eventually,
  expect,
  fill,
  gitea,
  GITEA_URL,
  giteaRepository,
  goto,
  selectByLabel,
  signUp,
  textOf,
  WAIT,
} from "../helpers.mjs";

scenario("a commit that names an issue appears on it and moves it", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Wired");
  const issueKey = await createIssue(page, key, "wire the webhook", "Task");

  // Connecting hands out the address and the secret exactly once.
  await goto(page, `/projects/${key}/repositories`);
  await clickButton(page, "Connect repository");
  await fill(page, "Repository", "acme/portal");
  await clickButton(page, "Connect");
  await page.waitForSelector('[data-testid="webhook-secret"]', { timeout: WAIT });
  const secret = await textOf(page, '[data-testid="webhook-secret"]');
  const webhookUrl = await textOf(page, '[data-testid="webhook-url"]');
  expect.truthy(secret.length >= 32, "a secret worth the name");
  await clickButton(page, "I have copied it");
  expect.contains(await bodyText(page), "read only", "without a token the tracker only listens");

  // The host's delivery, signed the way GitHub signs it. The signature is made
  // here rather than in the page, which is served over plain http and so has
  // no SubtleCrypto; the delivery itself still crosses the same proxy a real
  // one would.
  const body = JSON.stringify({
    ref: "refs/heads/" + issueKey + "-wire",
    created: true,
    commits: [{
      id: "abc1234def5678",
      message: issueKey + " #start-progress wire it up",
      url: "https://github.com/acme/portal/commit/abc1234def5678",
      timestamp: "2026-09-01T10:00:00Z",
      author: { name: "Somebody", email: "somebody@elsewhere.example" },
    }],
  });
  const signature = "sha256=" + createHmac("sha256", secret).update(body).digest("hex");
  const receipt = await page.evaluate(
    async (path, body, signature) => {
      const response = await fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-GitHub-Event": "push", "X-Hub-Signature-256": signature },
        body,
      });
      return { status: response.status, body: await response.json() };
    },
    new URL(webhookUrl).pathname,
    body,
    signature,
  );
  expect.equal(receipt.status, 200, `the delivery was accepted: ${JSON.stringify(receipt.body)}`);
  expect.equal(receipt.body.transitions.join(","), `${issueKey}: Start progress`, "the commit's command was carried out");

  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector('[data-commit="abc1234def5678"]', { timeout: WAIT });
  const detail = await bodyText(page);
  expect.contains(detail, "wire it up", "the commit is on the issue");
  expect.contains(detail, issueKey + "-wire", "and so is the branch named for it");
  expect.contains(detail, "In Progress", "the issue moved");
  expect.contains(detail, "moved this from To Do to In Progress", "and the changelog says so");

  await goto(page, `/projects/${key}/repositories`);
  await page.waitForSelector('[data-repository="acme/portal"]', { timeout: WAIT });
  expect.contains(await bodyText(page), "1 commit", "the repository counts what it has sent");
});

scenario("a branch made from a ticket is a real branch, and what happens to it comes back to the ticket", async ({ page }) => {
  await signUp(page);
  const key = await createProject(page, "Branchy");
  const issueKey = await createIssue(page, key, "wire the portal", "Task");
  const repo = await giteaRepository("e2e");
  const second = await giteaRepository("e2e-api");

  try {
    // The connection: a repository on the stack's own git host, with a token
    // so the tracker can write to it. And a second one, because the work on
    // one ticket often lands in more than one repository.
    await goto(page, `/projects/${key}/repositories`);
    await clickButton(page, "Connect repository");
    await selectByLabel(page, "#repo-host", "Gitea");
    await fill(page, "Repository", repo.fullName);
    await fill(page, "API address", `${GITEA_URL}/api/v1`);
    await fill(page, "Access token", repo.token);
    await clickButton(page, "Connect");
    await page.waitForSelector('[data-testid="webhook-secret"]', { timeout: WAIT });
    const secret = await textOf(page, '[data-testid="webhook-secret"]');
    const webhookUrl = await textOf(page, '[data-testid="webhook-url"]');
    await clickButton(page, "I have copied it");
    expect.contains(await bodyText(page), "read and write", "with a token the tracker can reach back");

    await clickButton(page, "Connect repository");
    await selectByLabel(page, "#repo-host", "Gitea");
    await fill(page, "Repository", second.fullName);
    await fill(page, "API address", `${GITEA_URL}/api/v1`);
    await fill(page, "Access token", second.token);
    await clickButton(page, "Connect");
    await page.waitForSelector('[data-testid="webhook-secret"]', { timeout: WAIT });
    await clickButton(page, "I have copied it");

    // The other half of the connection, made where a person would make it: the
    // host is told where to deliver, at the address the api has inside the stack.
    await gitea("POST", `/repos/${repo.fullName}/hooks`, {
      type: "gitea",
      active: true,
      events: ["push", "create", "delete", "pull_request"],
      config: { url: `http://api:8080${new URL(webhookUrl).pathname}`, content_type: "json", secret },
    });

    // The branch is named for the ticket before it exists, and then exists.
    await goto(page, `/issues/${issueKey}`);
    await clickButton(page, "Create branch");
    const suggested = await page.$eval("#branch-name", (el) => el.value);
    expect.equal(suggested, `${issueKey}-wire-the-portal`, "the branch is named for the ticket");
    expect.equal(await page.$eval("#branch-from", (el) => el.value), "main", "and starts from the default branch");
    await selectByLabel(page, "#branch-repository", repo.fullName);
    await clickButton(page, "Create");
    await page.waitForSelector(`[data-branch="${suggested}"]`, { timeout: WAIT });
    expect.contains(await textOf(page, '[data-testid="checkout-command"]'), `git switch ${suggested}`, "with the command to start on it");
    const onHost = await gitea("GET", `/repos/${repo.fullName}/branches/${suggested}`);
    expect.equal(onHost.name, suggested, "the branch is really on the host");

    // The form stays open and has moved on to the repository without a
    // branch yet, so the same ticket gets a branch in the second one too.
    expect.equal(
      await page.$eval("#branch-repository", (el) => el.options[el.selectedIndex].textContent.trim()),
      second.fullName,
      "the next repository is offered",
    );
    await clickButton(page, "Create");
    await page.waitForFunction(
      (name) => document.querySelectorAll(`[data-branch="${name}"]`).length === 2,
      { timeout: WAIT },
      suggested,
    );
    const onSecond = await gitea("GET", `/repos/${second.fullName}/branches/${suggested}`);
    expect.equal(onSecond.name, suggested, "and that branch is on its host too");
    await clickButton(page, "Done");

    // Work on the branch reaches the ticket without naming it: the branch is
    // the ticket's, so its commits are.
    await gitea("POST", `/repos/${repo.fullName}/contents/wiring.txt`, {
      content: Buffer.from("wired\n").toString("base64"),
      branch: suggested,
      message: "wire it up",
    });
    await eventually(page, "[data-commit]");
    let detail = await bodyText(page);
    expect.contains(detail, "wire it up", "the commit is on the ticket");
    expect.contains(detail, "1 commit", "and the branch counts it");
    const head = await page.$eval(
      `[data-branch="${suggested}"][data-branch-repository="${repo.fullName}"] [data-branch-head]`,
      (el) => el.getAttribute("data-branch-head"),
    );
    expect.equal(head, (await gitea("GET", `/repos/${repo.fullName}/branches/${suggested}`)).commit.id, "the branch's head is the host's");

    // The pull request from the branch is the ticket's too, and merging it
    // merges the branch.
    const pull = await gitea("POST", `/repos/${repo.fullName}/pulls`, { head: suggested, base: "main", title: "Wire the portal" });
    await eventually(page, `[data-pull-request="${pull.number}"]`);
    expect.contains(await bodyText(page), "Wire the portal", "the pull request is on the ticket");
    await gitea("POST", `/repos/${repo.fullName}/pulls/${pull.number}/merge`, { Do: "merge" });
    await eventually(page, `[data-branch="${suggested}"][data-branch-merged="true"]`);
    detail = await bodyText(page);
    expect.contains(detail, "merged", "and the merge is on the ticket");
  } finally {
    await repo.remove();
    await second.remove();
  }
});
