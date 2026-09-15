import net from "node:net";
import { basename } from "node:path";
import { expect } from "./runner.mjs";

export const BASE_URL = process.env.E2E_BASE_URL ?? "http://web:5173";

/** A password long enough to satisfy the minimum length rule. */
export const PASSWORD = "an entirely adequate password";

/** How long any one thing on the page is waited for before the step gives up. */
export const WAIT = 15_000;

/** A full page load fetches every module from the dev server; with several browsers at it, longer. */
export const LOAD_WAIT = 30_000;

/** A day in milliseconds, for the dates the scenarios ask for by offset. */
const DAY_MS = 86_400_000;

/** A date this many days from now, written the way a date field reads it. */
export function day(offset) {
  return new Date(Date.now() + offset * DAY_MS).toISOString().slice(0, 10);
}

/** Returns identifiers that will not collide with other runs. */
export function unique(prefix) {
  const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
  return {
    email: `${prefix}-${suffix}@armature.test`,
    org: `${prefix} ${suffix}`,
    name: `${prefix} tester`,
  };
}

// Waits for the app's own data-settled rather than network silence, which is
// only known half a second after the fact.
export async function goto(page, path) {
  await leaving(page);
  await page.goto(`${BASE_URL}${path}`, { waitUntil: "domcontentloaded", timeout: LOAD_WAIT });
  await settled(page);
}

// A page left mid-action may navigate on its own as the action lands, and a
// load issued at that moment is one Chromium sometimes never reports finished.
async function leaving(page) {
  const busy = await page.evaluate(() => document.body?.dataset.settled === "false").catch(() => false);
  if (busy) await settled(page, WAIT).catch(() => {});
}

/** Reloads the page the way goto loads one. */
export async function reload(page) {
  await page.reload({ waitUntil: "domcontentloaded", timeout: LOAD_WAIT });
  await settled(page);
}

/** Frames data-settled has to hold: a query's answer and the render that asks the next are not one step. */
const SETTLED_FRAMES = 3;

/** Frames one in-page wait may run before it is asked again, so no promise outlives its context. */
const SETTLED_PATIENCE_FRAMES = 60;

/** Waits for the page to have nothing in flight, be it a route or a query, and to stay that way. */
export async function settled(page, timeout = LOAD_WAIT) {
  await page.waitForFunction(
    (frames, patience) =>
      new Promise((resolve) => {
        let quiet = 0;
        let waited = 0;
        const tick = () => {
          quiet = document.body.dataset.settled === "true" ? quiet + 1 : 0;
          if (quiet >= frames) return resolve(true);
          if (++waited >= patience) return resolve(false);
          requestAnimationFrame(tick);
        };
        tick();
      }),
    { timeout },
    SETTLED_FRAMES,
    SETTLED_PATIENCE_FRAMES,
  );
}

/** Fills a field by its visible label, the way a person finds it. */
export async function fill(page, label, value) {
  const id = `#field-${label.toLowerCase().replace(/\s+/g, "-")}`;
  await page.waitForSelector(id, { timeout: WAIT });
  await page.click(id, { clickCount: 3 });
  // Pasted in one piece rather than typed: a key event per character was a
  // round trip each, and a form field does not care how its text arrived.
  if (value === "") await page.keyboard.press("Backspace");
  else await page.keyboard.sendCharacter(value);
}

export async function submit(page) {
  await page.click("button[type=submit]");
}

/**
 * Makes an API token on the settings page and returns the secret the page
 * shows once; readOnly ticks the box that makes it a token that cannot write,
 * and projects confines it to those project keys.
 */
export async function createToken(page, name, { readOnly = false, projects = [] } = {}) {
  await goto(page, "/settings/tokens");
  await page.waitForSelector("#field-can-only-read", { timeout: WAIT });
  await fill(page, "New token name", name);
  if (readOnly) await page.click("#field-can-only-read");
  for (const key of projects) {
    await page.waitForSelector(`[data-project="${key}"]`, { timeout: WAIT });
    await page.click(`[data-project="${key}"]`);
  }
  await submit(page);
  await page.waitForFunction(() => document.body.innerText.includes("Copy this token now"), { timeout: WAIT });
  return page.$eval("code", (code) => code.textContent.trim());
}

/** Opens the command palette with the keyboard and waits for it. */
export async function openPalette(page) {
  await page.keyboard.down("Control");
  await page.keyboard.press("k");
  await page.keyboard.up("Control");
  await page.waitForSelector("[data-palette]", { timeout: WAIT });
}

/**
 * Asks the guide a question through the Help button and follows the first
 * answer; returns the answer's id, or null when the guide had none.
 */
export async function askGuide(page, question) {
  await page.waitForSelector('[data-action="guide"]', { timeout: WAIT });
  await page.click('[data-action="guide"]');
  await page.waitForSelector('[data-palette][data-palette-mode="ask"]', { timeout: WAIT });
  await page.type('[data-palette] input', question);
  await page.waitForFunction(
    () => document.querySelector('[data-palette-option^="answer:"]') || document.querySelector("[data-palette-empty]"),
    { timeout: WAIT },
  );
  const first = await page.$('[data-palette-option^="answer:"]');
  if (!first) {
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => !document.querySelector("[data-palette]"), { timeout: WAIT });
    return null;
  }
  const id = await first.evaluate((el) => el.getAttribute("data-palette-option"));
  await first.click();
  return id;
}

/** Waits until the SPA has navigated to the given path. */
export async function waitForPath(page, path, timeout = WAIT) {
  await page.waitForFunction((want) => location.pathname === want, { timeout }, path);
}

/**
 * Waits for the authenticated shell to actually be on screen. The URL changes
 * as soon as the route matches, which is before the layout has rendered, so
 * waiting on the path alone makes every following selector a race.
 */
export async function waitForApp(page, timeout = WAIT) {
  await page.waitForSelector("header", { timeout });
  await page.waitForSelector('a[href="/settings/tokens"]', { timeout });
}

export async function textOf(page, selector) {
  await page.waitForSelector(selector, { timeout: WAIT });
  return page.$eval(selector, (el) => el.textContent.trim());
}

export async function bodyText(page) {
  return page.evaluate(() => document.body.innerText);
}

/** Registers a new organization through the UI and lands in the app. */
export async function signUp(page, who = unique("user")) {
  await goto(page, "/signup");
  await fill(page, "Your name", who.name);
  await fill(page, "Work email", who.email);
  await fill(page, "Password", PASSWORD);
  await fill(page, "Repeat password", PASSWORD);
  await fill(page, "Organization name", who.org);
  await submit(page);
  await waitForPath(page, "/");
  await waitForApp(page);
  return who;
}

/**
 * Creates a project through the UI and returns its key. With no template named
 * the form's default stands, which is whatever the server offers first.
 */
export async function createProject(page, name, template) {
  await goto(page, "/projects");
  await page.waitForSelector("button", { timeout: WAIT });
  await clickButton(page, "New project");
  await fill(page, "Project name", name);
  if (template) {
    await page.waitForSelector(`[data-template="${template}"]`, { timeout: WAIT });
    await page.click(`[data-template="${template}"]`);
  }

  // The key field is prefilled from the server's suggestion; read it back so
  // the test refers to the project the same way the application does.
  await page.waitForFunction(() => document.querySelector("#field-key")?.value?.length >= 2, { timeout: WAIT });
  const key = await page.$eval("#field-key", (el) => el.value);

  await clickButton(page, "Create project");
  // The app moves to the new project itself; return once it has arrived.
  await waitForPath(page, `/projects/${key}`);
  await settled(page);
  await page.waitForFunction((n) => document.body.innerText.includes(n), { timeout: WAIT }, name);
  return key;
}

/** Creates an issue in a project through the UI and returns its key. */
export async function createIssue(page, projectKey, summary, typeName) {
  await goto(page, `/projects/${projectKey}`);
  await clickButton(page, "New issue");
  if (typeName) await selectByLabel(page, "#issue-type", typeName);
  await fill(page, "Summary", summary);
  await clickButton(page, "Create issue");
  await page.waitForFunction((s) => document.body.innerText.includes(s), { timeout: WAIT }, summary);

  return page.evaluate((s) => {
    const row = [...document.querySelectorAll("a")].find((a) => a.textContent.includes(s));
    return row?.getAttribute("href")?.split("/").pop() ?? "";
  }, summary);
}

/** How long to leave between the events of a drag, so the page can redraw. */
const DRAG_STEP_MS = 60;

