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
  unique,
  uniqueName,
  WAIT,
  waitForPath,
} from "../helpers.mjs";

/** The sentence a demo refused for its taken key ends with. */
const DEMO_IS_HERE = "it is under Projects as Production line 3";

serialScenario("renaming the installation shows in the sidebar, and the old settings address leads there", async ({ page }) => {
  await signUp(page, unique("namer"), { visit: false });
  const putBack = await keepInstallationName();
  const name = uniqueName("Plant");
  try {
    await goto(page, "/settings/organization");
    await waitForPath(page, "/settings/installation");
    await page.waitForSelector("#field-installation-name", { timeout: WAIT });
    expect.equal(await page.$('[data-action="delete-organization"]'), null, "nothing deletes the installation");

    await replaceValue(page, "#field-installation-name", name);
    await clickButton(page, "Rename");
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
    await goto(page, "/settings/installation");
    await clickButton(page, "Add the demo project");
    await waitForPath(page, `/projects/${DEMO_PROJECT_KEY}`);
    await page.waitForFunction(() => document.body.innerText.includes("Welding cell 2 stops intermittently"), { timeout: WAIT });
  }

  await goto(page, "/settings/installation");
  await clickButton(page, "Add the demo project");
  await page.waitForFunction((words) => document.body.innerText.includes(words), { timeout: WAIT }, DEMO_IS_HERE);
});
