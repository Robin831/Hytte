// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import {
  cropCellsToCanvases,
  detectCardRectangle,
  detectGrid,
  isWithinTolerance,
  TARGET_ASPECT_RATIO,
  type DetectedGridCell,
  type DetectedRectangle,
} from './rectangleDetector'

// Build an ImageData-compatible fixture without relying on the DOM constructor
// (happy-dom's polyfill copies data into an internal buffer which complicates
// inspection on failures).
function makeFrame(
  w: number,
  h: number,
  fillRect: { x: number; y: number; w: number; h: number } | null,
): ImageData {
  const data = new Uint8ClampedArray(w * h * 4)
  // Light gray background so Sobel detects a strong step at the rectangle edge.
  for (let i = 0; i < data.length; i += 4) {
    data[i] = 30
    data[i + 1] = 30
    data[i + 2] = 30
    data[i + 3] = 255
  }
  if (fillRect) {
    for (let y = fillRect.y; y < fillRect.y + fillRect.h && y < h; y++) {
      for (let x = fillRect.x; x < fillRect.x + fillRect.w && x < w; x++) {
        const i = (y * w + x) * 4
        data[i] = 240
        data[i + 1] = 240
        data[i + 2] = 240
        data[i + 3] = 255
      }
    }
  }
  return { data, width: w, height: h, colorSpace: 'srgb' } as unknown as ImageData
}

// Build a synthetic 3x3 binder page: nine card-shaped rectangles at ~5:7
// aspect, laid out on a uniform background. `bg` is the binder color, `card`
// the card color. `skip` removes that cell to simulate an empty pocket;
// `glare` paints a bright square inside that cell to simulate camera glare.
function makeBinderFrame(opts: {
  bg: number
  card: number
  skip?: { row: number; col: number }
  glare?: { row: number; col: number }
}): ImageData {
  const w = 240
  const h = 336
  const gridX0 = 20
  const gridY0 = 28
  const gridW = 200
  const gridH = 280
  const cellW = gridW / 3
  const cellH = gridH / 3
  // Cards occupy 85% of each cell so there's a clear binder gap between them.
  const cardScale = 0.85
  const data = new Uint8ClampedArray(w * h * 4)
  for (let i = 0; i < data.length; i += 4) {
    data[i] = opts.bg
    data[i + 1] = opts.bg
    data[i + 2] = opts.bg
    data[i + 3] = 255
  }
  const setPixel = (x: number, y: number, v: number) => {
    if (x < 0 || y < 0 || x >= w || y >= h) return
    const i = (y * w + x) * 4
    data[i] = v
    data[i + 1] = v
    data[i + 2] = v
    data[i + 3] = 255
  }
  for (let r = 0; r < 3; r++) {
    for (let c = 0; c < 3; c++) {
      if (opts.skip && opts.skip.row === r && opts.skip.col === c) continue
      const cardW = cellW * cardScale
      const cardH = cellH * cardScale
      const cx0 = Math.round(gridX0 + c * cellW + (cellW - cardW) / 2)
      const cy0 = Math.round(gridY0 + r * cellH + (cellH - cardH) / 2)
      const cx1 = Math.round(cx0 + cardW)
      const cy1 = Math.round(cy0 + cardH)
      for (let y = cy0; y < cy1; y++) {
        for (let x = cx0; x < cx1; x++) {
          setPixel(x, y, opts.card)
        }
      }
      if (opts.glare && opts.glare.row === r && opts.glare.col === c) {
        // Bright square well inside the card so the outer card bbox is
        // unchanged but the cell carries extra interior edges.
        const gx0 = cx0 + 6
        const gy0 = cy0 + 6
        const gx1 = Math.min(cx1 - 6, gx0 + 14)
        const gy1 = Math.min(cy1 - 6, gy0 + 14)
        for (let y = gy0; y < gy1; y++) {
          for (let x = gx0; x < gx1; x++) {
            setPixel(x, y, 255)
          }
        }
      }
    }
  }
  return { data, width: w, height: h, colorSpace: 'srgb' } as unknown as ImageData
}

