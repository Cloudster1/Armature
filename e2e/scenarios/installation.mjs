// The installation's own page: what it is called, and the demo an
// administrator may fill it with. Both are everybody's, so both are serial.

import { serialScenario } from "../runner.mjs";
import {
  clickButton,
  DEMO_PROJECT_KEY,
  demoProjectAdded,
  expect,
  goto,
  keepInstallationName,
  replaceValue,
  signUp,
  suiteDemo,
  unique,
  uniqueName,
  WAIT,
  waitForPath,
} from "../helpers.mjs";

/** The sentence a demo refused for its taken key ends with. */
const DEMO_IS_HERE = "it is under Projects as Production line 3";

/** The worker builds the suite demo's hundreds of issues in seconds; a busy runner gets this long. */
const SUITE_DEMO_WAIT = 180_000;

serialScenario("renaming the installation shows in the sidebar, and the old settings address leads there", async ({ page }) => {
  await signUp(page, unique("namer"), { visit: false });
  const putBack = await keepInstallationName();
  const name = uniqueName("Plant");
  try {
    await goto(page, "/settings/organization");
    await waitForPath(page, "/admin/installation");
    await page.waitForSelector("#field-installation-name", { timeout: WAIT });
    expect.equal(await page.$('[data-action="delete-organization"]'), null, "nothing deletes the installation");

    await replaceValue(page, "#field-installation-name", name);
    const renamed = page.waitForResponse((r) => r.url().endsWith("/api/v1/installation") && r.request().method() === "PUT", { timeout: WAIT });
    await clickButton(page, "Rename");
    expect.equal((await renamed).status(), 200, "the rename is saved");
    // The settings area heads its sidebar with the way back; the work's sidebar carries the name.
    await goto(page, "/");
    await page.waitForFunction((n) => document.querySelector("[data-installation-name]")?.textContent.trim() === n, { timeout: WAIT }, name);
  } finally {
    await putBack();
  }
});

// A second run on the same database finds the demo there: its key is taken for
// good, since a project is archived rather than deleted. Then only the refusal is left to see.
serialScenario("an administrator adds the demo project, and a second time is refused with where it is", async ({ page }) => {
  await signUp(page, unique("demo"), { visit: false });
  if (!(await demoProjectAdded())) {
    await goto(page, "/admin/installation");
    await clickButton(page, "Add the demo project");
    await waitForPath(page, `/projects/${DEMO_PROJECT_KEY}`);
    await page.waitForFunction(() => document.body.innerText.includes("Welding cell 2 stops intermittently"), { timeout: WAIT });
  }

  await goto(page, "/admin/installation");
  await clickButton(page, "Add the demo project");
  await page.waitForFunction((words) => document.body.innerText.includes(words), { timeout: WAIT }, DEMO_IS_HERE);
});

// Exciter's issue 261 is EXC-261, and what its GitHub issue depends on blocks it here. A second run finds the
// suite demo there, from the first or from make seed, and only looks.
serialScenario("an administrator adds the suite demo, and EXC-261 is Exciter's issue 261 with what blocks it", async ({ page }) => {
  await signUp(page, unique("suite"), { visit: false });
  await goto(page, "/admin/installation");
  if (!(await suiteDemo())) {
    await clickButton(page, "Add the suite demo");
  }
  await page.waitForSelector('[data-suite-demo="done"]', { timeout: SUITE_DEMO_WAIT });
  for (const key of ["ARM", "EXC", "STA"]) {
    await page.waitForSelector(`[data-suite-demo="done"] a[href="/projects/${key}"]`, { timeout: WAIT });
  }

  await goto(page, "/issues/EXC-261");
  await page.waitForFunction(() => document.body.innerText.includes("Risks in traceability"), { timeout: WAIT });
  await page.waitForSelector('[data-issue-link="EXC-203"]', { timeout: WAIT });
  expect.contains(await page.$eval('[data-issue-link="EXC-203"]', (el) => el.innerText), "blocked by", "the issue says what blocks it");
});
