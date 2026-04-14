#!/usr/bin/env node
/* eslint-disable no-console */
/**
 * record-demo.js — Captures a screen-recording of the demo path into a
 * webm file (1280×800, ~25fps, 50–90 seconds). Upload the webm to
 * Loom / YouTube / wherever, or convert to mp4 with:
 *
 *   ffmpeg -i demo.webm -c:v libx264 -crf 22 demo.mp4
 *
 * Prerequisites:
 *   scripts/run-local.sh start    # dashboard at :3003, analyzer :18081
 *   cd src/dashboard && npx playwright install chromium  (first run only)
 *
 * Run from repo root:
 *   node scripts/record-demo.js [--slow] [--output /path/to/out.webm]
 *
 * Flags:
 *   --slow     Add longer pauses between steps (~2× runtime, nicer for narration)
 *   --headed   Show the browser window (default: headless)
 *   --output   Destination webm file (default: ./demo.webm under repo root)
 *   --base     Dashboard base URL (default: http://localhost:3003)
 */

const { chromium } = require(require('path').join(
  __dirname, '..', 'src', 'dashboard', 'node_modules', 'playwright'
));
const fs = require('fs');
const path = require('path');

const args = process.argv.slice(2);
const flag = name => args.includes(`--${name}`);
const arg  = (name, def) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 && args[i + 1] ? args[i + 1] : def;
};

const SLOW     = flag('slow');
const HEADLESS = !flag('headed');
const BASE     = arg('base', 'http://localhost:3003');
const OUTPUT   = path.resolve(arg('output', path.join(__dirname, '..', 'demo.webm')));
const TMPDIR   = path.join(require('os').tmpdir(), `cluster-demo-${Date.now()}`);

const PAUSE = SLOW ? 3500 : 1600;     // between steps
const SHORT = SLOW ? 1500 : 700;      // between sub-actions
const VIEW  = { width: 1280, height: 800 };

function log(step, msg) { console.log(`[${String(step).padStart(2, '0')}] ${msg}`); }

(async () => {
  console.log(`record-demo.js — capturing demo path`);
  console.log(`  base:     ${BASE}`);
  console.log(`  output:   ${OUTPUT}`);
  console.log(`  speed:    ${SLOW ? 'slow (narration-friendly)' : 'fast'}`);
  console.log(`  headless: ${HEADLESS}`);
  console.log('');

  fs.mkdirSync(TMPDIR, { recursive: true });

  const browser = await chromium.launch({ headless: HEADLESS });
  const ctx = await browser.newContext({
    viewport: VIEW,
    recordVideo: { dir: TMPDIR, size: VIEW },
  });
  const page = await ctx.newPage();

  try {
    // -- 1. Dashboard first paint
    log(1, 'opening dashboard home');
    await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    // Wait for real content — score cards, which appear after /api/v1/health resolves
    await page.waitForSelector('text=Overall Health', { timeout: 10_000 });
    await page.waitForTimeout(PAUSE);

    // -- 2. Expand score breakdown
    log(2, 'expanding score breakdown');
    const showBreakdown = page.getByRole('button', { name: /Show score breakdown/i });
    if (await showBreakdown.count()) {
      await showBreakdown.click();
      await page.waitForTimeout(PAUSE);
    } else {
      log(2, '  (no breakdown toggle — scores may be null; skipping)');
    }

    // -- 3. Click the first factor to trigger Level-3 drill
    log(3, 'drilling into first factor');
    const chevronRow = page.locator('button:has(svg.lucide-trending-down)').first();
    if (await chevronRow.count()) {
      await chevronRow.click();
      await page.waitForTimeout(PAUSE);

      // Scroll the panel so the full resource list is visible
      await page.keyboard.press('End');
      await page.waitForTimeout(SHORT);
      await page.keyboard.press('Home');
      await page.waitForTimeout(SHORT);
    }

    // -- 4. Navigate to Workloads → Pods to show the radar-style list
    log(4, 'opening Workloads → Pods');
    await page.goto(BASE + '/workloads/pods?group=core&version=v1', {
      waitUntil: 'domcontentloaded',
    });
    await page.waitForTimeout(PAUSE);

    // -- 5. Click the first pod to open detail page
    log(5, 'opening first pod detail');
    const firstPod = page.locator('a[href*="/workloads/pods/"]').first();
    if (await firstPod.count()) {
      await firstPod.click();
      await page.waitForTimeout(PAUSE);
    }

    // -- 6. Score Impact tab
    log(6, 'opening Score Impact tab');
    const scoreImpactTab = page.getByRole('tab', { name: /Score Impact/i });
    if (await scoreImpactTab.count()) {
      await scoreImpactTab.click();
      await page.waitForTimeout(PAUSE);
    }

    // -- 7. Logs tab → teleprompter scroll in action
    log(7, 'opening Logs tab');
    const logsTab = page.getByRole('tab', { name: /^Logs$/i });
    if (await logsTab.count()) {
      await logsTab.click();
      await page.waitForTimeout(PAUSE);
    }

    // -- 8. Incidents
    log(8, 'navigating to Incidents');
    await page.goto(BASE + '/incidents', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(PAUSE);

    // Open first incident if any
    const firstIncident = page.locator('a[href^="/incidents/"]').first();
    if (await firstIncident.count()) {
      log(8, 'opening first incident');
      await firstIncident.click();
      await page.waitForTimeout(PAUSE);
    }

    // -- 9. Optimization
    log(9, 'navigating to Optimization');
    await page.goto(BASE + '/optimization', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(PAUSE);

    // -- 10. Back to home (circular narrative)
    log(10, 'returning to home');
    await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(PAUSE);

    console.log('');
    console.log('demo path complete, finalising video…');
  } catch (e) {
    console.error('error during demo path:', e.message);
  }

  const video = page.video();
  await ctx.close();
  await browser.close();

  // Move the auto-named video to the requested output path
  const recorded = await video.path();
  fs.renameSync(recorded, OUTPUT);
  fs.rmSync(TMPDIR, { recursive: true, force: true });

  const sizeMB = (fs.statSync(OUTPUT).size / 1024 / 1024).toFixed(1);
  console.log('');
  console.log(`✓ recorded ${sizeMB} MB → ${OUTPUT}`);
  console.log('');
  console.log('Upload to Loom:    https://www.loom.com/ (use "Upload video")');
  console.log('Upload to YouTube: https://studio.youtube.com/');
  console.log(`Convert to mp4:    ffmpeg -i "${OUTPUT}" -c:v libx264 -crf 22 "${OUTPUT.replace(/\.webm$/, '.mp4')}"`);
})();
