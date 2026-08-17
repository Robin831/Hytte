import { vi } from 'vitest'

/**
 * Handle returned by {@link stubEventSource}, for driving the stubbed stream.
 */
export interface EventSourceStub {
  /**
   * Delivers a named SSE event to every open subscriber. Objects are
   * JSON-encoded into `event.data` the way the server sends them; pass a string
   * to send a raw (possibly malformed) payload.
   */
  dispatch(type: string, data?: unknown): void
  /** URLs of the connections opened so far, in order. */
  urls(): string[]
  /** Connections opened and not yet closed — pins effect cleanup. */
  openCount(): number
}

/**
 * Stubs the global `EventSource` with a recording fake.
 *
 * Neither jsdom nor happy-dom implements EventSource, so any page that
 * subscribes to an SSE hub throws on mount without this. Unlike an inert stub,
 * this one keeps the registered listeners and exposes `dispatch()`, so a test
 * can drive the server-push paths (Stride's `stride_eval_started` /
 * `stride_eval_ready`) instead of leaving them uncovered.
 *
 * Call from a `beforeEach`; `vi.unstubAllGlobals()` in `afterEach` removes it.
 * Each call gets its own listener/instance state, so tests never leak into
 * each other.
 */
export function stubEventSource(): EventSourceStub {
  type Listener = (event: MessageEvent) => void

  const instances: FakeEventSource[] = []

  class FakeEventSource {
    url: string
    closed = false
    onopen: (() => void) | null = null
    onerror: (() => void) | null = null
    private listeners = new Map<string, Set<Listener>>()

    constructor(url: string) {
      this.url = url
      instances.push(this)
    }

    addEventListener(type: string, fn: Listener) {
      const set = this.listeners.get(type) ?? new Set<Listener>()
      set.add(fn)
      this.listeners.set(type, set)
    }

    removeEventListener(type: string, fn: Listener) {
      this.listeners.get(type)?.delete(fn)
    }

    close() {
      this.closed = true
    }

    emit(type: string, payload: string) {
      if (this.closed) return
      const event = new MessageEvent(type, { data: payload })
      for (const fn of this.listeners.get(type) ?? []) fn(event)
    }
  }

  vi.stubGlobal('EventSource', FakeEventSource)

  return {
    dispatch(type, data = {}) {
      const payload = typeof data === 'string' ? data : JSON.stringify(data)
      // Snapshot first: a listener may close its connection or open a new one.
      for (const es of [...instances]) es.emit(type, payload)
    },
    urls: () => instances.map(es => es.url),
    openCount: () => instances.filter(es => !es.closed).length,
  }
}