function pause(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** The box an element occupies, measured on the runner's side. */
async function boxOf(page, selector) {
  await page.waitForSelector(selector, { timeout: WAIT });
  return page.$eval(selector, (el) => {
    const box = el.getBoundingClientRect();
    return { left: box.left, top: box.top, right: box.right, bottom: box.bottom, width: box.width, height: box.height };
  });
}

async function centreOf(page, selector) {
  const box = await boxOf(page, selector);
  return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
}

/** Fires one pointer event at an element, at a point on the page. */
async function firePointer(page, selector, type, at) {
  await page.evaluate(
    (sel, kind, point) => {
      const element = document.querySelector(sel);
      if (!element) throw new Error(`nothing at ${sel} to send a ${kind} to`);
      element.dispatchEvent(
        new PointerEvent(kind, {
          bubbles: true,
          cancelable: true,
          clientX: point.x,
          clientY: point.y,
          pointerId: 1,
          isPrimary: true,
          pointerType: "mouse",
          button: 0,
        }),
      );
    },
    selector,
    type,
    at,
  );
}

/**
 * Drags with pointer events, one short evaluation each: a single in-page
 * promise waiting on frames is collected mid-drag under load.
 */
async function pointerDrag(page, { target, from, to, via = [], moveTarget }) {
  const moving = moveTarget ?? target;
  await firePointer(page, target, "pointerdown", from);
  for (const point of via) {
    await pause(DRAG_STEP_MS);
    await firePointer(page, moving, "pointermove", point);
  }
  const end = typeof to === "function" ? await to() : to;
  await pause(DRAG_STEP_MS);
  await firePointer(page, moving, "pointermove", end);
  await pause(DRAG_STEP_MS);
  await firePointer(page, moving, "pointerup", end);
}

/**
 * Fires one HTML5 drag event, keeping the DataTransfer on the window so the
 * events of a drag share one exactly as a real drag would.
 */
async function fireDrag(page, selector, type) {
  await page.evaluate(
    (sel, kind) => {
      const element = document.querySelector(sel);
      if (!element) throw new Error(`nothing at ${sel} to send a ${kind} to`);
      if (kind === "dragstart") window.e2eDragTransfer = new DataTransfer();
      element.dispatchEvent(
        new DragEvent(kind, { bubbles: true, cancelable: true, dataTransfer: window.e2eDragTransfer }),
      );
    },
    selector,
    type,
  );
}

/**
 * Drags a card onto a swimlane. The mouse cannot drive an HTML5 drag in a
 * headless browser, so the events the application listens for are sent instead.
 */
export async function dragCardTo(page, cardKey, swimlaneName) {
  const card = `[data-card="${cardKey}"]`;
  const lane = `[data-swimlane="${swimlaneName}"]`;
  await page.waitForSelector(card, { timeout: WAIT });
  await page.waitForSelector(lane, { timeout: WAIT });

  await fireDrag(page, card, "dragstart");
  await pause(DRAG_STEP_MS);
  await fireDrag(page, lane, "dragover");
  await pause(DRAG_STEP_MS);
  await fireDrag(page, lane, "drop");
  await pause(DRAG_STEP_MS);
  await fireDrag(page, card, "dragend");
}

/** Returns the card keys in a swimlane, in the order they are shown. */
export async function cardsIn(page, swimlaneName) {
  return page.evaluate((lane) => {
    const column = document.querySelector(`[data-swimlane="${lane}"]`);
    if (!column) return [];
    return [...column.querySelectorAll("[data-card]")].map((el) => el.getAttribute("data-card"));
  }, swimlaneName);
}

/**
 * Picks the option with this visible text, and fires the change React listens
 * for; setting `value` alone does not.
 */
export async function selectByLabel(page, selector, optionText) {
  await page.waitForFunction(
    (sel, text) =>
      [...(document.querySelector(sel)?.options ?? [])].some((o) => o.textContent.trim() === text),
    { timeout: WAIT },
    selector,
    optionText,
  );
  await page.evaluate(
    (sel, text) => {
      const select = document.querySelector(sel);
      const option = [...select.options].find((o) => o.textContent.trim() === text);
      const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value").set;
      setter.call(select, option.value);
      select.dispatchEvent(new Event("change", { bubbles: true }));
    },
    selector,
    optionText,
  );
}

/** Adds a child issue from an issue's own children panel. */
export async function addChild(page, parentKey, summary, typeName) {
  await goto(page, `/issues/${parentKey}`);
  await clickButton(page, "Add child");
  if (typeName) await selectByLabel(page, "#child-type", typeName);
  await page.waitForSelector("#child-summary", { timeout: WAIT });
  await page.type("#child-summary", summary);
  await clickButton(page, "Add");
  await page.waitForFunction((s) => document.body.innerText.includes(s), { timeout: WAIT }, summary);

  return page.evaluate((s) => {
    const row = [...document.querySelectorAll("a")].find((a) => a.textContent.trim() === s);
    return row?.getAttribute("href")?.split("/").pop() ?? "";
  }, summary);
}

/** The rows of the project hierarchy view, with their indentation. */
export async function treeRows(page) {
  return page.evaluate(() =>
    [...document.querySelectorAll("li > div")].map((row) => ({
      text: row.innerText.replace(/\s+/g, " ").trim(),
      indent: parseFloat(row.style.paddingLeft) || 0,
    })),
  );
}

/** Sets a date input and fires the change React listens for. */
export async function setDate(page, selector, value) {
  await page.waitForSelector(selector, { timeout: WAIT });
  await page.evaluate(
    (sel, v) => {
      const input = document.querySelector(sel);
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
      setter.call(input, v);
      input.dispatchEvent(new Event("input", { bubbles: true }));
      input.dispatchEvent(new Event("change", { bubbles: true }));
    },
    selector,
    value,
  );
}

/** Schedules an issue from its own page, which is the keyboard route. */
export async function scheduleIssue(page, issueKey, start, due) {
  await goto(page, `/issues/${issueKey}`);
  await setDate(page, "#issue-start", start);
  await page.waitForFunction(() => !document.querySelector("#issue-start")?.disabled, { timeout: WAIT });
  await setDate(page, "#issue-due", due);
  await page.waitForFunction(
    (want) => document.querySelector("#issue-due")?.value === want,
    { timeout: WAIT },
    due,
  );
}

/** The dates a plan bar currently covers. */
export async function barRange(page, issueKey) {
  await page.waitForSelector(`[data-plan-bar="${issueKey}"]`, { timeout: WAIT });
  return page.evaluate((key) => {
    const el = document.querySelector(`[data-plan-bar="${key}"]`);
    return { start: el.dataset.planStart?.slice(0, 10), due: el.dataset.planDue?.slice(0, 10) };
  }, issueKey);
}

/** Where a bar is taken hold of: just inside its left edge, or on its right one. */
const BAR_GRIP_INSET_PX = 20;
const BAR_EDGE_INSET_PX = 2;

/** Drags a plan bar by a number of days, by its body or by its right edge. */
export async function dragBar(page, issueKey, days, mode = "move") {
  const bar = `[data-plan-bar="${issueKey}"]`;
  const box = await boxOf(page, bar);
  // The chart says how wide a day is; the fitted zoom depends on the window.
  const perDay = await page.$eval(bar, (el) => Number(el.closest("[data-px-per-day]").dataset.pxPerDay));
  const y = box.top + box.height / 2;
  const x = mode === "end" ? box.right - BAR_EDGE_INSET_PX : box.left + Math.min(BAR_GRIP_INSET_PX, box.width / 2);
  await pointerDrag(page, {
    target: mode === "end" ? `${bar} span:last-of-type` : bar,
    from: { x, y },
    to: { x: x + days * perDay, y },
  });
}

/** Clear of the day's own boundary, so the drag lands in the day it names. */
const DAY_INSET_PX = 2;

/**
 * Drags across the empty calendar of a plan row, from one day offset to another
 * relative to today, the way a person makes or schedules a ticket there.
 */
export async function dragAcross(page, rowSelector, fromDays, toDays) {
  await page.waitForSelector(rowSelector, { timeout: WAIT });
  const chart = await page.$eval(rowSelector, (row) => {
    const canvas = row.closest("[data-px-per-day]");
    const todayLine = canvas.querySelector("span[aria-hidden='true'].w-px");
    const box = row.getBoundingClientRect();
    return {
      perDay: Number(canvas.dataset.pxPerDay),
      left: canvas.getBoundingClientRect().left,
      todayX: todayLine ? parseFloat(todayLine.style.left) : 0,
      y: box.top + box.height / 2,
    };
  });
  const at = (days) => ({ x: chart.left + chart.todayX + days * chart.perDay + DAY_INSET_PX, y: chart.y });
  await pointerDrag(page, { target: rowSelector, from: at(fromDays), to: at(toDays) });
}

/** Types into the add row of a plan group and presses Enter. */
export async function addOnPlan(page, groupLabel, summary) {
  const field = `[data-plan-add="${groupLabel}"] input`;
  await page.waitForSelector(field, { timeout: WAIT });
  await page.click(field);
  await page.type(field, summary);
  await page.keyboard.press("Enter");
}

/** Clicks the button whose visible label matches exactly. */
export async function clickButton(page, label) {
  await page.waitForFunction(
    (text) => [...document.querySelectorAll("button")].some((b) => b.textContent.trim() === text),
    { timeout: WAIT },
    label,
  );
  await page.evaluate((text) => {
    const button = [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === text);
    button?.click();
  }, label);
}

/** Sets a controlled input's value the way typing would, replacing what was there. */
export async function replaceValue(page, selector, value) {
  await page.waitForSelector(selector, { timeout: WAIT });
  await page.$eval(
    selector,
    (input, text) => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
      setter.call(input, text);
      input.dispatchEvent(new Event("input", { bubbles: true }));
    },
    value,
  );
}

/**
 * Presses, moves and releases the real mouse. The workflow canvas is React
 * Flow, whose drag and connect listen for mouse events on the window, which
 * a synthetic pointer event dispatched at one element never reaches.
 */
async function mouseDrag(page, from, to, via = []) {
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  // A drag is measured from where it begins, so it begins within a pixel.
  await page.mouse.move(from.x + 2, from.y, { steps: 2 });
  for (const point of [...via, to]) {
    await pause(DRAG_STEP_MS);
    await page.mouse.move(point.x, point.y, { steps: 4 });
  }
  await pause(DRAG_STEP_MS);
  await page.mouse.up();
}

/** The centre of an element after scrolling it into view, so the mouse can reach it. */
async function centreInView(page, selector) {
  await page.waitForSelector(selector, { timeout: WAIT });
  await page.$eval(selector, (el) => el.scrollIntoView({ block: "center", inline: "center" }));
  return centreOf(page, selector);
}

/** Clicks a status on the workflow canvas, which opens it in the inspector. */
export async function selectNode(page, statusName) {
  const at = await centreInView(page, `[data-workflow-node="${statusName}"]`);
  await page.mouse.click(at.x, at.y);
  await page.waitForSelector(`[data-node-panel="${statusName}"]`, { timeout: WAIT });
}

/** Drags a status on the canvas by an offset and returns where it landed. */
export async function dragNode(page, statusName, dx, dy) {
  const at = await centreInView(page, `[data-workflow-node="${statusName}"]`);
  await mouseDrag(page, at, { x: at.x + dx, y: at.y + dy }, [{ x: at.x + dx / 2, y: at.y + dy / 2 }]);
  await pause(DRAG_STEP_MS);
  return nodePosition(page, statusName);
}

export async function nodePosition(page, statusName) {
  return page.$eval(`[data-workflow-node="${statusName}"]`, (el) => ({
    x: Number(el.dataset.x),
    y: Number(el.dataset.y),
  }));
}

/**
 * Draws a transition by dragging from a status's ring onto another status.
 * `from` is a status name, or "any status" for the box global moves leave.
 */
export async function dragConnect(page, from, to) {
  const start = await centreInView(page, `[data-connect-handle="${from}"]`);
  const end = await centreOf(page, `[data-workflow-node="${to}"]`);
  await mouseDrag(page, start, end, [{ x: (start.x + end.x) / 2, y: (start.y + end.y) / 2 }]);
  await page.waitForSelector("#field-transition-name", { timeout: WAIT });
}

/** Opens the designer on the named workflow from the settings list. */
export async function openWorkflowDesign(page, name) {
  await goto(page, "/settings/workflows");
  const edit = `[data-workflow-card="${name}"] [data-action="edit"]`;
  await page.waitForSelector(edit, { timeout: WAIT });
  await page.click(edit);
  await page.waitForSelector("[data-workflow-canvas]", { timeout: WAIT });
}

/** Deletes a scheme from the library's Schemes tab and confirms. */
export async function deleteScheme(page, name) {
  await goto(page, "/settings/workflows/schemes");
  const remove = `[data-scheme-card="${name}"] [data-action="delete"]`;
  await page.waitForSelector(remove, { timeout: WAIT });
  await page.click(remove);
  await confirm(page);
}

