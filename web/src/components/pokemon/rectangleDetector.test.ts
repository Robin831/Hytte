// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import {
  detectCardRectangle,
  isWithinTolerance,
  TARGET_ASPECT_RATIO,
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
