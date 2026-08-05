import { useCallback, useEffect, useMemo, useReducer } from 'react'
import { useAuth } from '../auth'
import {
  type RecentLocation,
  isValidRecentLocation,
  loadRecentLocations,
  saveRecentLocations,
  addRecentLocation,
  parseRecentLocationsPreference,
  buildDefaultLocations,
} from '../recentLocations'

/** Resolve a location name, checking recents first then the fetched known locations. */
function findLocation(
  name: string,
  recents: RecentLocation[],
  knownLocations?: RecentLocation[],
): RecentLocation | undefined {
  const fromRecents = recents.find((l) => l.name === name)
  if (fromRecents) return fromRecents
  return knownLocations?.find((l) => l.name === name)
}

interface LocationState {
  /** The active location, or null before anything has been resolved. */
  selected: RecentLocation | null
  /** Recently used locations, most recent first. */
  recents: RecentLocation[]
  /** Canonical city list from /api/weather/locations, sorted by name. */
  known: RecentLocation[]
  /** True once the locations request has settled (successfully or not). */
  locationsLoaded: boolean
  /** True once the preferences request has settled (successfully or not). */
  prefsFetched: boolean
  /**
   * A saved preference name that could not be resolved yet because the known
   * locations list had not arrived. Retried once the list settles.
   */
  pendingPreferenceName: string | null
  /** True once the user has explicitly picked a location this session. */
  userHasSelected: boolean
  /** Set when a selection needs to be written to localStorage or the server. */
  pendingSave: { location: RecentLocation; recents: RecentLocation[] } | null
}

type LocationAction =
  | { type: 'locationsResolved'; locations: RecentLocation[] }
  | { type: 'locationsSettled' }
  | { type: 'preferencesResolved'; serverRecents: RecentLocation[] | null; savedName: string | null }
  | { type: 'preferencesSettled' }
  | { type: 'selectByName'; name: string }
  | { type: 'select'; location: RecentLocation }
  | { type: 'saved' }

/**
 * Read the initial location and recents from localStorage.
 * Returns a null location on first visit — coordinates then come from the API.
 */
function initLocationState(): LocationState {
  const base: LocationState = {
    selected: null,
    recents: [],
    known: [],
    locationsLoaded: false,
    prefsFetched: false,
    pendingPreferenceName: null,
    userHasSelected: false,
    pendingSave: null,
  }

  const recents = loadRecentLocations()
  if (!recents) return base

  try {
    const stored = localStorage.getItem('weather_location')
    if (stored) {
      // Prefer full-JSON storage with lat+lon matching (avoids duplicate-name collisions)
      try {
        const parsed = JSON.parse(stored) as unknown
        if (isValidRecentLocation(parsed)) {
          const found = recents.find((l) => l.lat === parsed.lat && l.lon === parsed.lon) ?? parsed
          return { ...base, selected: found, recents }
        }
      } catch {
        // Not JSON — fall through to legacy name matching
      }
      const loc = findLocation(stored, recents)
      if (loc) return { ...base, selected: loc, recents }
    }
  } catch {
    // localStorage may be unavailable.
  }
  return { ...base, selected: recents[0] ?? null, recents }
}

/**
 * Resolve a preference name that arrived before the known locations list.
 * A no-op unless a name is pending, the list has settled, and the user has not
 * already made an explicit choice (which always wins over a stored preference).
 */
function applyPendingPreference(state: LocationState): LocationState {
  if (!state.pendingPreferenceName || !state.locationsLoaded || state.userHasSelected) return state
  const loc = findLocation(state.pendingPreferenceName, state.recents, state.known)
  if (!loc) return state
  return { ...state, selected: loc, pendingPreferenceName: null }
}

function locationReducer(state: LocationState, action: LocationAction): LocationState {
  switch (action.type) {
    case 'locationsResolved': {
      const locs = [...action.locations].sort((a, b) => a.name.localeCompare(b.name))
      const locMap = new Map(locs.map((l) => [l.name, l]))
      const defaults = buildDefaultLocations(locs)
      return applyPendingPreference({
        ...state,
        known: locs,
        // First visit — build defaults from API data (no hardcoded coordinates).
        // Otherwise reconcile stored locations with canonical API coordinates.
        recents:
          state.recents.length === 0 ? defaults : state.recents.map((r) => locMap.get(r.name) ?? r),
        selected: state.selected ?? defaults[0] ?? locs[0] ?? null,
      })
    }

    case 'locationsSettled':
      return applyPendingPreference({ ...state, locationsLoaded: true })

    case 'preferencesResolved': {
      const { serverRecents, savedName } = action
      // Recents captured before the server sync — they carry a selection the user
      // may have made while the preferences request was still in flight.
      const priorRecents = state.recents
      const recents = serverRecents && serverRecents.length > 0 ? serverRecents : priorRecents

      if (state.userHasSelected) {
        // User interacted before prefs loaded; push their choice server-side.
        return state.selected
          ? { ...state, recents, pendingSave: { location: state.selected, recents: priorRecents } }
          : { ...state, recents }
      }
      if (!savedName) return { ...state, recents }

      // Resolve from server recents first, then known cities from the API.
      const loc = findLocation(savedName, serverRecents ?? priorRecents, state.known)
      if (loc) return { ...state, recents, selected: loc }
      // Known locations may not be loaded yet; store the name and retry when they arrive.
      return applyPendingPreference({ ...state, recents, pendingPreferenceName: savedName })
    }

    case 'preferencesSettled':
      return applyPendingPreference({ ...state, prefsFetched: true })

    case 'selectByName': {
      const loc = findLocation(action.name, state.recents, state.known)
      if (!loc) return { ...state, userHasSelected: true }
      const recents = addRecentLocation(state.recents, loc)
      return {
        ...state,
        userHasSelected: true,
        selected: loc,
        recents,
        pendingSave: { location: loc, recents },
      }
    }

    case 'select': {
      const recents = addRecentLocation(state.recents, action.location)
      return {
        ...state,
        userHasSelected: true,
        selected: action.location,
        recents,
        pendingSave: { location: action.location, recents },
      }
    }

    case 'saved':
      return { ...state, pendingSave: null }

    default:
      return state
  }
}

