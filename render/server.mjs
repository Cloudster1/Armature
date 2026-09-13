// Prints a page of the product as a PDF, for the api to hand back as a file.
// It opens only paths under its own base address: the api names a path.
import { createServer } from "node:http";
import puppeteer from "puppeteer";

const PORT = Number(process.env.RENDER_PORT ?? 8090);
const APP_URL = (process.env.RENDER_APP_URL ?? "http://web:5173").replace(/\/$/, "");
/**
 * The whole print's budget. It stays under the api's render.DefaultTimeout, 20s
 * in backend/internal/render/render.go, so the api hears a reason, not a cut line.
 */
const RENDER_BUDGET_MS = Number(process.env.RENDER_BUDGET_MS ?? 18_000);
/** A moment for fonts and the last frame after the widgets have all answered. */
const SETTLE_MS = 400;
/** A path is a line long; anything bigger is not one of ours. */
const MAX_BODY_BYTES = 4096;
/** The paper: landscape A4 suits a two-column dashboard. */
const PAGE = { format: "A4", landscape: true, printBackground: true, margin: { top: "12mm", right: "12mm", bottom: "12mm", left: "12mm" } };
const VIEWPORT = { width: 1280, height: 860, deviceScaleFactor: 1 };

// The same arguments as e2e/screenshots.mjs, kept in step by hand: the two
// directories are built into separate images, so neither imports the other.
const browser = await puppeteer.launch({
  executablePath: process.env.E2E_CHROME ?? "/usr/bin/chromium-browser",
  args: ["--no-sandbox", "--disable-dev-shm-usage", "--disable-crash-reporter", "--disable-crashpad", `--user-data-dir=/tmp/chrome-render-${process.pid}`],
});

/** A request the caller can put right, answered with 400 rather than 502. */
class BadRequest extends Error {}

function pathOf(body) {
  let asked;
  try {
    asked = JSON.parse(body);
  } catch {
    throw new BadRequest("the body is not JSON");
  }
  const path = String(asked?.path ?? "");
  if (!path.startsWith("/") || path.startsWith("//")) throw new BadRequest("a path starts with one slash");
  return path;
}

async function print(path) {
  const deadline = Date.now() + RENDER_BUDGET_MS;
  // What is left of the budget, so no one step can spend it all.
  const left = () => {
    const remaining = deadline - Date.now();
    if (remaining <= 0) throw new Error(`the page took longer than ${RENDER_BUDGET_MS}ms to print`);
    return remaining;
  };
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    await page.setViewport(VIEWPORT);
    page.setDefaultTimeout(left());
    await page.goto(APP_URL + path, { waitUntil: "networkidle0", timeout: left() });
    // Every widget has answered once nothing on the page is still counting.
    await page.waitForFunction(
      () => document.querySelectorAll("[data-widget]").length > 0 && !document.querySelector("[data-widget-loading]") && !document.querySelector("[data-skeleton]"),
      { timeout: left() },
    );
    await new Promise((resolve) => setTimeout(resolve, Math.min(SETTLE_MS, left())));
    return await page.pdf({ ...PAGE, timeout: left() });
  } finally {
    await context.close();
  }
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    let over = false;
    req.on("data", (chunk) => {
      size += chunk.length;
      // Past the cap nothing more is kept, but the rest is read off the socket
      // so the caller hears the refusal rather than a dropped connection.
      if (size > MAX_BODY_BYTES) over = true;
      if (!over) chunks.push(chunk);
    });
    req.on("end", () =>
      over
        ? reject(new BadRequest(`a body longer than ${MAX_BODY_BYTES} bytes is not a path`))
        : resolve(Buffer.concat(chunks).toString("utf8")),
    );
    req.on("error", reject);
  });
}

function answer(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

const server = createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/healthz") {
    // The compose healthcheck reads this: without a browser there is nothing to
    // print, so the container says so rather than taking work it cannot do.
    const connected = browser.connected ?? browser.isConnected();
    answer(res, connected ? 200 : 503, { status: connected ? "ok" : "the browser is gone" });
    return;
  }
  if (req.method !== "POST" || req.url !== "/pdf") {
    answer(res, 404, { error: "not found" });
    return;
  }
  let path;
  try {
    path = pathOf(await readBody(req));
  } catch (error) {
    const bad = error instanceof BadRequest;
    answer(res, bad ? 400 : 502, { error: error.message });
    return;
  }
  try {
    const pdf = await print(path);
    res.writeHead(200, { "Content-Type": "application/pdf", "Content-Length": pdf.length });
    res.end(pdf);
  } catch (error) {
    process.stderr.write(`render failed: ${error.message}\n`);
    answer(res, 502, { error: error.message });
  }
});

server.listen(PORT, "0.0.0.0", () => process.stdout.write(`render service on :${PORT}, printing ${APP_URL}\n`));
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, async () => {
    server.close();
    await browser.close();
    process.exit(0);
  });
}
