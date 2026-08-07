// Layout overflow probe for a ui-preview page: reports document vs viewport
// width (docWidth > viewport means horizontal overflow) and lists elements
// wider than a threshold to find what refuses to shrink.
//
//   node tools/ui-preview/measure.mjs '/wordfeud?tab=board' [viewportWidth]
//
// Env: PREVIEW_PORT, PREVIEW_DIR — as in shoot.mjs.
import { chromium } from 'playwright'
import { readFileSync } from 'fs'

const previewDir = process.env.PREVIEW_DIR ?? '/tmp/hytte-ui-preview'
const port = process.env.PREVIEW_PORT ?? '8095'
const base = `http://localhost:${port}`
const token = readFileSync(`${previewDir}/token`, 'utf8').trim()

const path = process.argv[2]
const width = Number(process.argv[3] ?? 1440)
if (!path) {
  console.error("usage: node measure.mjs '/wordfeud?tab=board' [viewportWidth]")
  process.exit(1)
}

const browser = await chromium.launch()
try {
  const ctx = await browser.newContext({ viewport: { width, height: 900 } })
  await ctx.addCookies([{ name: 'session', value: token, url: base }])
  const page = await ctx.newPage()
  await page.goto(base + path, { waitUntil: 'networkidle' })
  const info = await page.evaluate(() => {
    const wide = []
    document.querySelectorAll('*').forEach(el => {
      const r = el.getBoundingClientRect()
      if (r.width > window.innerWidth * 0.7) {
        wide.push({
          tag: el.tagName,
          cls: String(el.className).slice(0, 90),
          w: Math.round(r.width),
        })
      }
    })
    return {
      docWidth: document.documentElement.scrollWidth,
      viewport: window.innerWidth,
      overflow: document.documentElement.scrollWidth > window.innerWidth,
      wide: wide.slice(0, 20),
    }
  })
  console.log(JSON.stringify(info, null, 1))
} finally {
  await browser.close()
}