/** Coins a status from the designer's own panel and waits for it to land on the canvas. */
export async function coinStatus(page, { name, category }) {
  await page.waitForSelector("#field-new-status-name", { timeout: WAIT });
  await page.type("#field-new-status-name", name);
  if (category) await selectByLabel(page, "#field-new-status-category", category);
  await clickButton(page, "Create and add");
  await page.waitForSelector(`[data-workflow-node="${name}"]`, { timeout: WAIT });
}

/**
 * Draws a workflow in the designer and returns its name.
 *
 * The statuses are the organization's; a workflow is one opinion about how
 * issues move between them, so the designer is a matter of putting the ones
 * this workflow uses on the canvas, saying which one issues open in, and
 * drawing the moves. The first status placed is where issues open until told
 * otherwise.
 */
export async function createWorkflow(page, { name, statuses, opensIn, transitions = [] }) {
  await goto(page, "/settings/workflows");
  await clickButton(page, "New workflow");
  await page.waitForSelector("#field-name", { timeout: WAIT });
  await page.type("#field-name", name);

  for (const status of statuses) {
    await selectByLabel(page, "#field-add-status", status);
    await clickButton(page, "Add status");
    await page.waitForSelector(`[data-workflow-node="${status}"]`, { timeout: WAIT });
  }
  if (opensIn && opensIn !== statuses[0]) {
    await selectNode(page, opensIn);
    await clickButton(page, "New issues open here");
    await page.waitForSelector(`[data-workflow-node="${opensIn}"] [data-workflow-initial]`, { timeout: WAIT });
  }

  for (const transition of transitions) {
    if (transition.from === "Anywhere") {
      await dragConnect(page, "any status", transition.to);
    } else {
      await selectNode(page, transition.from);
      await selectByLabel(page, "#field-to-status", transition.to);
      await clickButton(page, "Add transition");
    }
    await replaceValue(page, "#field-transition-name", transition.name);
  }

  await clickButton(page, "Create workflow");
  await waitForButtonToGo(page, "Create workflow");
  return name;
}

/** Builds a scheme mapping one issue type onto a workflow. */
export async function createScheme(page, { name, issueType, workflow }) {
  await goto(page, "/settings/workflows/schemes");
  await clickButton(page, "New scheme");
  await page.waitForSelector("#field-scheme-name", { timeout: WAIT });
  await page.type("#field-scheme-name", name);

  await selectByLabel(page, 'select[aria-label="Issue type"]', issueType);
  await selectByLabel(page, 'select[aria-label="Workflow"]', workflow);

  await clickButton(page, "Create scheme");
  await waitForButtonToGo(page, "Create scheme");
  return name;
}

/** Waits for an editor to close, which is how a successful save shows itself. */
export async function waitForButtonToGo(page, label) {
  await page.waitForFunction(
    (text) => ![...document.querySelectorAll("button")].some((b) => b.textContent.trim() === text),
    { timeout: WAIT },
    label,
  );
}

/** Reads the project's workflow table as rows of issue type, workflow and scope. */
export async function assignmentRows(page, projectKey) {
  await goto(page, `/projects/${projectKey}/workflows`);
  await page.waitForSelector('[data-testid="workflow-assignments"] [data-assignment]', {
    timeout: WAIT,
  });
  return page.evaluate(() =>
    Object.fromEntries(
      [...document.querySelectorAll("[data-assignment]")].map((row) => [
        row.getAttribute("data-assignment"),
        // The control cell is left out: a select's options are not what the row says.
        [...row.querySelectorAll("td:not([data-assignment-control])")]
          .map((cell) => cell.innerText)
          .join(" ")
          .replace(/\s+/g, " ")
          .trim(),
      ]),
    ),
  );
}

/** The transitions an issue currently offers, by name. */
export async function movesOffered(page, issueKey) {
  await goto(page, `/issues/${issueKey}`);
  await page.waitForFunction((key) => document.body.innerText.includes(key), { timeout: WAIT }, issueKey);
  return page.$$eval("button", (bs) => bs.map((b) => b.textContent.trim()));
}

/** Creates a sprint on the planning page and returns its name. */
export async function createSprint(page, projectKey, name) {
  await goto(page, `/projects/${projectKey}/sprints`);
  await page.waitForSelector("#field-new-sprint", { timeout: WAIT });
  await page.type("#field-new-sprint", name);
  await clickButton(page, "Add sprint");
  await page.waitForFunction(
    (n) => document.querySelector(`[data-sprint="${n}"]`) !== null,
    { timeout: WAIT },
    name,
  );
  return name;
}

/** Gives a sprint its dates and capacity, which is what starting one needs. */
export async function planSprint(page, name, { from, to, capacity }) {
  const card = `[data-sprint="${name}"]`;
  await page.waitForSelector(`${card} input[type="date"]`, { timeout: WAIT });

  const [start, end] = await page.$$(`${card} input[type="date"]`);
  await setDateInput(page, start, from);
  await setDateInput(page, end, to);

  const box = await page.$(`${card} input[type="number"]`);
  await box.click({ clickCount: 3 });
  await box.type(String(capacity));
  // The capacity is saved on blur, so something else has to take the focus.
  await page.evaluate(() => document.activeElement?.blur());
  await page.waitForFunction(
    (n, c) => document.querySelector(`[data-sprint="${n}"]`)?.innerText.includes(`/${c} pts`),
    { timeout: WAIT },
    name,
    capacity,
  );
}

async function setDateInput(page, handle, value) {
  await page.evaluate((el, v) => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
    setter.call(el, v);
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
  }, handle, value);
}

/** Sizes an issue from its detail page. */
export async function estimateIssue(page, issueKey, points) {
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector("#issue-estimate", { timeout: WAIT });
  await page.click("#issue-estimate", { clickCount: 3 });
  await page.type("#issue-estimate", String(points));
  await page.evaluate(() => document.activeElement?.blur());
  await page.waitForFunction(
    (p) => document.querySelector("#issue-estimate")?.value === String(p),
    { timeout: WAIT },
    points,
  );
}

/** Commits an issue to a sprint from its detail page. */
export async function commitIssue(page, issueKey, sprintName) {
  await goto(page, `/issues/${issueKey}`);
  await selectByLabel(page, "#issue-sprint", sprintName);
  await page.waitForFunction(
    (n) => {
      const select = document.querySelector("#issue-sprint");
      return select?.options[select.selectedIndex]?.textContent.trim().startsWith(n);
    },
    { timeout: WAIT },
    sprintName,
  );
}

/** Makes an issue count towards a milestone from its own page. */
export async function assignMilestone(page, issueKey, milestoneName) {
  await goto(page, `/issues/${issueKey}`);
  await selectByLabel(page, "#issue-milestone", milestoneName);
  await page.waitForFunction(
    (n) => {
      const select = document.querySelector("#issue-milestone");
      return select?.options[select.selectedIndex]?.textContent.trim() === n;
    },
    { timeout: WAIT },
    milestoneName,
  );
}

/** Sets a milestone on the project's milestones page and returns its name. */
export async function createMilestone(page, projectKey, name, dueOn) {
  await goto(page, `/projects/${projectKey}/milestones`);
  await page.waitForSelector("#field-milestone-name", { timeout: WAIT });
  await page.type("#field-milestone-name", name);
  if (dueOn) await setDate(page, "#field-milestone-due", dueOn);
  await clickButton(page, "Add milestone");
  await page.waitForSelector(`[data-milestone="${name}"]`, { timeout: WAIT });
  return name;
}

/** The progress line a milestone shows on its card. */
export async function milestoneProgress(page, projectKey, name) {
  await goto(page, `/projects/${projectKey}/milestones`);
  await page.waitForSelector(`[data-milestone="${name}"] [data-milestone-progress]`, { timeout: WAIT });
  return textOf(page, `[data-milestone="${name}"] [data-milestone-progress]`);
}

/**
 * Measures how the plan's two panes sit against each other: where each row
 * starts on either side of the divider, how the headers compare, and whether
 * the calendar has grown wider than the pane it was meant to fit.
 */
export async function planGeometry(page) {
  await page.waitForSelector('[data-testid="plan-calendar"] [data-plan-bar-row]', { timeout: WAIT });
  return page.evaluate(() => {
    const sidebar = document.querySelector('[data-testid="plan-sidebar"]');
    const calendar = document.querySelector('[data-testid="plan-calendar"]');
    const canvas = calendar.querySelector('[data-testid="plan-canvas"]');
    const chart = calendar.querySelector("[data-px-per-day]");
    const rows = [...sidebar.querySelectorAll("[data-plan-row]")].map((side) => {
      const key = side.dataset.planRow;
      const bar = calendar.querySelector(`[data-plan-bar-row="${key}"]`);
      return { key, side: side.getBoundingClientRect().top, calendar: bar?.getBoundingClientRect().top ?? null };
    });
    const flag = calendar.querySelector("[data-milestone-flag]");
    const canvasBox = canvas.getBoundingClientRect();
    return {
      rows,
      sidebarHeaderHeight: sidebar.firstElementChild.getBoundingClientRect().height,
      calendarHeaderHeight: chart.getBoundingClientRect().top - canvasBox.top,
      paneWidth: calendar.clientWidth,
      scrollWidth: calendar.scrollWidth,
      canvasWidth: canvasBox.width,
      flag: flag
        ? { left: flag.getBoundingClientRect().left, right: flag.getBoundingClientRect().right, canvasRight: canvasBox.right, canvasLeft: canvasBox.left }
        : null,
    };
  });
}

/** The keys of the rows the plan is showing, in order, with group rows in brackets. */
export async function planRows(page) {
  await page.waitForSelector('[data-testid="plan-sidebar"] [data-plan-row], [data-testid="plan-sidebar"] [data-plan-group]', { timeout: WAIT });
  return page.$$eval('[data-testid="plan-sidebar"] [data-plan-row], [data-testid="plan-sidebar"] [data-plan-group]', (els) =>
    els.map((el) => (el.dataset.planGroup ? `[${el.dataset.planGroup}]` : el.dataset.planRow)),
  );
}

/** One figure off the plan's meter, by its label. */
export async function planFigure(page, label) {
  return textOf(page, `[data-plan-figure="${label}"] dd`);
}

/** Switches the plan to one of its zooms by the label on the button. */
export async function zoomPlan(page, label) {
  await page.evaluate(
    (l) => [...document.querySelectorAll('[role="group"][aria-label="Zoom"] button')].find((b) => b.textContent.trim() === l)?.click(),
    label,
  );
  await page.waitForFunction(
    (l) => document.querySelector(`[role="group"][aria-label="Zoom"] button[aria-pressed="true"]`)?.textContent.trim() === l,
    { timeout: WAIT },
    label,
  );
}

