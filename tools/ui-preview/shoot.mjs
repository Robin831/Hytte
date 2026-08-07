// Screenshot pages of the ui-preview instance at desktop + mobile widths.
//
//   node tools/ui-preview/shoot.mjs '/wordfeud?tab=board' [more paths...]
//
// Env: PREVIEW_PORT (default 8095), PREVIEW_DIR (default /tmp/hytte-ui-preview),
// SHOT_DIR (default $PREVIEW_DIR/shots).
// Requires playwright + its chromium (see README.md).
import { chromium } from 'playwright'
import { readFileSync, mkdirSync } from 'fs'

const previewDir = process.env.PREVIEW_DIR ?? '/tmp/hytte-ui-preview'
const port = process.env.PREVIEW_PORT ?? '8095'
const shotDir = process.env.SHOT_DIR ?? `${previewDir}/shots`
const base = `http://localhost:${port}`
const token = readFileSync(`${previewDir}/token`, 'utf8').trim()

const paths = process.argv.slice(2)
if (paths.length === 0) {
  console.error("usage: node shoot.mjs '/wordfeud?tab=board' [path...]")
  process.exit(1)
}
mkdirSync(shotDir, { recursive: true })

const viewports = [
  { tag: 'desktop', width: 1440, height: 900 },
  { tag: 'mobile', width: 390, height: 844 },
]

const browser = await chromium.launch()
try {
  for (const path of paths) {
    for (const vp of viewports) {
      const ctx = await browser.newContext({
        viewport: { width: vp.width, height: vp.height },
        deviceScaleFactor: 2,
      })
      await ctx.addCookies([{ name: 'session', value: token, url: base }])
      const page = await ctx.newPage()
      await page.goto(base + path, { waitUntil: 'networkidle' })
      await page.waitForTimeout(1500) // let auto-loading views settle
      const slug = path.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '')
      const file = `${shotDir}/${slug}-${vp.tag}.png`
      // Note: position of fixed elements (e.g. the mobile hamburger) is not
      // reliable in fullPage captures — they paint once at final scroll.
      await page.screenshot({ path: file, fullPage: true })
      console.log('captured', file)
      await ctx.close()
    }
  }
} finally {
  await browser.close()
}
