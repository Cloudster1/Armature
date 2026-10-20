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

/** A phone held upright, and the same phone on its side, too short to pin a dialog's title and buttons. */
const PHONE = { width: 390, height: 664 };
const PHONE_ON_ITS_SIDE = { width: 740, height: 320 };
/** A phone with its browser's bars and keyboard taking their share, still tall enough to pin. */
const PHONE_BEHIND_BARS = { width: PHONE.width, height: 430 };

/** How many labels a person piles onto a new issue in the report (#294) that grew the dialog past the window. */
const MANY_LABELS = 30;

/** A small turn of the wheel, short of the end of any dialog these scenarios open. */
const NUDGE_PX = 100;

/** How many times Tab is pressed at most before the dialog's last button must have the focus. */
const TAB_LIMIT = 80;

const LAYER = "[data-dialog-backdrop]";

/** How far down the window a dialog reaches with all of its content drawn, however much of it the window shows. */
function fullBottom(page, dialog) {
  return page.$eval(dialog, (el) => {
    const top = el.getBoundingClientRect().top + (el.closest("[data-dialog-backdrop]")?.scrollTop ?? 0);
    return Math.round(top + ["[data-dialog-header]", "[data-dialog-body]", "[data-dialog-footer]"].reduce((sum, part) => sum + (el.querySelector(part)?.scrollHeight ?? 0), 0));
  });
}

/** Whether a press at the middle of an element reaches it, rather than something pinned over it. */
function pressable(page, selector) {
  return page.$eval(selector, (el) => {
    const box = el.getBoundingClientRect();
    const x = box.left + box.width / 2;
    const y = box.top + box.height / 2;
    if (x < 0 || y < 0 || x > window.innerWidth || y > window.innerHeight) return false;
    const hit = document.elementFromPoint(x, y);
    return hit !== null && (hit === el || el.contains(hit));
  });
}

/** How far the dialog's layer and the page behind it are scrolled. */
function scrolled(page) {
  return page.evaluate((layer) => ({
    layer: document.querySelector(layer)?.scrollTop ?? 0,
    page: document.querySelector("main")?.scrollTop ?? 0,
  }), LAYER);
}

/** Turns the wheel once at a point and waits for the dialog's layer to move past where it was. */
async function nudgeAt(page, point, what) {
  const before = await scrolled(page);
  await page.mouse.move(point.x, point.y);
  await page.mouse.wheel({ deltaY: NUDGE_PX });
  await page
    .waitForFunction((layer, was) => (document.querySelector(layer)?.scrollTop ?? 0) > was, { timeout: WAIT }, LAYER, before.layer)
    .catch(() => {
      throw new Error(`a turn of the wheel over ${what} did not scroll the dialog`);
    });
  expect.equal((await scrolled(page)).page, before.page, `a turn of the wheel over ${what} leaves the page behind the dialog where it was`);
}

/** Turns the wheel up over the dialog's layer until it is back at its top. */
async function wheelToTop(page) {
  await page.mouse.move(...(await page.evaluate(() => [window.innerWidth / 2, window.innerHeight / 2])));
  for (let turn = 0; turn < WHEEL_TURNS && (await scrolled(page)).layer > 0; turn++) {
    await page.mouse.wheel({ deltaY: -WHEEL_STEP_PX });
    await page.waitForFunction(() => new Promise((resolve) => requestAnimationFrame(() => resolve(true))), { timeout: WAIT });
  }
  await page.waitForFunction((layer) => document.querySelector(layer).scrollTop === 0, { timeout: WAIT }, LAYER);
}

/** The middle of the title, of the buttons, and of the dimmed page beside the dialog. */
function spots(page, dialog) {
  return page.$eval(dialog, (el) => {
    const middle = (part) => {
      const box = part.getBoundingClientRect();
      return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
    };
    const box = el.getBoundingClientRect();
    return {
      title: middle(el.querySelector("[data-dialog-header] h2")),
      buttons: { x: box.left + 8, y: middle(el.querySelector("[data-dialog-footer]")).y },
      beside: { x: box.left / 2, y: window.innerHeight / 2 },
    };
  });
}