/** The capacity line a sprint shows on the plan's sprint view. */
export async function capacityOf(page, projectKey, sprintName) {
  await goto(page, `/projects/${projectKey}/plan?view=sprints`);
  await page.waitForSelector(`[data-sprint-capacity="${sprintName}"]`, { timeout: WAIT });
  return page.$eval(`[data-sprint-capacity="${sprintName}"]`, (el) =>
    el.innerText.replace(/\s+/g, " ").trim(),
  );
}

/** Forms a team in a project and returns its name. */
export async function createTeam(page, projectKey, name) {
  await goto(page, `/projects/${projectKey}/teams`);
  await page.waitForSelector("#field-new-team", { timeout: WAIT });
  await page.click("#field-new-team");
  await page.type("#field-new-team", name);
  // The button is disabled until the field holds something, so typing has to
  // have landed before the click is worth making.
  await page.waitForFunction(
    (n) => document.querySelector("#field-new-team")?.value === n,
    { timeout: WAIT },
    name,
  );
  await clickButton(page, "Form team");
  await page.waitForFunction(
    (n) => document.querySelector(`[data-team="${n}"]`) !== null,
    { timeout: WAIT },
    name,
  );
  return name;
}

/** Says how many points a team can take on in a week, from the teams page. */
export async function setTeamCapacity(page, projectKey, teamName, points) {
  await goto(page, `/projects/${projectKey}/teams`);
  const field = `[data-team-capacity="${teamName}"]`;
  await page.waitForSelector(field, { timeout: WAIT });
  await page.click(field, { clickCount: 3 });
  await page.type(field, String(points));
  await page.evaluate(() => document.activeElement?.blur());
  await page.waitForFunction(
    (sel, want) => document.querySelector(sel)?.value === want,
    { timeout: WAIT },
    field,
    String(points),
  );
}

/**
 * Drags an issue's sidebar row on the plan onto another row, or above the rows
 * with "top", the way a person moves a ticket under a new parent.
 */
/** The row is taken hold of by its right end, clear of the text and the chevron. */
const ROW_GRIP_INSET_PX = 40;
/** Far enough for the drag to count as one rather than a click. */
const DRAG_THRESHOLD_PX = 20;

export async function dragRow(page, issueKey, onto) {
  const source = `[data-plan-row="${issueKey}"]`;
  const box = await boxOf(page, source);
  const x = box.left + box.width - ROW_GRIP_INSET_PX;
  const y = box.top + box.height / 2;
  const destination = onto === "top" ? "[data-plan-drop-top]" : `[data-plan-row="${onto}"]`;
  await pointerDrag(page, {
    target: source,
    from: { x, y },
    // Past the threshold first: the drop zone above the rows is only drawn once
    // a drag is under way, so it is measured after that move.
    via: [{ x, y: y + DRAG_THRESHOLD_PX }],
    to: async () => {
      const to = await boxOf(page, destination);
      return { x, y: to.top + to.height / 2 };
    },
  });
}

/** Drags the link handle at the end of one issue's bar onto another issue's row, making the first block the second. */
/** Far enough right of the handle to be a drag, while staying on the other row. */
const LINK_DROP_NUDGE_PX = 30;

export async function dragLink(page, fromKey, toKey) {
  const handle = `[data-plan-link-handle="${fromKey}"]`;
  const start = await centreOf(page, handle);
  const row = await boxOf(page, `[data-plan-bar-row="${toKey}"]`);
  await pointerDrag(page, {
    target: handle,
    from: start,
    to: { x: start.x + LINK_DROP_NUDGE_PX, y: row.top + row.height / 2 },
  });
}

/** Drags the dot on one ticket's box in the dependency graph onto another ticket's box. */
export async function dragGraphLink(page, fromKey, toKey) {
  const handle = `[data-graph-handle="${fromKey}"]`;
  const from = await centreOf(page, handle);
  const to = await centreOf(page, `[data-graph-node="${toKey}"]`);
  await pointerDrag(page, { target: handle, from, to });
}

/** Types a query into the query box on the page and applies it with Enter. */
export async function runQuery(page, text) {
  await page.waitForSelector("[data-query]", { timeout: WAIT });
  await page.click("[data-query]", { clickCount: 3 });
  await page.keyboard.press("Backspace");
  if (text) await page.type("[data-query]", text);
  await page.keyboard.press("Enter");
}

/** Picks the row under the query box whose text starts with the words, by mouse, and waits for the list to settle. */
export async function pickSuggestion(page, text) {
  await page.waitForSelector("[data-query-suggestions] [data-suggestion]", { timeout: WAIT });
  await page.waitForFunction(
    (t) => [...document.querySelectorAll("[data-query-suggestions] [data-suggestion]")].some((el) => el.dataset.suggestionText?.startsWith(t) || el.innerText.trim().startsWith(t)),
    { timeout: WAIT },
    text,
  );
  await page.evaluate((t) => {
    const row = [...document.querySelectorAll("[data-query-suggestions] [data-suggestion]")].find((el) => el.dataset.suggestionText?.startsWith(t) || el.innerText.trim().startsWith(t));
    row.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
  }, text);
}

/** The issue keys listed in the page's issue table, top to bottom. */
export async function issueRows(page) {
  await page.waitForSelector("[data-issue-row]", { timeout: WAIT });
  return page.$$eval("[data-issue-row]", (rows) => rows.map((row) => row.dataset.issueRow));
}

/** Sets how many days done work stays on the plan; an empty string keeps everything. */
export async function setClosedForDays(page, days) {
  await openPlanFilters(page);
  const field = "[data-plan-closed-for]";
  await page.waitForSelector(field, { timeout: WAIT });
  await page.click(field, { clickCount: 3 });
  await page.keyboard.press("Backspace");
  if (days !== "") await page.type(field, String(days));
  await page.waitForFunction(
    (sel, want) => document.querySelector(sel)?.value === want,
    { timeout: WAIT },
    field,
    String(days),
  );
}

/** Puts the signed-in person on a team. */
export async function joinTeam(page, projectKey, teamName, personName) {
  await goto(page, `/projects/${projectKey}/teams`);
  const card = `[data-team="${teamName}"]`;
  await page.waitForSelector(`${card} select`, { timeout: WAIT });
  await selectByLabel(page, `${card} select`, personName);
  await page.evaluate((sel) => {
    const scope = document.querySelector(sel);
    [...scope.querySelectorAll("button")].find((b) => b.textContent.trim() === "Add")?.click();
  }, card);
  await page.waitForFunction(
    (sel, who) => document.querySelector(sel)?.innerText.includes(who),
    { timeout: WAIT },
    card,
    personName,
  );
}

/** Hands an issue to a team from its detail page. */
export async function assignTeam(page, issueKey, teamName) {
  await goto(page, `/issues/${issueKey}`);
  await selectByLabel(page, "#issue-team", teamName);
  await page.waitForFunction(
    (n) => {
      const select = document.querySelector("#issue-team");
      return select?.options[select.selectedIndex]?.textContent.trim() === n;
    },
    { timeout: WAIT },
    teamName,
  );
}

/** The cards on one of a project's boards, chosen by name. */
export async function cardsOnBoard(page, projectKey, boardName) {
  await goto(page, `/projects/${projectKey}/board`);
  await page.waitForSelector(`[data-board="${boardName}"]`, { timeout: WAIT });
  await page.click(`[data-board="${boardName}"]`);
  // The heading changes the moment the button is pressed; the cards arrive with
  // the next response. Waiting on the heading would read the previous board.
  await page.waitForFunction(
    (n) => document.querySelector("[data-board-showing]")?.getAttribute("data-board-showing") === n,
    { timeout: WAIT },
    boardName,
  );
  return page.$$eval("[data-card]", (els) => els.map((el) => el.getAttribute("data-card")));
}

/** Invites somebody into the organization and returns their sign-in details. */
export async function inviteMember(page, prefix, role = "member") {
  // The display name is unique too: the access table is read by name, and two
  // people called the same thing would make it ambiguous.
  const identity = unique(prefix);
  const who = { email: identity.email, name: identity.org };
  const token = await inviteAddress(page, who.email, role);
  return { ...who, token };
}

/** Makes a local account on the users page and waits for its row. */
export async function createLocalUser(page, { email, name, role = "Member", password = PASSWORD }) {
  await goto(page, "/settings/users");
  await fill(page, "Email", email);
  await fill(page, "Name", name);
  await selectByLabel(page, "#field-role", role);
  await fill(page, "Password", password);
  await clickButton(page, "Create user");
  await page.waitForSelector(`[data-user="${email}"]`, { timeout: WAIT });
}

/** Invites an address into the signed-in person's organization and returns the invitation's token. */
export async function inviteAddress(page, email, role = "member") {
  await goto(page, "/settings/tokens");
  return page.evaluate(async (email, role) => {
    const response = await fetch("/api/v1/invites", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, role }),
    });
    if (!response.ok) throw new Error(`invite failed: ${response.status}`);
    return (await response.json()).token;
  }, email, role);
}

/**
 * Offers an invitation from this browser as it is, signed in or not, and
 * returns the refusal's error code, or an empty string when it was taken.
 */
export async function tryAcceptInvite(page, { token, name = "Invited Person", password = PASSWORD }) {
  return page.evaluate(
    async (token, name, password) => {
      const response = await fetch("/api/v1/auth/invites/accept", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, name, password }),
      });
      if (response.ok) return "";
      const body = await response.json().catch(() => ({}));
      return body.error?.code ?? String(response.status);
    },
    token,
    name,
    password,
  );
}

/**
 * Accepts an invitation in this browser, which leaves it signed in as them.
 *
 * Signing out first is not tidiness: an invitation accepted while somebody else
 * is signed in joins with that existing account, which is right for a person
 * following a link in their own browser and wrong for a test pretending to be
 * two people.
 */
export async function acceptInvite(page, who) {
  await signOut(page);
  await goto(page, "/login");
  const refused = await tryAcceptInvite(page, { token: who.token, name: who.name });
  if (refused) throw new Error(`accept failed: ${refused}`);
}

/** Grants a role on the access page, the way an administrator would. */
export async function grantRole(page, { who, role, projectKey }) {
  await goto(page, "/settings/access");
  await page.waitForSelector('select[aria-label="Who to grant to"]', { timeout: WAIT });

  await selectByLabel(page, 'select[aria-label="Who to grant to"]', who);
  await selectByLabel(page, 'select[aria-label="Role"]', role);
  if (projectKey) {
    await selectByLabel(page, 'select[aria-label="Where the role applies"]', projectKey);
  }
  await clickButton(page, "Grant");

  await page.waitForFunction(
    (name, where) => {
      const table = document.querySelector('[data-testid="role-assignments"]');
      return [...(table?.querySelectorAll("tr") ?? [])].some(
        (row) => row.innerText.includes(name) && row.innerText.includes(where),
      );
    },
    { timeout: WAIT },
    who,
    projectKey ?? "Every project",
  );
}

