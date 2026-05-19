// Pure helpers for detecting Pokémon-card-shaped rectangles in an `ImageData`
// frame. Kept free of React/DOM lifecycle so it can be exercised in unit tests
// with synthetic frames.

export interface DetectedRectangle {
  x: number
  y: number
  w: number
  h: number
  score: number
}

export interface DetectedGridCell {
  row: number
  col: number
  x: number
  y: number
  w: number
  h: number
}

export type RectangleDetectorStatus = 'searching' | 'candidate' | 'locked' | 'captured'

export interface RectangleDetectorState {
  status: RectangleDetectorStatus
  bounds: DetectedRectangle | null
  ticks: number
}

// Pokémon TCG cards are 63x88mm — width/height ≈ 0.716.
export const TARGET_ASPECT_RATIO = 0.716
// Maximum allowed deviation from TARGET_ASPECT_RATIO before a single-card
// candidate is rejected.
const ASPECT_TOLERANCE = 0.18
// Wider tolerance for grid cells. A binder page is often photographed at a
// slight angle; up to ~5° of rotational skew shifts each card's axis-aligned
// bounding box by a few pixels, which the per-cell percentile bbox reads as
// extra aspect-ratio noise.
const GRID_ASPECT_TOLERANCE = 0.22
// Approximate width (in pixels) of the small grid we downsample to before
// running Sobel. ~80 keeps CPU work tiny while still revealing card edges.
const DOWNSAMPLE_WIDTH = 80
// Sobel magnitude threshold (sum-of-absolute-gradients, 0..510 range).
const EDGE_THRESHOLD = 90
// Reject detections below this combined score.
const MIN_SCORE = 0.55
// Reject detections whose shorter side is below this fraction of the
// downsampled frame's shorter side — too small to be a card the user is
// trying to scan.
const MIN_RECT_FRACTION = 0.25
// Reject detections that fill almost the entire frame (likely the table /
// background dominating the edge map).
const MAX_RECT_FRACTION = 0.97
// Robust percentile bounds for edge-pixel bounding box.
const PERCENTILE_LO = 0.05
const PERCENTILE_HI = 0.95

// Default output resolution for grid-cell crops. Matches the resolution the
// single-card scanner already feeds into ingestion (5:7 aspect at ~1500×2100),
// so downstream code can treat both single-card and per-cell crops the same.
const DEFAULT_CROP_W = 1500
const DEFAULT_CROP_H = 2100

interface EdgeMap {
  // Paired downsampled-coordinate arrays — edgesX[k]/edgesY[k] is one edge pixel.
  edgesX: number[]
  edgesY: number[]
  dw: number
  dh: number
  scale: number
}

// Downsample the frame, run a 3x3 Sobel pass, and return the coordinates of
// pixels whose gradient magnitude crosses EDGE_THRESHOLD. Shared by the
// single-card detector and the grid detector so both stages see the same edge
// pixels.
function computeEdgeMap(image: ImageData): EdgeMap | null {
  const srcW = image.width
  const srcH = image.height
  if (srcW < 16 || srcH < 16) return null

  const scale = DOWNSAMPLE_WIDTH / Math.max(srcW, srcH)
  const dw = Math.max(16, Math.round(srcW * scale))
  const dh = Math.max(16, Math.round(srcH * scale))

  const gray = new Uint8Array(dw * dh)
  const data = image.data
  for (let y = 0; y < dh; y++) {
    const sy = Math.min(srcH - 1, Math.floor(y / scale))
    for (let x = 0; x < dw; x++) {
      const sx = Math.min(srcW - 1, Math.floor(x / scale))
      const idx = (sy * srcW + sx) * 4
      gray[y * dw + x] = (data[idx] * 0.299 + data[idx + 1] * 0.587 + data[idx + 2] * 0.114) | 0
    }
  }

  const edgesX: number[] = []
  const edgesY: number[] = []
  for (let y = 1; y < dh - 1; y++) {
    for (let x = 1; x < dw - 1; x++) {
      const a = gray[(y - 1) * dw + (x - 1)]
      const b = gray[(y - 1) * dw + x]
      const c = gray[(y - 1) * dw + (x + 1)]
      const d = gray[y * dw + (x - 1)]
      const f = gray[y * dw + (x + 1)]
      const g = gray[(y + 1) * dw + (x - 1)]
      const h = gray[(y + 1) * dw + x]
      const i = gray[(y + 1) * dw + (x + 1)]
      const gx = -a + c - 2 * d + 2 * f - g + i
      const gy = -a - 2 * b - c + g + 2 * h + i
      const mag = Math.abs(gx) + Math.abs(gy)
      if (mag >= EDGE_THRESHOLD) {
        edgesX.push(x)
        edgesY.push(y)
      }
    }
  }

  return { edgesX, edgesY, dw, dh, scale }
}

