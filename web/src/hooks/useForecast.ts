import { useEffect, useReducer, useRef } from 'react'
import { usePreferredLocation } from '../usePreferredLocation'

export interface TimeseriesEntry {
  time: string
  data: {
    instant: {
      details: {
        air_temperature: number
      }
    }
    next_1_hours?: { summary: { symbol_code: string } }
    next_6_hours?: { summary: { symbol_code: string } }
  }
}

export interface ForecastResponse {
  properties: { timeseries: TimeseriesEntry[] }
}

type State = { loading: boolean; error: boolean; data: ForecastResponse | null }
type Action = { type: 'start' } | { type: 'done'; data: ForecastResponse } | { type: 'error' }

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { loading: true, error: false, data: state.data }
    case 'done': return { loading: false, error: false, data: action.data }
    case 'error': return { loading: false, error: true, data: state.data }
  }
}

const cache = new Map<string, ForecastResponse>()

function cacheKey(lat: number, lon: number, name: string) {
  return `${lat},${lon},${name}`
}

export function useForecast(): State {
  const location = usePreferredLocation()
  const key = cacheKey(location.lat, location.lon, location.name)
  const [state, dispatch] = useReducer(reducer, {
    loading: !cache.has(key),
    error: false,
    data: cache.get(key) ?? null,
  })
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    const cached = cache.get(key)
    if (cached) {
      dispatch({ type: 'done', data: cached })
      return
    }

    const controller = new AbortController()
    dispatch({ type: 'start' })
    fetch(
      `/api/weather/forecast?lat=${location.lat}&lon=${location.lon}&location=${encodeURIComponent(location.name)}`,
      { signal: controller.signal, credentials: 'include' },
    )
      .then((r) => (r.ok ? (r.json() as Promise<ForecastResponse>) : Promise.reject()))
      .then((d) => {
        cache.set(key, d)
        if (mountedRef.current) dispatch({ type: 'done', data: d })
      })
      .catch(() => {
        if (!controller.signal.aborted && mountedRef.current) dispatch({ type: 'error' })
      })
    return () => controller.abort()
  }, [key, location.lat, location.lon, location.name])

  return state
}