/** Takes every role somebody holds away, so a test can start from nothing. */
export async function revokeAll(page, who) {
  await goto(page, "/settings/access");
  await page.waitForSelector('[data-testid="role-assignments"]', { timeout: WAIT });

  for (;;) {
    const held = await rolesHeldBy(page, who);
    if (held === 0) break;
    await page.evaluate((name) => {
      const rows = [...document.querySelectorAll('[data-testid="role-assignments"] tr')];
      const row = rows.find((each) => each.innerText.includes(name));
      [...row.querySelectorAll("button")].find((b) => b.textContent.trim() === "Revoke")?.click();
    }, who);
    // Revoking asks first now; the loop would otherwise spin on the same row.
    await confirm(page);
    // One row fewer rather than none: the same person may hold several roles.
    await page.waitForFunction(
      (name, was) =>
        [...document.querySelectorAll('[data-testid="role-assignments"] tr')].filter((row) =>
          row.innerText.includes(name),
        ).length < was,
      { timeout: WAIT },
      who,
      held,
    );
  }
}

/** How many rows of the access table name this person. */
async function rolesHeldBy(page, who) {
  return page.evaluate(
    (name) =>
      [...document.querySelectorAll('[data-testid="role-assignments"] tr')].filter((row) =>
        row.innerText.includes(name),
      ).length,
    who,
  );
}

/** Signs out, which the sign-in page needs before it will show a form again. */
export async function signOut(page) {
  await goto(page, "/");
  const found = await page.evaluate(() => {
    const button = [...document.querySelectorAll("button")].find(
      (b) => b.textContent.trim() === "Sign out",
    );
    button?.click();
    return Boolean(button);
  });
  if (found) await waitForPath(page, "/login");
}

export async function signIn(page, email, password = PASSWORD) {
  await signOut(page);
  await goto(page, "/login");
  await fill(page, "Email", email);
  await fill(page, "Password", password);
  await submit(page);
}

export { expect };

/** Starts a planned sprint from the sprints page and waits for it to be running. */
export async function startSprint(page, projectKey, name) {
  await goto(page, `/projects/${projectKey}/sprints`);
  await page.waitForSelector(`[data-sprint="${name}"]`, { timeout: WAIT });
  await page.evaluate((n) => {
    const card = document.querySelector(`[data-sprint="${n}"]`);
    [...card.querySelectorAll("button")].find((b) => b.textContent.trim() === "Start sprint")?.click();
  }, name);
  await page.waitForFunction(
    (n) => document.querySelector(`[data-sprint="${n}"]`)?.innerText.includes("Running"),
    { timeout: WAIT },
    name,
  );
}

/** Adds a board to a project through the board page's form and returns its name. */
export async function createBoard(page, projectKey, name, type) {
  await goto(page, `/projects/${projectKey}/board`);
  await clickButton(page, "New board");
  await fill(page, "Board name", name);
  await page.click(`[data-board-type="${type}"]`);
  await clickButton(page, "Create board");
  await page.waitForSelector(`[data-board="${name}"]`, { timeout: WAIT });
  return name;
}

// ------------------------------------------------------------------ gitea ---

/** The stack's own git host, as this container reaches it. */
export const GITEA_URL = process.env.E2E_GITEA_URL ?? "http://gitea:3000";
const GITEA_USER = process.env.E2E_GITEA_USER ?? "demo";
const GITEA_PASSWORD = process.env.E2E_GITEA_PASSWORD ?? "demo password please";

/**
 * Calls Gitea's API as its demo user, the way a person with a repository
 * there would: this is the other side of the connection, not part of the
 * product under test.
 */