/** Opens New issue on a project and waits for the arrangement's fields. */
async function openNewIssue(page, key) {
  await goto(page, `/projects/${key}`);
  await clickButton(page, "New issue");
  // The built-in arrangement asks for the assignee among much else: it has loaded.
  await page.waitForSelector("[data-create-issue] #field-assignee", { timeout: WAIT });
}

/** Marks the last control of the create form, the one a person scrolls down to. */
function markLastControl(page) {
  return page.evaluate(() => {
    document.querySelector("[data-last-control]")?.removeAttribute("data-last-control");
    const controls = document.querySelectorAll("#create-issue input, #create-issue select, #create-issue textarea, #create-issue button");
    controls[controls.length - 1]?.setAttribute("data-last-control", "");
  });
}

const CREATE = '[data-create-issue] button[type="submit"][form="create-issue"]';

/** Presses Create where it is drawn and waits for the issue to be made. */
async function pressCreate(page) {
  expect.truthy(await pressable(page, CREATE), "a press on Create issue lands on it, not on something drawn over it");
  const box = await page.$eval(CREATE, (el) => {
    const rect = el.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
  });
  await page.mouse.click(box.x, box.y);
  await page.waitForFunction(() => document.body.innerText.includes("Created "), { timeout: WAIT });
  expect.truthy(!(await page.$("[data-create-issue]")), "the dialog closed once the issue was made");
}

// Reported in #294: with a pile of labels the dialog grew past the window and
// neither the page nor the dialog would scroll to Create.
for (const [where, size] of [["a laptop", SHORT_WINDOW], ["a phone", PHONE]]) {
  scenario(`a new issue with many labels on ${where} scrolls like a page wherever the wheel turns, and Create stays in reach`, async ({ page }) => {
    await signUp(page, unique("user"), { visit: false });
    const key = await createProject(page, "Many Labels");
    const labels = Array.from({ length: MANY_LABELS }, (_, i) => uniqueName(`tall-${i}`, "-"));
    await withViewport(page, size, async () => {
      await openNewIssue(page, key);
      await fill(page, "Summary", `made with ${MANY_LABELS} labels on ${where}`);
      await page.click("[data-create-issue] #create-labels");
      for (const label of labels) {
        await page.keyboard.sendCharacter(label);
        await page.keyboard.press("Enter");
      }
      await page.waitForFunction((n) => document.querySelectorAll("[data-create-issue] [data-label]").length === n, { timeout: WAIT }, MANY_LABELS);
      const bottom = await fullBottom(page, "[data-create-issue]");
      expect.truthy(bottom > size.height, `the dialog runs past the bottom of the window (to ${bottom} of ${size.height} pixels)`);

      await wheelToTop(page);
      const at = await spots(page, "[data-create-issue]");
      await nudgeAt(page, at.title, "the title");
      await nudgeAt(page, at.buttons, "the buttons");
      await nudgeAt(page, at.beside, "the page beside the dialog");

      // A press on the layer's own scrollbar scrolls; it is not a press outside the dialog.
      const bar = await page.$eval(LAYER, (el) => ({ width: el.offsetWidth - el.clientWidth, right: el.getBoundingClientRect().right }));
      if (bar.width > 0) {
        await page.mouse.click(bar.right - bar.width / 2, size.height / 2);
        expect.truthy(await page.$("[data-create-issue]"), "a press on the scrollbar leaves the dialog open");
      }

      await markLastControl(page);
      await wheelTo(page, LAYER, "[data-last-control]");
      expect.truthy(await pressable(page, "[data-last-control]"), "the last field, scrolled to, is not hidden under the buttons");
      await pressCreate(page);
    });
  });
}

scenario("a new issue on a phone on its side scrolls its title and buttons along with the form", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Sideways");
  await withViewport(page, PHONE_ON_ITS_SIDE, async () => {
    await openNewIssue(page, key);
    await fill(page, "Summary", "made on a phone on its side");
    // Pinned, the title and the buttons would leave too little window for the form between them.
    const pinned = await page.$eval("[data-create-issue] [data-dialog-footer]", (el) => getComputedStyle(el).position);
    expect.equal(pinned, "static", "the buttons scroll with the form on a window this short");
    await markLastControl(page);
    await wheelTo(page, LAYER, "[data-last-control]");
    await wheelTo(page, LAYER, CREATE);
    await pressCreate(page);
  });
});