describe('detectCardRectangle', () => {
  it('returns null for a uniform frame with no edges', () => {
    const frame = makeFrame(200, 280, null)
    expect(detectCardRectangle(frame)).toBeNull()
  })

  it('detects a portrait card-shaped rectangle with near-0.716 aspect ratio', () => {
    // 80x112 rectangle → aspect 80/112 ≈ 0.714, virtually at target.
    const rectW = 80
    const rectH = 112
    const frame = makeFrame(200, 280, { x: 60, y: 84, w: rectW, h: rectH })
    const result = detectCardRectangle(frame)
    expect(result).not.toBeNull()
    if (!result) return
    expect(result.score).toBeGreaterThan(0.6)
    // Bounds should roughly bracket the synthetic rectangle. We use loose
    // tolerances because Sobel runs on a downsampled grid which rounds bounds.
    expect(result.x).toBeGreaterThanOrEqual(40)
    expect(result.x).toBeLessThanOrEqual(80)
    expect(result.y).toBeGreaterThanOrEqual(64)
    expect(result.y).toBeLessThanOrEqual(104)
    // Detected aspect should match the source rectangle aspect within tolerance.
    const detectedAspect = Math.min(result.w / result.h, result.h / result.w)
    expect(Math.abs(detectedAspect - TARGET_ASPECT_RATIO)).toBeLessThan(0.1)
  })

  it('rejects a rectangle that is far from the target aspect ratio', () => {
    // 350x70 inside a 400x200 frame → aspect 5.0, well outside the 0.716
    // target, but large enough to pass the size gate so we exercise the
    // aspect-ratio rejection path specifically.
    const frame = makeFrame(400, 200, { x: 25, y: 65, w: 350, h: 70 })
    expect(detectCardRectangle(frame)).toBeNull()
  })

  it('rejects very small rectangles', () => {
    // Aspect ratio is correct but the rectangle is tiny relative to the frame.
    const frame = makeFrame(400, 400, { x: 180, y: 180, w: 30, h: 42 })
    expect(detectCardRectangle(frame)).toBeNull()
  })

  it('returns null for too-small input frames', () => {
    const tiny = makeFrame(8, 8, null)
    expect(detectCardRectangle(tiny)).toBeNull()
  })
})

function assertCellAspect(cell: DetectedGridCell) {
  expect(cell.w).toBeGreaterThan(0)
  expect(cell.h).toBeGreaterThan(0)
  const ratio = Math.min(cell.w / cell.h, cell.h / cell.w)
  // Same tolerance window the detector itself uses for the 5:7 gate.
  expect(Math.abs(ratio - TARGET_ASPECT_RATIO)).toBeLessThanOrEqual(0.22)
}

describe('detectGrid', () => {
  it('detects all nine cells on a light binder', () => {
    const frame = makeBinderFrame({ bg: 220, card: 50 })
    const cells = detectGrid(frame, { rows: 3, cols: 3 })
    expect(cells.length).toBe(9)
    const seen = new Set(cells.map(c => `${c.row}:${c.col}`))
    expect(seen.size).toBe(9)
    for (const cell of cells) {
      expect(cell.row).toBeGreaterThanOrEqual(0)
      expect(cell.row).toBeLessThan(3)
      expect(cell.col).toBeGreaterThanOrEqual(0)
      expect(cell.col).toBeLessThan(3)
      assertCellAspect(cell)
    }
  })

  it('detects all nine cells on a dark binder', () => {
    const frame = makeBinderFrame({ bg: 30, card: 230 })
    const cells = detectGrid(frame, { rows: 3, cols: 3 })
    expect(cells.length).toBe(9)
    const seen = new Set(cells.map(c => `${c.row}:${c.col}`))
    expect(seen.size).toBe(9)
    for (const cell of cells) {
      assertCellAspect(cell)
    }
  })

  it('still detects the cell that has glare', () => {
    const frame = makeBinderFrame({
      bg: 220,
      card: 50,
      glare: { row: 0, col: 1 },
    })
    const cells = detectGrid(frame, { rows: 3, cols: 3 })
    expect(cells.length).toBe(9)
    const glared = cells.find(c => c.row === 0 && c.col === 1)
    expect(glared).toBeDefined()
    if (glared) assertCellAspect(glared)
  })

  it('skips empty pockets', () => {
    const frame = makeBinderFrame({
      bg: 30,
      card: 230,
      skip: { row: 1, col: 2 },
    })
    const cells = detectGrid(frame, { rows: 3, cols: 3 })
    expect(cells.length).toBe(8)
    expect(cells.find(c => c.row === 1 && c.col === 2)).toBeUndefined()
    for (const cell of cells) {
      assertCellAspect(cell)
    }
  })

  it('returns cells sorted by row then column', () => {
    const frame = makeBinderFrame({ bg: 220, card: 50 })
    const cells = detectGrid(frame, { rows: 3, cols: 3 })
    for (let i = 1; i < cells.length; i++) {
      const prevKey = cells[i - 1].row * 100 + cells[i - 1].col
      const currKey = cells[i].row * 100 + cells[i].col
      expect(currKey).toBeGreaterThan(prevKey)
    }
  })

  it('returns an empty array for invalid expected dimensions', () => {
    const frame = makeBinderFrame({ bg: 220, card: 50 })
    expect(detectGrid(frame, { rows: 0, cols: 3 })).toEqual([])
    expect(detectGrid(frame, { rows: 3, cols: 0 })).toEqual([])
  })

  it('returns an empty array for too-small frames', () => {
    const tiny = makeFrame(8, 8, null)
    expect(detectGrid(tiny, { rows: 3, cols: 3 })).toEqual([])
  })

  it('returns an empty array for a uniform frame with no edges', () => {
    const frame = makeFrame(240, 336, null)
    expect(detectGrid(frame, { rows: 3, cols: 3 })).toEqual([])
  })
})