export async function gitea(method, path, body) {
  const response = await fetch(`${GITEA_URL}/api/v1${path}`, {
    method,
    headers: {
      Authorization: "Basic " + Buffer.from(`${GITEA_USER}:${GITEA_PASSWORD}`).toString("base64"),
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) throw new Error(`gitea ${method} ${path} answered ${response.status}: ${text}`);
  return text ? JSON.parse(text) : null;
}

/** Makes a fresh repository with a first commit on main, and a token that can write to it. */
export async function giteaRepository(prefix) {
  const name = `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
  await gitea("POST", "/user/repos", { name, auto_init: true, default_branch: "main" });
  const token = await gitea("POST", `/users/${GITEA_USER}/tokens`, { name, scopes: ["write:repository", "write:issue"] });
  return {
    owner: GITEA_USER,
    name,
    fullName: `${GITEA_USER}/${name}`,
    token: token.sha1,
    async remove() {
      await gitea("DELETE", `/repos/${GITEA_USER}/${name}`).catch(() => {});
      await gitea("DELETE", `/users/${GITEA_USER}/tokens/${name}`).catch(() => {});
    },
  };
}

/** Between reloads, while waiting for what a webhook or a worker brings in. */
const RELOAD_EVERY_MS = 300;

/** Reloads the page until the selector is there, for what a webhook brings in on its own time. */
export async function eventually(page, selector, timeout = 30_000) {
  const until = Date.now() + timeout;
  for (;;) {
    const found = await page.$(selector);
    if (found) return found;
    if (Date.now() > until) throw new Error(`${selector} did not appear within ${timeout}ms`);
    await pause(RELOAD_EVERY_MS);
    await reload(page);
  }
}

// ------------------------------------------------------------------- mail ---

const MAILPIT_URL = process.env.E2E_MAILPIT_URL ?? "http://mailpit:8025";

/** Between asks of the mailbox: the relay takes a moment to deliver. */
const MAIL_POLL_MS = 500;

/**
 * Waits for a mail to an address whose subject contains the words, and returns
 * its subject and text. Mailpit keeps everything the stack sends; `since`
 * skips what was there before the step under test.
 */
export async function mailFor(address, subjectPart, { since = 0, timeout = 20_000, saying = "" } = {}) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const search = await fetch(`${MAILPIT_URL}/api/v1/search?query=${encodeURIComponent(`to:${address}`)}`);
    const { messages = [] } = await search.json();
    // Mailpit's snippet is the body's first line, enough to tell two mails
    // about one issue apart when they may arrive in either order.
    const found = messages.find((m) => m.Subject.includes(subjectPart) && new Date(m.Created).getTime() >= since && (!saying || m.Snippet?.includes(saying)));
    if (found) {
      const message = await (await fetch(`${MAILPIT_URL}/api/v1/message/${found.ID}`)).json();
      return { subject: message.Subject, text: message.Text, messageId: message.MessageID };
    }
    await pause(MAIL_POLL_MS);
  }
  throw new Error(`no mail to ${address} about "${subjectPart}" within ${timeout}ms`);
}

/** The six digits a code mail carries. */
export function codeIn(text) {
  const match = text.match(/\b\d{6}\b/);
  if (!match) throw new Error(`no code in ${JSON.stringify(text)}`);
  return match[0];
}

/** The organization's slug, which the portal's door is named by. */
export async function orgSlug(page) {
  return page.evaluate(async () => {
    const response = await fetch("/api/v1/auth/me", { credentials: "include" });
    const body = await response.json();
    return body.principal.org.slug;
  });
}

/** Enters a desk's door with a mailed code, as somebody without an account. */
export async function enterDesk(page, slug, address, next) {
  const since = Date.now() - 1000;
  await goto(page, `/desk/${slug}${next ? `?next=${encodeURIComponent(next)}` : ""}`);
  await fill(page, "Email", address);
  await clickButton(page, "Send me a code");
  await page.waitForSelector("[data-desk-code]", { timeout: WAIT });
  const mail = await mailFor(address, "Your code", { since });
  await page.type("[data-desk-code]", codeIn(mail.text));
  await clickButton(page, "Enter");
  await waitForPath(page, next ?? "/portal");
}

/** Walks into a desk whose door is open: a name and an address, no code. */
export async function enterOpenDesk(page, slug, deskKey, who, next) {
  await goto(page, `/desk/${slug}?desk=${deskKey}${next ? `&next=${encodeURIComponent(next)}` : ""}`);
  await page.waitForSelector(`[data-open-door="${deskKey}"]`, { timeout: WAIT });
  await fill(page, "Your name", who.name);
  await fill(page, "Email", who.email);
  await clickButton(page, "Continue");
  await waitForPath(page, next ?? "/portal");
}

/** Flips the door's code switch on the service desk page and waits for the word. */
export async function toggleDoor(page, projectKey) {
  await goto(page, `/projects/${projectKey}/service-desk`);
  await page.waitForSelector('[data-action="toggle-door"]', { timeout: WAIT });
  const before = await page.$eval('[data-action="toggle-door"]', (el) => el.dataset.doorVerifies);
  await page.click('[data-action="toggle-door"]');
  await page.waitForFunction((was) => document.querySelector('[data-action="toggle-door"]')?.dataset.doorVerifies !== was, { timeout: WAIT }, before);
  return page.$eval("[data-desk-address]", (el) => el.textContent.trim());
}

/** Deletes the signed-in account from the profile page and waits for the sign-in page. */
export async function deleteMyAccount(page) {
  await goto(page, "/settings/profile");
  await page.waitForSelector('[data-action="erase-me"]', { timeout: WAIT });
  await page.click('[data-action="erase-me"]');
  await confirm(page);
  await waitForPath(page, "/login");
}

/**
 * Invites an address from the Access page's Members tab, the way an
 * administrator would, and returns the link the page shows and the secret in
 * it. The link names the app's configured address, not this browser's, so
 * callers open the secret on BASE_URL themselves.
 */
export async function inviteFromMembersPage(page, email, roleLabel = "Member") {
  await goto(page, "/settings/access");
  await page.click('[data-access-tab="Members"]');
  await fill(page, "Invite by email", email);
  await selectByLabel(page, "#field-as", roleLabel);
  await clickButton(page, "Send invitation");
  const shown = `[data-invite-link="${email}"] [data-invite-url]`;
  await page.waitForSelector(shown, { timeout: WAIT });
  const link = await page.$eval(shown, (el) => el.textContent.trim());
  return { link, token: link.slice(link.indexOf("#") + 1) };
}

/** Opens an invitation link while signed out and makes the account it asks for. */
export async function joinThroughLink(page, token, who) {
  await signOut(page);
  await goto(page, `/invite#${token}`);
  await fill(page, "Your name", who.name);
  await fill(page, "Password", PASSWORD);
  await fill(page, "Repeat password", PASSWORD);
  await page.click("[data-invite-accept] button[type=submit]");
  await waitForPath(page, "/");
  await waitForApp(page);
}

/** Removes a member by name on the Access page's Members tab and waits for the row to go. */
export async function removeMember(page, name) {
  await goto(page, "/settings/access");
  await page.click('[data-access-tab="Members"]');
  const row = `[data-member="${name}"]`;
  await page.waitForSelector(`${row} [data-action="remove-member"]`, { timeout: WAIT });
  await page.click(`${row} [data-action="remove-member"]`);
  await confirm(page);
  await page.waitForFunction((sel) => !document.querySelector(sel), { timeout: WAIT }, row);
}

/** Flips one of the project's features on its settings page and waits for the switch to agree. */
export async function turnFeature(page, projectKey, feature, on) {
  await goto(page, `/projects/${projectKey}/settings`);
  const selector = `[data-feature="${feature}"]`;
  await page.waitForSelector(selector, { timeout: WAIT });
  const was = await page.$eval(selector, (el) => el.getAttribute("aria-checked"));
  if (was === String(on)) return;
  await page.click(selector);
  await page.waitForFunction((sel, want) => document.querySelector(sel)?.getAttribute("aria-checked") === want, { timeout: WAIT }, selector, String(on));
}

/** Adds a domain to the desk's trusted list on its setup page and waits for the tag. */
export async function trustDomain(page, projectKey, domain) {
  await goto(page, `/projects/${projectKey}/service-desk`);
  await page.waitForSelector("#field-trusted-domain", { timeout: WAIT });
  await page.type("#field-trusted-domain", domain);
  await page.click('[data-action="add-domain"]');
  await page.waitForSelector(`[data-trusted-domain="${domain}"]`, { timeout: WAIT });
}

/** Tries an open door with a name and an address and returns the refusal it shows. */
export async function turnedAwayAtOpenDesk(page, slug, deskKey, who) {
  await goto(page, `/desk/${slug}?desk=${deskKey}`);
  await page.waitForSelector(`[data-open-door="${deskKey}"]`, { timeout: WAIT });
  await fill(page, "Your name", who.name);
  await fill(page, "Email", who.email);
  await clickButton(page, "Continue");
  return textOf(page, "[role=alert]");
}

/**
 * Sends a mail straight to the development relay over SMTP, the way a reply
 * from a requester's own mail client would arrive. Node has no mail client, and
 * the conversation is six lines, so it is spoken here.
 */
export async function sendMail({ from, to, subject, body, inReplyTo }) {
  const [host, port] = (process.env.E2E_SMTP_ADDR ?? "mailpit:1025").split(":");
  const message = [
    `From: ${from}`,
    `To: ${to}`,
    `Subject: ${subject}`,
    `Message-ID: <${Date.now().toString(36)}${Math.random().toString(36).slice(2)}@armature.test>`,
    ...(inReplyTo ? [`In-Reply-To: <${inReplyTo.replace(/^<|>$/g, "")}>`] : []),
    "Content-Type: text/plain; charset=utf-8",
    "",
    ...body.split("\n").map((line) => (line.startsWith(".") ? "." + line : line)),
  ].join("\r\n");
  const steps = [`HELO e2e`, `MAIL FROM:<${from}>`, `RCPT TO:<${to}>`, `DATA`, `${message}\r\n.`, `QUIT`];
  await new Promise((resolve, reject) => {
    const socket = net.createConnection({ host, port: Number(port) });
    let buffer = "";
    let step = 0;
    socket.setEncoding("utf8");
    socket.on("data", (chunk) => {
      buffer += chunk;
      // A reply is complete at a line whose fourth character is a space.
      if (!/^\d{3} [^\n]*\r?\n$/m.test(buffer.split("\r\n").filter(Boolean).at(-1) + "\r\n")) return;
      const code = buffer.slice(0, 1);
      buffer = "";
      if (code !== "2" && code !== "3") {
        reject(new Error(`smtp refused at step ${step}: ${chunk.trim()}`));
        socket.end();
        return;
      }
      if (step >= steps.length) {
        socket.end();
        resolve();
        return;
      }
      socket.write(steps[step++] + "\r\n");
    });
    socket.on("error", reject);
  });
}

/** Presses the confirm button in the dialog that asks before something goes. */
export async function confirm(page) {
  const button = '[role="dialog"] [data-action="confirm"]';
  await page.waitForSelector(button, { timeout: WAIT });
  // The button is pressed through the DOM rather than by coordinates: the
  // dialog may still be settling where it sits, and a click by position on a
  // loaded machine has landed beside it.
  await page.evaluate((sel) => document.querySelector(sel)?.click(), button);
  await page.waitForFunction((sel) => !document.querySelector(sel), { timeout: WAIT }, button);
}

/** Opens the Filters popover on an issue list, which holds the label and milestone selects. */
export async function openFilters(page) {
  await page.waitForSelector('[data-action="filters"]', { timeout: WAIT });
  const open = await page.$eval('[data-action="filters"]', (el) => el.getAttribute("aria-expanded") === "true");
  if (!open) await page.click('[data-action="filters"]');
  await page.waitForSelector('[data-popover] select', { timeout: WAIT });
}

/** Opens the plan toolbar's Filters popover, which holds the type chips, the selects and the closed-for days. */
export async function openPlanFilters(page) {
  await page.waitForSelector('[data-action="plan-filters"]', { timeout: WAIT });
  const open = await page.$eval('[data-action="plan-filters"]', (el) => el.getAttribute("aria-expanded") === "true");
  if (!open) await page.click('[data-action="plan-filters"]');
  await page.waitForSelector('[data-testid="plan-filters"]', { timeout: WAIT });
}

/**
 * Decides one issue type's workflow on the project's page, the way a project
 * administrator does, and waits until the table says so. "Follow the
 * organization" hands the type back.
 */
export async function assignWorkflow(page, projectKey, typeName, workflowName) {
  await goto(page, `/projects/${projectKey}/workflows`);
  const control = `select[aria-label="Workflow for ${typeName}"]`;
  await page.waitForSelector(control, { timeout: WAIT });
  await selectByLabel(page, control, workflowName);
  const expected = workflowName === "Follow the organization" ? "Organization" : workflowName;
  await page.waitForFunction(
    (type, text) => {
      const row = document.querySelector(`[data-assignment="${type}"]`);
      const cells = [...(row?.querySelectorAll("td:not([data-assignment-control])") ?? [])];
      return cells.map((cell) => cell.innerText).join(" ").includes(text);
    },
    { timeout: WAIT },
    typeName,
    expected,
  );
}

/**
 * Arranges one issue type: each move names a place and the part of the page it
 * goes to, in the words a person reads. follow hands the type back.
 */
export async function arrangeIssue(page, projectKey, typeName, { moves = [], follow = false } = {}) {
  await goto(page, `/projects/${projectKey}/issue-view`);
  await selectByLabel(page, "#arrange-type", typeName);
  for (const { place, area } of moves) {
    await selectByLabel(page, `[data-arrange-where="${place}"]`, area);
    await settled(page);
    await page.waitForFunction(
      (key, title) => {
        const card = [...document.querySelectorAll("[data-arrange-area]")].find((el) => el.querySelector("h3")?.textContent.trim() === title);
        return Boolean(card?.querySelector(`[data-arrange-place="${key}"]`));
      },
      { timeout: WAIT },
      place,
      area,
    );
  }
  if (follow) {
    await clickButton(page, "Follow the organization");
    await settled(page);
    await page.waitForSelector('[data-arrange-origin="organization"], [data-arrange-origin="builtin"]', { timeout: WAIT });
  }
}

/** The labels one of an issue page's groups shows, in the order it shows them. */
export async function groupRows(page, title) {
  await page.waitForSelector(`[data-issue-group="${title}"]`, { timeout: WAIT });
  return page.evaluate((name) => {
    const group = document.querySelector(`[data-issue-group="${name}"]`);
    return [...(group?.querySelectorAll(":scope > dl > div > dt") ?? [])].map((dt) => dt.textContent.trim());
  }, title);
}

/** The runner's viewport, and the one the suite goes back to. */
const DEFAULT_VIEWPORT = { width: 1280, height: 860 };

/** Runs fn at another viewport width and puts the runner's own back afterwards. */
export async function withViewport(page, width, fn) {
  await page.setViewport({ width, height: DEFAULT_VIEWPORT.height });
  try {
    await fn();
  } finally {
    await page.setViewport(DEFAULT_VIEWPORT);
  }
}

/**
 * Opens an issue beside its list by clicking the row's key cell, which is not
 * a link, and waits for the panel to show it. Works docked or as the drawer.
 */
export async function openIssueBeside(page, issueKey) {
  await page.waitForSelector(`[data-issue-row="${issueKey}"]`, { timeout: WAIT });
  await page.click(`[data-issue-row="${issueKey}"] td:nth-child(2)`);
  await page.waitForSelector(`[data-issue-panel="${issueKey}"]`, { timeout: WAIT });
}

/** Puts a widget of this kind on the dashboard being looked at, arranging first if need be. */
export async function addWidget(page, kind) {
  if (!(await page.$(`[data-add-widget="${kind}"]`))) {
    await clickButton(page, "Arrange");
    await page.waitForSelector(`[data-add-widget="${kind}"]`, { timeout: WAIT });
  }
  await page.click(`[data-add-widget="${kind}"]`);
  await page.waitForSelector(`[data-widget="${kind}"]`, { timeout: WAIT });
}

/** Opens a chart widget's settings and picks what it should draw; each change saves at once. */
export async function chartSettings(page, { shape, groupBy, splitBy, series } = {}) {
  if (!(await page.$('[data-testid="chart-settings"]'))) {
    await page.click('[data-widget="chart"] [data-action="chart-settings"]');
    await page.waitForSelector('[data-testid="chart-settings"]', { timeout: WAIT });
  }
  const pick = async (label) => {
    await page.evaluate(
      (text) => [...document.querySelectorAll('[data-testid="chart-settings"] [role="group"][aria-label="Shape"] button')].find((b) => b.textContent.trim() === text)?.click(),
      label,
    );
  };
  if (shape) await pick(shape);
  if (groupBy) await selectByLabel(page, '[data-testid="chart-settings"] #field-group-by', groupBy);
  if (splitBy) await selectByLabel(page, '[data-testid="chart-settings"] #field-split-by', splitBy);
  if (series) await selectByLabel(page, '[data-testid="chart-settings"] #field-series', series);
}

/**
 * Opens a form, names the thing it makes and submits it, which is the shape of
 * every dialog here. Without a name the field's own suggestion stands.
 */
async function nameAndSubmit(page, { open, field, name, before, submit, until }) {
  if (open) await open();
  await page.waitForSelector(field, { timeout: WAIT });
  if (name !== undefined) {
    await page.click(field, { clickCount: 3 });
    await page.type(field, name);
  }
  if (before) await before();
  await clickButton(page, submit);
  if (until) await until();
}

/** Picks the template card whose text carries this title. */
async function pickTemplate(page, title) {
  await page.waitForFunction(
    (text) => [...document.querySelectorAll("[data-dashboard-template]")].some((el) => el.innerText.includes(text)),
    { timeout: WAIT },
    title,
  );
  await page.evaluate(
    (text) => [...document.querySelectorAll("[data-dashboard-template]")].find((el) => el.innerText.includes(text))?.click(),
    title,
  );
}

/**
 * Makes a dashboard on the project's dashboard page, from a template named by
 * its title on the card, or blank when none is given, and waits for it to show.
 */
export async function createDashboard(page, projectKey, name, templateTitle) {
  await goto(page, `/projects/${projectKey}/dashboard`);
  await nameAndSubmit(page, {
    open: () => clickButton(page, "New dashboard"),
    field: "#field-dashboard-name",
    name,
    before: templateTitle ? () => pickTemplate(page, templateTitle) : undefined,
    submit: "Create dashboard",
    until: () => page.waitForSelector(`[data-dashboard="${name}"]`, { timeout: WAIT }),
  });
}

/** Saves the dashboard being arranged as a template under a name of its own. */
export async function saveTemplate(page, name) {
  await nameAndSubmit(page, {
    open: () => clickAction(page, '[data-action="save-template"]'),
    field: "[data-save-template] #field-template-name",
    name,
    submit: "Save template",
    until: () => page.waitForFunction(() => !document.querySelector("[data-save-template]"), { timeout: WAIT }),
  });
}

/** From the milestone's card, makes the dashboard about it and waits to land there. */
export async function milestoneDashboard(page, projectKey, name) {
  await goto(page, `/projects/${projectKey}/milestones`);
  await nameAndSubmit(page, {
    open: () => clickAction(page, `[data-milestone="${name}"] [data-action="milestone-dashboard"]`),
    // The dialog opens with the milestone's own name in it, which is what it is for.
    field: `[data-milestone-dashboard="${name}"] #field-dashboard-name`,
    submit: "Create dashboard",
    until: async () => {
      await waitForPath(page, `/projects/${projectKey}/dashboard`);
      await page.waitForSelector("[data-widget]", { timeout: WAIT });
    },
  });
}

/** Waits for a control and presses it, for the ones that are not buttons by their text. */
async function clickAction(page, selector) {
  await page.waitForSelector(selector, { timeout: WAIT });
  await page.click(selector);
}

/** Presses Escape and waits for the dialog to be gone, so the page behind it is clickable. */
async function closeDialog(page, selector) {
  await page.keyboard.press("Escape");
  await page.waitForFunction((sel) => !document.querySelector(sel), { timeout: WAIT }, selector);
}

/** Makes a link to the dashboard on the page and returns its address, read from the one place it is shown. */
export async function shareDashboard(page, name) {
  await nameAndSubmit(page, {
    open: () => clickAction(page, '[data-action="share"]'),
    // The dialog's own field: the page behind it has an id of the same name.
    field: "[data-share-dialog] #field-link-name",
    name,
    submit: "New link",
    until: () => page.waitForSelector("[data-share-url]", { timeout: WAIT }),
  });
  const url = await page.$eval("[data-share-url]", (el) => el.getAttribute("data-share-url"));
  await closeDialog(page, "[data-share-dialog]");
  return url;
}

/** Revokes a link by its name from the share dialog. */
export async function revokeShare(page, name) {
  await clickAction(page, '[data-action="share"]');
  await clickAction(page, `[data-share="${name}"] [data-action="revoke-share"]`);
  await confirm(page);
  await page.waitForFunction((n) => !document.querySelector(`[data-share="${n}"]`), { timeout: WAIT }, name);
  await closeDialog(page, "[data-share-dialog]");
}

/** Waits until the status widget says the dashboard is counting this many issues. */
export async function expectCounted(page, count) {
  await page.waitForFunction(
    (n) => document.querySelector('[data-widget="status_breakdown"]')?.innerText.includes(`${n} in all`),
    { timeout: WAIT },
    count,
  );
}

/** A signed-up person with a project of two tasks and a bug, on its dashboard. */
export async function dashboardFixture(page, name) {
  const who = await signUp(page);
  const key = await createProject(page, name);
  await createIssue(page, key, "one task", "Task");
  await createIssue(page, key, "another task", "Task");
  await createIssue(page, key, "the bug", "Bug");
  await goto(page, `/projects/${key}/dashboard`);
  await expectCounted(page, 3);
  return { who, key };
}

// ------------------------------------------------------------ notifications ---

/** What an editor holds: the textarea's value in markdown mode, the text in rich mode. */
export async function editorText(page, selector) {
  return page.$eval(selector, (el) => ("value" in el && el.tagName === "TEXTAREA" ? el.value : el.textContent) ?? "");
}

/** Empties an editor and types into it, whichever face it shows. */
export async function writeInEditor(page, selector, text) {
  await page.waitForSelector(selector, { timeout: WAIT });
  await page.focus(selector);
  await page.keyboard.down("Control");
  await page.keyboard.press("a");
  await page.keyboard.up("Control");
  await page.keyboard.press("Backspace");
  if (text) await page.type(selector, text);
}

/** Switches every editor on the page to the rich or the markdown face. */
export async function setEditorMode(page, mode) {
  const option = `[data-editor-mode="${mode}"]`;
  await page.waitForSelector(option, { timeout: WAIT });
  await page.click(option);
  await page.waitForFunction((sel) => document.querySelector(sel)?.getAttribute("aria-pressed") === "true" || document.querySelector(sel)?.getAttribute("aria-checked") === "true", { timeout: WAIT }, option);
}

/**
 * Posts a comment on an issue that names somebody through the mention picker:
 * an at sign and the first letters of the name, then the offered option.
 */
export async function mentionInComment(page, issueKey, words, personName) {
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector("#new-comment", { timeout: WAIT });
  await page.click("#new-comment");
  await page.type("#new-comment", `${words} @${personName.slice(0, 3)}`);
  await page.waitForSelector(`[data-mention-option="${personName}"]`, { timeout: WAIT });
  await page.click(`[data-mention-option="${personName}"]`);
  await page.waitForFunction(
    (name) => {
      const el = document.querySelector("#new-comment");
      const text = el?.tagName === "TEXTAREA" ? el.value : el?.textContent;
      return text?.includes(`@${name}`);
    },
    { timeout: WAIT },
    personName,
  );
  await clickButton(page, "Comment");
  await page.waitForFunction((w) => document.body.innerText.includes(w), { timeout: WAIT }, words);
}

/** The inbox page's rows: kind and whether each is read. */
export async function inboxRows(page) {
  await goto(page, "/inbox");
  await page.waitForSelector("[data-inbox-view]", { timeout: WAIT });
  return page.$$eval("[data-notification]", (rows) => rows.map((r) => ({ kind: r.dataset.notification, read: r.dataset.notificationRead === "true", text: r.innerText })));
}

/** What the bell says, as a number. */
export async function unreadCount(page) {
  await page.waitForSelector("[data-unread-count]", { timeout: WAIT });
  return Number(await page.$eval("[data-unread-count]", (el) => el.dataset.unreadCount));
}

// ---------------------------------------------------------------- automation ---

/**
 * Makes a rule on a project's Automation page through the editor: a trigger,
 * one action, and returns to the table. The action's value goes into the
 * first action row's field.
 */
export async function createRule(page, projectKey, { name, trigger, action, value, text }) {
  await goto(page, `/projects/${projectKey}/automation`);
  await page.waitForSelector('[data-action="new-rule"]', { timeout: WAIT });
  await page.click('[data-action="new-rule"]');
  await page.waitForSelector("[data-rule-editor]", { timeout: WAIT });
  await fill(page, "Rule name", name);
  await selectByLabel(page, "#field-trigger", trigger);
  await selectByLabel(page, "#field-action-0", action);
  if (value !== undefined) {
    await page.waitForSelector("#field-action-value-0", { timeout: WAIT });
    await page.click("#field-action-value-0", { clickCount: 3 });
    await page.type("#field-action-value-0", value);
  }
  if (text !== undefined) {
    await page.waitForSelector("#field-action-text-0", { timeout: WAIT });
    await page.click("#field-action-text-0", { clickCount: 3 });
    await page.type("#field-action-text-0", text);
  }
  await page.click('[data-action="save-rule"]');
  await page.waitForSelector(`[data-rule="${name}"]`, { timeout: WAIT });
}

/** Opens a rule's menu item by its data-action. */
export async function ruleMenu(page, name, action) {
  await page.click(`[data-rule-menu="${name}"]`);
  await page.waitForSelector(`[data-action="${action}"]`, { timeout: WAIT });
  await page.click(`[data-action="${action}"]`);
}

/** Reads an incoming rule's address from the editor. */
export async function incomingAddress(page, name) {
  await ruleMenu(page, name, "rule-edit");
  await page.waitForSelector("[data-incoming-address]", { timeout: WAIT });
  const text = await textOf(page, "[data-incoming-address]");
  const match = text.match(/\/api\/v1\/automation\/hooks\/[A-Za-z0-9_-]+/);
  await page.keyboard.press("Escape");
  return match[0];
}

/** Adds a webhook on the settings page and returns the secret shown once. */
export async function createWebhook(page, name, url, { topics = ["*"] } = {}) {
  await goto(page, "/settings/webhooks");
  await page.waitForSelector('[data-action="new-webhook"]', { timeout: WAIT });
  await page.click('[data-action="new-webhook"]');
  await fill(page, "Webhook name", name);
  await fill(page, "Address", url);
  // Everything is ticked to start with; the topics asked for replace it.
  await page.click('[data-topic="*"]');
  for (const topic of topics) await page.click(`[data-topic="${topic}"]`);
  await page.click('[data-action="save-webhook"]');
  await page.waitForSelector("[data-secret-value]", { timeout: WAIT });
  return textOf(page, "[data-secret-value]");
}

/** Opens a webhook's menu item by its data-action. */
export async function webhookMenu(page, name, action) {
  await page.click(`[data-webhook-menu="${name}"]`);
  await page.waitForSelector(`[data-action="${action}"]`, { timeout: WAIT });
  await page.click(`[data-action="${action}"]`);
}

// ------------------------------------------------------ versions, components ---

/** Adds a version on the Releases page and waits for its row. */
export async function createVersion(page, projectKey, name, releaseOn) {
  await goto(page, `/projects/${projectKey}/releases`);
  await page.waitForSelector("#field-version-name", { timeout: WAIT });
  await page.type("#field-version-name", name);
  if (releaseOn) await page.$eval("#field-version-release", (el, v) => { el.value = v; el.dispatchEvent(new Event("input", { bubbles: true })); }, releaseOn);
  await clickButton(page, "Add version");
  await page.waitForSelector(`[data-version="${name}"]`, { timeout: WAIT });
}

/** Opens a version's menu item by its data-action. */
export async function versionMenu(page, name, action) {
  await page.click(`[data-version-menu="${name}"]`);
  await page.waitForSelector(`[data-action="${action}"]`, { timeout: WAIT });
  await page.click(`[data-action="${action}"]`);
}

/** Adds a component on the Components page, with who new work goes to. */
export async function createComponent(page, projectKey, name, { assignee } = {}) {
  await goto(page, `/projects/${projectKey}/components`);
  await page.waitForSelector("#field-component-name", { timeout: WAIT });
  await page.type("#field-component-name", name);
  if (assignee) await selectByLabel(page, "#field-component-assignee", assignee);
  await clickButton(page, "Add component");
  await page.waitForSelector(`[data-component="${name}"]`, { timeout: WAIT });
}

/**
 * Ticks a name in one of the issue page's set pickers (issue-fix-versions,
 * issue-affects-versions, issue-components) and waits for its tag.
 */
export async function pickName(page, pickerId, name) {
  await page.waitForSelector(`#${pickerId}`, { timeout: WAIT });
  await page.click(`#${pickerId}`);
  await page.waitForSelector(`[data-picker-option="${name}"]`, { timeout: WAIT });
  await page.click(`[data-picker-option="${name}"]`);
  await page.waitForSelector(`[data-picker="${pickerId}"] [data-picked="${name}"]`, { timeout: WAIT });
  await page.keyboard.press("Escape");
}

// ------------------------------------------------- filters, bulk, files, move ---

/** Saves the search that is running under a name and waits for its chip. */
export async function saveSearch(page, name, { shared = false } = {}) {
  await page.waitForSelector('[data-action="save-search"]', { timeout: WAIT });
  await page.click('[data-action="save-search"]');
  await page.waitForSelector("[data-save-filter]", { timeout: WAIT });
  await fill(page, "Filter name", name);
  if (shared) await page.click("#field-share-filter");
  await page.click('[data-save-filter] button[type="submit"]');
  await page.waitForSelector(`[data-filter="${name}"]`, { timeout: WAIT });
}

/** Ticks every row on an issue list and waits for the bulk bar. */
export async function selectAllIssues(page) {
  await page.waitForSelector("[data-select-all]", { timeout: WAIT });
  await page.click("[data-select-all]");
  await page.waitForSelector("[data-bulk-bar]", { timeout: WAIT });
}

/** Applies a bulk change from the bar and returns the refusals listed in the result. */
export async function applyBulk(page, { transition, priority } = {}) {
  if (transition) {
    await page.click("#field-bulk-transition", { clickCount: 3 });
    await page.type("#field-bulk-transition", transition);
  }
  if (priority) await selectByLabel(page, "#field-bulk-priority", priority);
  await page.click('[data-action="bulk-apply"]');
  await page.waitForSelector("[data-bulk-result]", { timeout: WAIT });
  const refused = await page.$$eval("[data-bulk-refusal]", (els) => els.map((el) => ({ key: el.dataset.bulkRefusal, text: el.innerText })));
  const title = await textOf(page, "[data-bulk-result] h2");
  return { refused, title };
}

/** Moves an issue from its page to another project through the More menu. */
export async function moveIssue(page, issueKey, targetKey) {
  await goto(page, `/issues/${issueKey}`);
  await page.waitForSelector('[data-action="issue-more"]', { timeout: WAIT });
  await page.click('[data-action="issue-more"]');
  await page.waitForSelector('[data-action="move"]', { timeout: WAIT });
  await page.click('[data-action="move"]');
  await page.waitForSelector("[data-move-dialog]", { timeout: WAIT });
  await page.waitForFunction((k) => [...document.querySelectorAll("#field-move-project option")].some((o) => o.value === k), { timeout: WAIT }, targetKey);
  await page.select("#field-move-project", targetKey);
  await page.click('[data-action="move-go"]');
  await page.waitForFunction((k) => new RegExp(`/issues/${k}-\\d+$`).test(location.pathname), { timeout: WAIT }, targetKey);
  const path = await location_of(page);
  // The page behind the new address has to have drawn before it is read.
  await page.waitForFunction((k) => document.body.innerText.includes("Moved from " + k), { timeout: WAIT }, issueKey);
  return path;
}

async function location_of(page) {
  return page.evaluate(() => location.pathname);
}

/** Imports a CSV written to the runner's disk through the project's Import page. */
export async function importCSV(page, projectKey, csvPath, { dry = false } = {}) {
  await goto(page, `/projects/${projectKey}/import`);
  await page.waitForSelector("[data-import-input]", { timeout: WAIT });
  const input = await page.$("[data-import-input]");
  await input.uploadFile(csvPath);
  await page.waitForSelector("[data-import-mapping]", { timeout: WAIT });
  await page.click('[data-action="dry-run"]');
  await page.waitForSelector('[data-import-report="dry"]', { timeout: WAIT });
  if (dry) return textOf(page, '[data-import-report="dry"]');
  await page.click('[data-action="import"]');
  await page.waitForSelector('[data-import-report="done"]', { timeout: WAIT });
  return textOf(page, '[data-import-report="done"]');
}

/** Puts one file on the issue or request shown, through its "Attach a file" picker. */
export async function attachFile(page, path) {
  await page.waitForSelector('input[type=file][aria-label="Attach a file"]', { timeout: WAIT });
  const picker = await page.$('input[type=file][aria-label="Attach a file"]');
  await picker.uploadFile(path);
  await page.waitForSelector(`[data-attachment="${basename(path)}"]`, { timeout: WAIT });
}

/** Queues files on the portal's raise form; they go up after the request is sent. */
export async function queueFiles(page, paths) {
  await page.waitForSelector('input[type=file][aria-label="Attach files"]', { timeout: WAIT });
  const picker = await page.$('input[type=file][aria-label="Attach files"]');
  await picker.uploadFile(...paths);
  for (const path of paths) {
    await page.waitForSelector(`[data-attach-queued-file="${basename(path)}"]`, { timeout: WAIT });
  }
}

/** Writes a knowledge base article on the desk's setup page, published when asked. */
export async function writeArticle(page, projectKey, title, body, { publish = true } = {}) {
  await goto(page, `/projects/${projectKey}/service-desk`);
  await page.waitForSelector("#field-article-title", { timeout: WAIT });
  await page.type("#field-article-title", title);
  await page.type("#field-article-body", body);
  await clickButton(page, "Add article");
  await page.waitForSelector(`[data-article="${title}"]`, { timeout: WAIT });
  if (publish) {
    await page.click(`[data-article="${title}"] [data-action="article-publish"]`);
    await page.waitForSelector(`[data-article="${title}"][data-article-published="true"]`, { timeout: WAIT });
  }
}

/** Writes a canned response on the desk's setup page. */
export async function writeCannedResponse(page, projectKey, name, body) {
  await goto(page, `/projects/${projectKey}/service-desk`);
  await page.waitForSelector("#field-canned-name", { timeout: WAIT });
  await page.type("#field-canned-name", name);
  await page.type("#field-canned-body", body);
  await clickButton(page, "Add response");
  await page.waitForSelector(`[data-canned="${name}"]`, { timeout: WAIT });
}

/** The titles the portal's article search offers for a query, once it has answered. */
export async function articlesOffered(page, query) {
  await page.waitForSelector("#field-article-search", { timeout: WAIT });
  await replaceValue(page, "#field-article-search", query);
  await page.waitForFunction(() => document.querySelectorAll("[data-article-search] [data-article]").length > 0, { timeout: WAIT });
  return page.$$eval("[data-article-search] [data-article]", (els) => els.map((el) => el.getAttribute("data-article")));
}

/** The rating path the resolution mail carries to the requester's address. */
export async function ratingLinkFor(address, requestKey, { since = 0 } = {}) {
  const mail = await mailFor(address, `[${requestKey}] your request is`, { since });
  const path = mail.text.match(/(\/rate\/[^\s]+)/)?.[1];
  if (!path) throw new Error(`no rating link in ${JSON.stringify(mail.text)}`);
  return path;
}

/** Rates a resolved request from its public page, as the requester would from the mail. */
export async function rateRequest(page, path, score, comment) {
  await goto(page, path);
  await page.waitForSelector("[data-rate-page]", { timeout: WAIT });
  await page.click(`[data-score="${score}"]`);
  if (comment) await page.type("#rate-comment", comment);
  await page.click('[data-action="send-rating"]');
  await page.waitForSelector("[data-rated]", { timeout: WAIT });
}

/** The days of the calendar grid an item's bar lies on, in order. */
export async function calendarDaysOf(page, key) {
  return page.$$eval(`[data-calendar-item="${key}"]`, (els) => els.map((el) => el.closest("[data-day]").getAttribute("data-day")));
}

/** Drags an issue's bar on the calendar from one day's cell to another's, the way a pointer does. */
export async function dragCalendarItem(page, key, fromDay, toDay) {
  const target = `[data-day="${fromDay}"] [data-calendar-item="${key}"]`;
  const from = await centreOf(page, target);
  const to = await centreOf(page, `[data-day="${toDay}"]`);
  await pointerDrag(page, { target, from, to, via: [{ x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 }] });
}

/** Posts how a project is doing from its issues page. */
export async function postStatus(page, projectKey, status, note) {
  await goto(page, `/projects/${projectKey}`);
  await page.waitForSelector('[data-action="post-status"]', { timeout: WAIT });
  await page.click('[data-action="post-status"]');
  await page.waitForSelector("[data-status-dialog]", { timeout: WAIT });
  await page.click(`[data-status-option="${status}"]`);
  if (note) await page.type("#field-status-note", note);
  await page.click('[data-action="post-status-go"]');
  await page.waitForSelector(`[data-status-strip] [data-project-status="${status}"]`, { timeout: WAIT });
}

/** The rows the audit log shows for one action, after narrowing to it. */
export async function auditRowsFor(page, action) {
  await goto(page, "/settings/audit");
  await page.waitForSelector("#field-audit-action", { timeout: WAIT });
  await page.waitForFunction((a) => [...document.querySelectorAll("#field-audit-action option")].some((o) => o.value === a), { timeout: WAIT }, action);
  await selectByLabel(page, "#field-audit-action", action);
  await page.waitForSelector(`[data-audit-row="${action}"]`, { timeout: WAIT });
  return page.$$eval("[data-audit-row]", (rows) => rows.map((r) => ({ action: r.getAttribute("data-audit-row"), text: r.innerText })));
}
