// Deleting the organization is for its owner alone; the deletion itself goes
// with organizations (#213), and a run must not delete the installation it runs in.

import { scenario } from "../runner.mjs";
import {
  expect,
  goto,
  signUp,
  unique,
  WAIT,
} from "../helpers.mjs";

scenario("an administrator who does not own the organization is not offered deleting it", async ({ page }) => {
  // The run's people are administrators; the run's administrator owns it.
  await signUp(page, unique("notowner"), { visit: false });

  await goto(page, "/settings/organization");
  await page.waitForFunction(() => document.body.innerText.includes("Only an owner of the organization can delete it."), { timeout: WAIT });
  expect.equal(await page.$('[data-action="delete-organization"]'), null, "no delete button");
});