export interface WeatherLocationState {
  /** The active location, or null until one has been resolved. */
  location: RecentLocation | null
  /** Recently used locations, most recent first. */
  recents: RecentLocation[]
  /** Canonical city list from the API, sorted by name. */
  knownLocations: RecentLocation[]
  /** Recents for the dropdown, always including the active location. */
  displayRecents: RecentLocation[]
  /** Known cities not already present in `displayRecents`. */
  otherCities: RecentLocation[]
  /**
   * True once the active location is known to be final: auth has settled, any
   * stored preference has been read, and the locations list (or recents) exists.
   * Fetching a forecast before this would hit the wrong location first.
   */
  locationResolved: boolean
  /** True once the user has explicitly picked a location this session. */
  userHasSelected: boolean
  /** Select a location from the dropdown by city name. */
  selectByName: (name: string) => void
  /** Select an arbitrary location from search or geolocation. */
  selectLocation: (result: { name: string; lat: number; lon: number }) => void
}

/**
 * Owns the weather page's location state machine: selection, recents, and the
 * localStorage-vs-preferences split.
 *
 * Anonymous visitors persist to localStorage; authenticated users persist to
 * `/api/settings/preferences` only, so recents never leak across accounts on a
 * shared browser. A preference that arrives before the canonical locations list
 * is parked in `pendingPreferenceName` and retried once the list settles, and an
 * explicit user selection always wins over a later-arriving stored preference.
 */
export function useWeatherLocation(): WeatherLocationState {
  const { user, loading: authLoading } = useAuth()
  const [state, dispatch] = useReducer(locationReducer, undefined, initLocationState)

  // Fetch available locations from the backend (single source of truth for coordinates).
  useEffect(() => {
    let cancelled = false
    fetch('/api/weather/locations')
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch locations')
        return r.json()
      })
      .then((data) => {
        if (cancelled) return
        dispatch({ type: 'locationsResolved', locations: (data.locations ?? []) as RecentLocation[] })
      })
      .catch((err: unknown) => {
        // Best-effort: the dropdown still shows recent locations from localStorage.
        console.warn('Failed to fetch locations:', err)
      })
      .finally(() => {
        if (!cancelled) dispatch({ type: 'locationsSettled' })
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Load the user's preferred location and recents once auth settles.
  useEffect(() => {
    if (authLoading || !user) return

    let cancelled = false
    fetch('/api/settings/preferences', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (cancelled) return
        const raw = data?.preferences?.recent_locations
        dispatch({
          type: 'preferencesResolved',
          serverRecents: raw ? parseRecentLocationsPreference(raw) : null,
          savedName: data?.preferences?.weather_location || data?.preferences?.home_location || null,
        })
      })
      .catch((err: unknown) => {
        // Preference load is best-effort; localStorage values are used as fallback.
        console.warn('Failed to fetch preferences:', err)
      })
      .finally(() => {
        if (!cancelled) dispatch({ type: 'preferencesSettled' })
      })

    return () => {
      cancelled = true
    }
  }, [user, authLoading])

  // Persist recent locations to localStorage — only for unauthenticated users after
  // auth settles. Authenticated users store recents server-side only to prevent
  // cross-account leakage.
  const { recents, pendingSave } = state
  useEffect(() => {
    if (authLoading || user) return
    saveRecentLocations(recents)
  }, [recents, user, authLoading])

  // Flush a queued selection to its persistence target.
  useEffect(() => {
    if (!pendingSave) return
    dispatch({ type: 'saved' })

    if (!user) {
      try {
        localStorage.setItem('weather_location', JSON.stringify(pendingSave.location))
      } catch {
        // localStorage may be unavailable.
      }
      saveRecentLocations(pendingSave.recents)
      return
    }

    fetch('/api/settings/preferences', {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        preferences: {
          weather_location: pendingSave.location.name,
          recent_locations: JSON.stringify(pendingSave.recents),
        },
      }),
    }).catch((err: unknown) => {
      // Best-effort save; localStorage is used as local fallback.
      console.warn('Failed to save preferences:', err)
    })
  }, [pendingSave, user])

  const selectByName = useCallback((name: string) => {
    dispatch({ type: 'selectByName', name })
  }, [])

  const selectLocation = useCallback((result: { name: string; lat: number; lon: number }) => {
    dispatch({ type: 'select', location: { name: result.name, lat: result.lat, lon: result.lon } })
  }, [])

  // Build dropdown options: recent locations, then remaining known cities not in
  // recents. The active location is always present so the dropdown never renders
  // empty or mismatched while loading.
  const { selected, known } = state
  const displayRecents = useMemo(() => {
    if (!selected || recents.some((l) => l.name === selected.name)) return recents
    return [selected, ...recents]
  }, [selected, recents])

  const otherCities = useMemo(() => {
    const recentNames = new Set(displayRecents.map((l) => l.name))
    return known.filter((l) => !recentNames.has(l.name))
  }, [displayRecents, known])

  const locationResolved =
    selected !== null &&
    !authLoading &&
    (!user || state.prefsFetched) &&
    (recents.length > 0 || state.locationsLoaded)

  return {
    location: selected,
    recents,
    knownLocations: known,
    displayRecents,
    otherCities,
    locationResolved,
    userHasSelected: state.userHasSelected,
    selectByName,
    selectLocation,
  }
}
