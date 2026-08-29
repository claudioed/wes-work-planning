// Verifies planning-mfe renders standalone at :5183 without console errors,
// and that the telemetry/rebalance dashboard + work-unit search actually
// render real data from the live wes-work-planning backend (:8083).
import { chromium } from "playwright";

const errors = [];
const consoleMessages = [];

const browser = await chromium.launch();
const page = await browser.newPage();

page.on("console", (msg) => {
  consoleMessages.push(`[${msg.type()}] ${msg.text()}`);
  if (msg.type() === "error") errors.push(msg.text());
});
page.on("pageerror", (err) => {
  errors.push(`pageerror: ${err.message}`);
});

await page.goto("http://localhost:5183/", { waitUntil: "networkidle" });

// Basic render check
const h1 = await page.textContent("h1");
if (h1?.trim() !== "Planning") {
  throw new Error(`Expected h1 "Planning", got "${h1}"`);
}

// Exercise the path telemetry/rebalance lookup
await page.fill('input[placeholder^="Path ID"]', "pick-a");
await page.click('text=Look up');
await page.waitForSelector('text=Telemetry · pick-a', { timeout: 10000 });
await page.waitForSelector('text=Rebalance recommendation · pick-a', { timeout: 10000 });
const backlogText = await page.textContent("body");
if (!backlogText?.includes("Backlog depth")) {
  throw new Error("Telemetry backlog depth stat did not render");
}

// Exercise the work-unit-by-reference search
await page.fill('input[placeholder^="Reference"]', "mfe-verify-ref-1");
await page.click('text=Search');
await page.waitForSelector('text=wu-mfe-verify-1', { timeout: 10000 });

await browser.close();

console.log("--- console messages ---");
for (const m of consoleMessages) console.log(m);

if (errors.length > 0) {
  console.error("FAIL: console errors detected:");
  for (const e of errors) console.error(" -", e);
  process.exit(1);
}

console.log("PASS: planning-mfe renders standalone with no console errors; telemetry, rebalance, and work-unit search all show live backend data.");