export function detectCardRectangle(image: ImageData): DetectedRectangle | null {
  const map = computeEdgeMap(image)
  if (!map) return null
  const { edgesX, edgesY, dw, dh, scale } = map
  const srcW = image.width
  const srcH = image.height
  const edgeCount = edgesX.length

  // Reject frames where edges are too sparse or saturate — both signal that
  // there isn't a single dominant rectangular object in view.
  const pixelCount = (dw - 2) * (dh - 2)
  if (edgeCount < pixelCount * 0.01) return null
  if (edgeCount > pixelCount * 0.6) return null

  const sortedX = edgesX.slice().sort((a, b) => a - b)
  const sortedY = edgesY.slice().sort((a, b) => a - b)

  const loIdx = Math.floor(sortedX.length * PERCENTILE_LO)
  const hiIdx = Math.min(sortedX.length - 1, Math.floor(sortedX.length * PERCENTILE_HI))
  const x0 = sortedX[loIdx]
  const x1 = sortedX[hiIdx]
  const y0 = sortedY[loIdx]
  const y1 = sortedY[hiIdx]
  const rectW = x1 - x0
  const rectH = y1 - y0
  if (rectW <= 0 || rectH <= 0) return null

  const minSide = Math.min(dw, dh)
  if (Math.min(rectW, rectH) < minSide * MIN_RECT_FRACTION) return null
  if (rectW > dw * MAX_RECT_FRACTION && rectH > dh * MAX_RECT_FRACTION) return null

  const aspectScore = aspectRatioScore(rectW, rectH)
  if (aspectScore <= 0) return null

  // Density: fraction of edge pixels that fall inside the inferred bounds.
  // A genuine rectangle puts most of its edges along its perimeter (and
  // possibly interior text/art), all of which land inside the box.
  let inside = 0
  for (let k = 0; k < edgesX.length; k++) {
    const ex = edgesX[k]
    const ey = edgesY[k]
    if (ex >= x0 && ex <= x1 && ey >= y0 && ey <= y1) inside++
  }
  const densityScore = inside / edgesX.length

  const score = aspectScore * 0.6 + densityScore * 0.4
  if (score < MIN_SCORE) return null

  const rx = Math.max(0, Math.round(x0 / scale))
  const ry = Math.max(0, Math.round(y0 / scale))
  return {
    x: rx,
    y: ry,
    w: Math.min(srcW - rx, Math.round(rectW / scale)),
    h: Math.min(srcH - ry, Math.round(rectH / scale)),
    score,
  }
}

// Find a rows×cols grid of card-shaped rectangles in a binder-page frame.
//
// Algorithm:
//   1. Run the shared Sobel pass over a downsampled copy of the frame.
//   2. Take the percentile bounding box of all edge pixels as the page bbox.
//      This trims stray background edges and survives ~5° page skew (the
//      axis-aligned hull still contains the rotated grid).
//   3. Bucket each edge pixel into the (row, col) cell of a uniform grid that
//      subdivides the page bbox. Card edges fall inside their own bucket; the
//      shared boundaries between adjacent cards are split across both buckets,
//      which is fine because we only use the bucket to localise per-card edges.
//   4. For each bucket: skip empty pockets via an adaptive edge-count gate,
//      derive a tight per-card bbox via the same percentile method, and reject
//      cells whose aspect ratio is far from 5:7.
//   5. Translate accepted cells back to source-image coordinates.
export function detectGrid(
  image: ImageData,
  expected: { rows: number; cols: number },
): DetectedGridCell[] {
  if (expected.rows <= 0 || expected.cols <= 0) return []
  const map = computeEdgeMap(image)
  if (!map) return []
  const { edgesX, edgesY, scale } = map
  if (edgesX.length === 0) return []

  const sortedX = edgesX.slice().sort((a, b) => a - b)
  const sortedY = edgesY.slice().sort((a, b) => a - b)
  const loIdx = Math.floor(sortedX.length * PERCENTILE_LO)
  const hiIdx = Math.min(sortedX.length - 1, Math.floor(sortedX.length * PERCENTILE_HI))
  const pageX0 = sortedX[loIdx]
  const pageX1 = sortedX[hiIdx]
  const pageY0 = sortedY[loIdx]
  const pageY1 = sortedY[hiIdx]
  const pageW = pageX1 - pageX0
  const pageH = pageY1 - pageY0
  if (pageW < expected.cols || pageH < expected.rows) return []

  const cellW = pageW / expected.cols
  const cellH = pageH / expected.rows

  interface Bucket {
    ex: number[]
    ey: number[]
  }
  const buckets: Bucket[][] = []
  for (let r = 0; r < expected.rows; r++) {
    const row: Bucket[] = []
    for (let c = 0; c < expected.cols; c++) row.push({ ex: [], ey: [] })
    buckets.push(row)
  }
  for (let k = 0; k < edgesX.length; k++) {
    const ex = edgesX[k]
    const ey = edgesY[k]
    if (ex < pageX0 || ex > pageX1 || ey < pageY0 || ey > pageY1) continue
    const c = Math.min(expected.cols - 1, Math.floor((ex - pageX0) / cellW))
    const r = Math.min(expected.rows - 1, Math.floor((ey - pageY0) / cellH))
    buckets[r][c].ex.push(ex)
    buckets[r][c].ey.push(ey)
  }

  // Empty-pocket gate: use the median bucket size so the threshold scales with
  // however dense the card art happens to be. The absolute floor of 8 guards
  // against noise spuriously satisfying an unrealistically small median.
  const counts = buckets.flat().map(b => b.ex.length)
  const sortedCounts = counts.slice().sort((a, b) => a - b)
  const median = sortedCounts[Math.floor(sortedCounts.length / 2)]
  const minEdgeCount = Math.max(8, Math.floor(median * 0.25))

  const srcW = image.width
  const srcH = image.height
  const cells: DetectedGridCell[] = []
  for (let r = 0; r < expected.rows; r++) {
    for (let c = 0; c < expected.cols; c++) {
      const bucket = buckets[r][c]
      if (bucket.ex.length < minEdgeCount) continue

      const sx = bucket.ex.slice().sort((a, b) => a - b)
      const sy = bucket.ey.slice().sort((a, b) => a - b)
      const li = Math.floor(sx.length * PERCENTILE_LO)
      const hi = Math.min(sx.length - 1, Math.floor(sx.length * PERCENTILE_HI))
      const cx0 = sx[li]
      const cx1 = sx[hi]
      const cy0 = sy[li]
      const cy1 = sy[hi]
      const cw = cx1 - cx0
      const ch = cy1 - cy0
      if (cw <= 0 || ch <= 0) continue

      const ratio = Math.min(cw / ch, ch / cw)
      if (Math.abs(ratio - TARGET_ASPECT_RATIO) > GRID_ASPECT_TOLERANCE) continue

      const rx = Math.max(0, Math.round(cx0 / scale))
      const ry = Math.max(0, Math.round(cy0 / scale))
      cells.push({
        row: r,
        col: c,
        x: rx,
        y: ry,
        w: Math.min(srcW - rx, Math.round(cw / scale)),
        h: Math.min(srcH - ry, Math.round(ch / scale)),
      })
    }
  }

  cells.sort((a, b) => a.row - b.row || a.col - b.col)
  return cells
}

