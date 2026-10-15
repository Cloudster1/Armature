// Overlays on a window shorter than they are, reached as a person reaches
// them: by the wheel and a press where they are drawn, not through the DOM.

import { scenario } from "../runner.mjs";
import {
  archiveProjects,
  clickButton,
  createProject,
  createProjects,
  expect,
  fill,
  goto,
  signUp,
  unique,
  uniqueName,
  WAIT,
  withViewport,
} from "../helpers.mjs";

/** A laptop window with the browser's bars taking their share. */
const SHORT_WINDOW = { width: 1280, height: 600 };

/** How many projects the switcher lists, as PROJECT_PICKER_RESULTS in web/src/config.ts has it. */
const SWITCHER_RESULTS = 20;

/** How far one turn of the wheel scrolls, and how many turns a person gives it. */
const WHEEL_STEP_PX = 400;
const WHEEL_TURNS = 12;

/** Whether all of an element is inside the window, so a press on it lands. */
function inWindow(page, selector) {
  return page.$eval(selector, (el) => {
    const box = el.getBoundingClientRect();
    return box.height > 0 && box.top >= 0 && box.bottom <= window.innerHeight && box.left >= 0 && box.right <= window.innerWidth;
  });
}

/** Turns the wheel over the box of within until target is in the window, as a person scrolls to it. */
async function wheelTo(page, within, target) {
  const box = await page.$eval(within, (el) => {
    const rect = el.getBoundingClientRect();
    const top = Math.max(rect.top, 0);
    const bottom = Math.min(rect.bottom, window.innerHeight);
    return { x: rect.left + rect.width / 2, y: (top + bottom) / 2 };
  });
  await page.mouse.move(box.x, box.y);
  for (let turn = 0; turn < WHEEL_TURNS && !(await inWindow(page, target)); turn++) {
    await page.mouse.wheel({ deltaY: WHEEL_STEP_PX });
    await page.waitForFunction(() => new Promise((resolve) => requestAnimationFrame(() => resolve(true))), { timeout: WAIT });
  }
  expect.truthy(await inWindow(page, target), `scrolling over ${within} never brought ${target} into the window`);
}

scenario("the new issue dialog on a short window keeps Create in reach and scrolls to its last field", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Short Window");
  await withViewport(page, SHORT_WINDOW, async () => {
    await goto(page, `/projects/${key}`);
    await clickButton(page, "New issue");
    // The built-in arrangement asks for the assignee among much else: it has loaded.
    await page.waitForSelector("[data-create-issue] #field-assignee", { timeout: WAIT });
    await fill(page, "Summary", "made on a short window");

    const create = '[data-create-issue] button[type="submit"][form="create-issue"]';
    expect.truthy(await inWindow(page, create), "the Create issue button is inside a 600 pixel window without scrolling");

    // The body scrolls under a footer that stays put.
    await page.evaluate(() => {
      const controls = document.querySelectorAll("#create-issue input, #create-issue select, #create-issue textarea, #create-issue button");
      controls[controls.length - 1]?.setAttribute("data-last-control", "");
    });
    await wheelTo(page, "#create-issue", "[data-last-control]");
    expect.truthy(await inWindow(page, create), "the Create issue button is still in the window once the body is scrolled");

    await page.click(create);
    await page.waitForFunction(() => document.body.innerText.includes("Created "), { timeout: WAIT });
    expect.truthy(!(await page.$("[data-create-issue]")), "the dialog closed once the issue was made");
  });
});

scenario("the project switcher on a short window scrolls to its last project", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const marker = uniqueName("Switchable", "-");
  const keys = await createProjects(page, marker, SWITCHER_RESULTS);
  try {
    await withViewport(page, SHORT_WINDOW, async () => {
      await goto(page, `/projects/${keys[0]}`);
      await page.click("[data-project-switcher]");
      await page.waitForSelector('[aria-label="Filter projects"]', { timeout: WAIT });
      await page.type('[aria-label="Filter projects"]', marker);
      await page.waitForFunction(
        (wanted) => {
          const listed = [...document.querySelectorAll("[data-project-option]")].map((el) => el.getAttribute("data-project-option"));
          return listed.length === wanted.length && listed.every((key) => wanted.includes(key));
        },
        { timeout: WAIT },
        keys,
      );

      const last = await page.evaluate(() => {
        const options = document.querySelectorAll("[data-project-option]");
        return options[options.length - 1].getAttribute("data-project-option");
      });
      const option = `[data-project-option="${last}"]`;
      await wheelTo(page, '[role="menu"][aria-label="Switch project"]', option);
      await page.click(option);
      await page.waitForFunction((k) => location.pathname === `/projects/${k}`, { timeout: WAIT }, last);
    });
  } finally {
    await archiveProjects(page, keys);
  }
});