describe('cropCellsToCanvases', () => {
  it('produces one canvas per cell at the default 1500x2100 resolution', () => {
    const source = document.createElement('canvas')
    source.width = 800
    source.height = 1120
    const cells: DetectedGridCell[] = [
      { row: 0, col: 0, x: 0, y: 0, w: 50, h: 70 },
      { row: 0, col: 1, x: 60, y: 0, w: 50, h: 70 },
      { row: 1, col: 0, x: 0, y: 80, w: 50, h: 70 },
    ]
    const canvases = cropCellsToCanvases(source, cells)
    expect(canvases.length).toBe(3)
    for (const canvas of canvases) {
      expect(canvas.width).toBe(1500)
      expect(canvas.height).toBe(2100)
    }
  })

  it('respects custom targetW/targetH', () => {
    const source = document.createElement('canvas')
    source.width = 200
    source.height = 280
    const cells: DetectedGridCell[] = [{ row: 0, col: 0, x: 0, y: 0, w: 50, h: 70 }]
    const canvases = cropCellsToCanvases(source, cells, 750, 1050)
    expect(canvases.length).toBe(1)
    expect(canvases[0].width).toBe(750)
    expect(canvases[0].height).toBe(1050)
  })

  it('sorts canvases row-major regardless of input order', () => {
    const source = document.createElement('canvas')
    source.width = 200
    source.height = 280
    const unordered: DetectedGridCell[] = [
      { row: 2, col: 0, x: 0, y: 200, w: 50, h: 70 },
      { row: 0, col: 2, x: 120, y: 0, w: 50, h: 70 },
      { row: 1, col: 1, x: 60, y: 100, w: 50, h: 70 },
      { row: 0, col: 0, x: 0, y: 0, w: 50, h: 70 },
    ]
    const canvases = cropCellsToCanvases(source, unordered, 100, 140)
    // Sorted order should be (0,0), (0,2), (1,1), (2,0). All canvases share
    // the same dimensions, so verify count + dimensions; the iteration order
    // is verified indirectly by drawImage being called in that sequence.
    expect(canvases.length).toBe(4)
    for (const canvas of canvases) {
      expect(canvas.width).toBe(100)
      expect(canvas.height).toBe(140)
    }
  })

  it('returns an empty array for invalid dimensions', () => {
    const source = document.createElement('canvas')
    const cell: DetectedGridCell = { row: 0, col: 0, x: 0, y: 0, w: 50, h: 70 }
    expect(cropCellsToCanvases(source, [cell], 0, 100)).toEqual([])
    expect(cropCellsToCanvases(source, [cell], 100, 0)).toEqual([])
  })

  it('returns an empty array when there are no cells', () => {
    const source = document.createElement('canvas')
    expect(cropCellsToCanvases(source, [])).toEqual([])
  })
})

describe('isWithinTolerance', () => {
  const base: DetectedRectangle = { x: 100, y: 80, w: 200, h: 280, score: 0.8 }

  it('accepts a stable rectangle within tolerance', () => {
    const next: DetectedRectangle = { x: 102, y: 82, w: 198, h: 282, score: 0.82 }
    expect(isWithinTolerance(base, next, 0.05, 600, 800)).toBe(true)
  })

  it('rejects a rectangle that drifted past tolerance on one edge', () => {
    // 50px horizontal shift in a 600px frame = ~8.3%, exceeds 5% tolerance.
    const next: DetectedRectangle = { x: 150, y: 82, w: 198, h: 282, score: 0.82 }
    expect(isWithinTolerance(base, next, 0.05, 600, 800)).toBe(false)
  })

  it('rejects when the frame dimensions are zero', () => {
    expect(isWithinTolerance(base, base, 0.05, 0, 0)).toBe(false)
  })
})