// Draw each detected cell into its own canvas at the single-card scanner's
// effective resolution. Returned canvases are sorted by (row, col) so the
// caller can pair them with the corresponding grid position.
export function cropCellsToCanvases(
  source: HTMLCanvasElement,
  cells: DetectedGridCell[],
  targetW: number = DEFAULT_CROP_W,
  targetH: number = DEFAULT_CROP_H,
): HTMLCanvasElement[] {
  if (targetW <= 0 || targetH <= 0) return []
  const ordered = cells.slice().sort((a, b) => a.row - b.row || a.col - b.col)
  const canvases: HTMLCanvasElement[] = []
  for (const cell of ordered) {
    const canvas = document.createElement('canvas')
    canvas.width = targetW
    canvas.height = targetH
    const ctx = canvas.getContext('2d')
    if (ctx && typeof ctx.drawImage === 'function' && cell.w > 0 && cell.h > 0) {
      try {
        ctx.drawImage(source, cell.x, cell.y, cell.w, cell.h, 0, 0, targetW, targetH)
      } catch {
        // Best-effort: a tainted source or environment without a working 2D
        // context shouldn't crash the whole capture flow — surface an empty
        // canvas and let the caller decide.
      }
    }
    canvases.push(canvas)
  }
  return canvases
}

// Returns a [0..1] score that peaks at TARGET_ASPECT_RATIO. Accepts the
// rectangle in either orientation (portrait or landscape) by considering
// `min(w/h, h/w)`.
function aspectRatioScore(w: number, h: number): number {
  const ratio = Math.min(w / h, h / w)
  const diff = Math.abs(ratio - TARGET_ASPECT_RATIO)
  return Math.max(0, 1 - diff / ASPECT_TOLERANCE)
}

// True when `curr` is within `tolerance` fraction of `prev` on every edge.
// Tolerance is expressed relative to the frame (canvasW/canvasH) so a small
// translation of a large rectangle counts the same as the same translation of
// a small rectangle.
export function isWithinTolerance(
  prev: DetectedRectangle,
  curr: DetectedRectangle,
  tolerance: number,
  canvasW: number,
  canvasH: number,
): boolean {
  if (canvasW <= 0 || canvasH <= 0) return false
  const dx = Math.abs(prev.x - curr.x) / canvasW
  const dy = Math.abs(prev.y - curr.y) / canvasH
  const dw = Math.abs(prev.w - curr.w) / canvasW
  const dh = Math.abs(prev.h - curr.h) / canvasH
  return dx <= tolerance && dy <= tolerance && dw <= tolerance && dh <= tolerance
}
