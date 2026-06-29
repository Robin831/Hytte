// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// videoFilters caches its one-time canvas-capability probe at module scope, so
// each test resets the module registry and re-imports after stubbing the
// environment it wants to exercise.

interface FakeCtx {
  filter: string
  fillStyle: string
  save: () => void
  restore: () => void
  drawImage: () => void
  fillRect: () => void
  beginPath: () => void
  moveTo: () => void
  lineTo: () => void
  arcTo: () => void
  arc: () => void
  closePath: () => void
  fill: () => void
}

function makeCtx(): FakeCtx {
  return {
    filter: 'none',
    fillStyle: '#000',
    save: () => {},
    restore: () => {},
    drawImage: () => {},
    fillRect: () => {},
    beginPath: () => {},
    moveTo: () => {},
    lineTo: () => {},
    arcTo: () => {},
    arc: () => {},
    closePath: () => {},
    fill: () => {},
  }
}

// stubCanvasSupport installs a canvas/video factory. `captureStream` controls
// whether the pipeline is considered supported.
function stubCanvasSupport(opts: { captureStream: boolean }): { trackStop: ReturnType<typeof vi.fn> } {
  const trackStop = vi.fn()
  const videoTrack = { kind: 'video', stop: trackStop } as unknown as MediaStreamTrack
  const captured = { getVideoTracks: () => [videoTrack] }
  const realCreate = document.createElement.bind(document)
  vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    if (tag === 'canvas') {
      const canvas: Record<string, unknown> = {
        width: 0,
        height: 0,
        getContext: () => makeCtx(),
      }
      if (opts.captureStream) canvas.captureStream = () => captured
      return canvas as unknown as HTMLElement
    }
    if (tag === 'video') {
      return {
        muted: false,
        playsInline: false,
        autoplay: false,
        readyState: 0,
        videoWidth: 0,
        videoHeight: 0,
        srcObject: null,
        play: async () => {},
        pause: () => {},
      } as unknown as HTMLElement
    }
    return realCreate(tag)
  })
  return { trackStop }
}

beforeEach(() => {
  vi.resetModules()
  vi.stubGlobal('requestAnimationFrame', () => 1)
  vi.stubGlobal('cancelAnimationFrame', () => {})
  // Minimal MediaStream so setSource can wrap a track.
  vi.stubGlobal('MediaStream', class {
    tracks: unknown[]
    constructor(tracks: unknown[] = []) { this.tracks = tracks }
    addTrack() {}
    getVideoTracks() { return [] }
    getAudioTracks() { return [] }
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('videoFilters — support detection', () => {
  it("always reports 'none' as supported", async () => {
    stubCanvasSupport({ captureStream: false })
    const mod = await import('./videoFilters')
    expect(mod.isFilterSupported('none')).toBe(true)
    expect(mod.supportedFilters()).toContain('none')
  })

  it('reports blur/grading supported when the canvas can captureStream', async () => {
    stubCanvasSupport({ captureStream: true })
    const mod = await import('./videoFilters')
    expect(mod.canvasFilterSupported()).toBe(true)
    expect(mod.isFilterSupported('blur')).toBe(true)
    expect(mod.isFilterSupported('grading')).toBe(true)
  })

  it('reports blur/grading unsupported when captureStream is missing', async () => {
    stubCanvasSupport({ captureStream: false })
    const mod = await import('./videoFilters')
    expect(mod.canvasFilterSupported()).toBe(false)
    expect(mod.isFilterSupported('blur')).toBe(false)
    expect(mod.isFilterSupported('grading')).toBe(false)
    expect(mod.supportedFilters()).toEqual(['none'])
  })

  it("gates the 'face' filter on the FaceDetector API", async () => {
    stubCanvasSupport({ captureStream: true })
    // No FaceDetector global → face unsupported even though canvas works.
    const mod = await import('./videoFilters')
    expect(mod.isFilterSupported('face')).toBe(false)

    vi.resetModules()
    stubCanvasSupport({ captureStream: true })
    vi.stubGlobal('FaceDetector', class { async detect() { return [] } })
    const mod2 = await import('./videoFilters')
    expect(mod2.isFilterSupported('face')).toBe(true)
  })
})

describe('videoFilters — pipeline lifecycle', () => {
  it('returns null when the browser cannot run a canvas pipeline', async () => {
    stubCanvasSupport({ captureStream: false })
    const mod = await import('./videoFilters')
    expect(mod.createVideoFilterPipeline()).toBeNull()
  })

  it('builds a pipeline exposing a stable output track and a working stop()', async () => {
    const { trackStop } = stubCanvasSupport({ captureStream: true })
    const mod = await import('./videoFilters')
    const pipeline = mod.createVideoFilterPipeline()
    expect(pipeline).not.toBeNull()
    expect(pipeline!.outputTrack).toBeTruthy()

    // Source + filter changes never throw and keep the same output track.
    const before = pipeline!.outputTrack
    pipeline!.setFilter('blur')
    pipeline!.setSource({ kind: 'video', stop: vi.fn() } as unknown as MediaStreamTrack)
    pipeline!.setFilter('grading')
    expect(pipeline!.outputTrack).toBe(before)

    pipeline!.stop()
    expect(trackStop).toHaveBeenCalled()
  })
})