scenario("tabbing through a tall new issue dialog never leaves the focus under its pinned title or buttons", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Tabbing");
  await withViewport(page, SHORT_WINDOW, async () => {
    await openNewIssue(page, key);
    await fill(page, "Summary", "made from the keyboard");
    let onCreate = false;
    for (let press = 0; press < TAB_LIMIT && !onCreate; press++) {
      await page.keyboard.press("Tab");
      await page.waitForFunction(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve(true)))), { timeout: WAIT });
      const focus = await page.evaluate((create) => {
        const el = document.activeElement;
        const dialog = document.querySelector("[data-create-issue]");
        const header = dialog.querySelector("[data-dialog-header]").getBoundingClientRect();
        const footer = dialog.querySelector("[data-dialog-footer]").getBoundingClientRect();
        const box = el.getBoundingClientRect();
        const pinnedPart = el.closest("[data-dialog-header], [data-dialog-footer]") !== null;
        return {
          name: el.id || el.getAttribute("aria-label") || el.textContent.trim().slice(0, 40),
          inside: dialog.contains(el),
          clear: pinnedPart || (box.top >= header.bottom - 1 && box.bottom <= footer.top + 1),
          create: el.matches(create),
        };
      }, CREATE);
      expect.truthy(focus.inside, `Tab keeps the focus inside the dialog (it went to ${focus.name})`);
      expect.truthy(focus.clear, `${focus.name} is in view between the title and the buttons once Tab reaches it`);
      onCreate = focus.create;
    }
    expect.truthy(onCreate, `Tab reaches Create issue within ${TAB_LIMIT} presses`);
    await page.keyboard.press("Enter");
    await page.waitForFunction(() => document.body.innerText.includes("Created "), { timeout: WAIT });
  });
});

scenario("the export dialog on a phone scrolls by the wheel over its description and keeps Download in reach", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const key = await createProject(page, "Tall Export", { visit: false });
  await withViewport(page, PHONE_BEHIND_BARS, async () => {
    await goto(page, `/search?q=${encodeURIComponent(`project = ${key}`)}`);
    await page.waitForSelector('[data-action="export-csv"]', { timeout: WAIT });
    await page.click('[data-action="export-csv"]');
    await page.waitForSelector("[data-export-dialog]", { timeout: WAIT });
    const bottom = await fullBottom(page, "[data-export-dialog]");
    expect.truthy(bottom > PHONE_BEHIND_BARS.height, `the export dialog runs past the bottom of the window (to ${bottom} of ${PHONE_BEHIND_BARS.height} pixels)`);

    const description = await page.$eval("[data-export-dialog] [data-dialog-header] p", (el) => {
      const box = el.getBoundingClientRect();
      return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
    });
    await nudgeAt(page, description, "the description");

    const last = '[data-export-column="parent"]';
    await wheelTo(page, LAYER, last);
    expect.truthy(await pressable(page, last), "the last column is not hidden under the buttons");
    await page.click(last);
    expect.truthy(await page.$eval(last, (el) => el.checked), "the last column is ticked");
    expect.truthy(await pressable(page, '[data-action="download-csv"]'), "a press on Download lands on it");
    const href = await page.$eval('[data-action="download-csv"]', (a) => a.getAttribute("href"));
    expect.contains(href, "parent", "the file asks for the column ticked at the end of the list");
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


scenario("the project switcher's filter takes a name of two words", async ({ page }) => {
  await signUp(page, unique("user"), { visit: false });
  const name = `${uniqueName("Switchable", "-")} Portal`;
  const key = await createProject(page, name, { visit: false });
  try {
    await goto(page, `/projects/${key}`);
    await page.click("[data-project-switcher]");
    await page.waitForSelector('[aria-label="Filter projects"]', { timeout: WAIT });
    await page.type('[aria-label="Filter projects"]', name);
    await page.waitForSelector(`[data-project-option="${key}"]`, { timeout: WAIT });
    expect.truthy(await page.$('[role="menu"][aria-label="Switch project"]'), "the menu stays open after the space");
    expect.equal(
      await page.$eval('[aria-label="Filter projects"]', (el) => el.value),
      name,
      "the box holds the whole name",
    );
  } finally {
    await archiveProjects(page, [key]);
  }
});
