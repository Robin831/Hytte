// Pure helpers for detecting a Pokémon-card-shaped rectangle in an `ImageData`
// frame. Kept free of React/DOM lifecycle so it can be exercised in unit tests
// with synthetic frames.

export interface DetectedRectangle {
  x: number
  y: number
  w: number
  h: number
  score: number
}

export type RectangleDetectorStatus = 'searching' | 'candidate' | 'locked' | 'captured'

export interface RectangleDetectorState {
  status: RectangleDetectorStatus
  bounds: DetectedRectangle | null
  ticks: number
}

// Pokémon TCG cards are 63x88mm — width/height ≈ 0.716.
export const TARGET_ASPECT_RATIO = 0.716
// Maximum allowed deviation from TARGET_ASPECT_RATIO before a candidate is rejected.
const ASPECT_TOLERANCE = 0.18
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

export function detectCardRectangle(image: ImageData): DetectedRectangle | null {
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

  // 3x3 Sobel — produces |Gx| + |Gy| (cheap approximation of magnitude).
  let edgeCount = 0
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
        edgeCount++
      }
    }
  }

  // Reject frames where edges are too sparse or saturate — both signal that
  // there isn't a single dominant rectangular object in view.
  const pixelCount = (dw - 2) * (dh - 2)
  if (edgeCount < pixelCount * 0.01) return null
  if (edgeCount > pixelCount * 0.6) return null

  edgesX.sort((a, b) => a - b)
  edgesY.sort((a, b) => a - b)

  const loIdx = Math.floor(edgesX.length * PERCENTILE_LO)
  const hiIdx = Math.min(edgesX.length - 1, Math.floor(edgesX.length * PERCENTILE_HI))
  const x0 = edgesX[loIdx]
  const x1 = edgesX[hiIdx]
  const y0 = edgesY[loIdx]
  const y1 = edgesY[hiIdx]
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

  return {
    x: Math.max(0, Math.round(x0 / scale)),
    y: Math.max(0, Math.round(y0 / scale)),
    w: Math.min(srcW, Math.round(rectW / scale)),
    h: Math.min(srcH, Math.round(rectH / scale)),
    score,
  }
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
