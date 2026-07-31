import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LocateFixed, Loader2, MapPin } from 'lucide-react'
import type { RecentLocation } from '../recentLocations'

/**
 * A location is identified by its coordinates. An empty `name` means the
 * coordinates came from the browser's geolocation — it is rendered with the
 * translated "My location" label so a stored selection is language-independent.
 */
export type PickedLocation = RecentLocation

interface LocationPickerProps {
  value: PickedLocation | null
  onChange: (location: PickedLocation) => void
  /** Selectable locations, sourced from GET /api/weather/locations. */
  locations: PickedLocation[]
  /** Disables the control while the location list is still loading. */
  loading?: boolean
}

const GEOLOCATION_TIMEOUT_MS = 10_000

/** Stable option/select key for a location — coordinates avoid duplicate-name collisions. */
export function locationKey(location: PickedLocation): string {
  return `${location.lat},${location.lon}`
}

/**
 * Dropdown of known locations plus a "use my location" button.
 *
 * Strings live in the `skywatch` namespace, currently the only consumer. Geolocation
 * coordinates are used as-is (no reverse geocoding); failures show an inline message
 * and leave the current selection untouched.
 */
export default function LocationPicker({ value, onChange, locations, loading = false }: LocationPickerProps) {
  const { t } = useTranslation('skywatch')
  const [locating, setLocating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Always include the active selection so geolocation results (which are not in
  // the known-locations list) stay visible in the dropdown.
  const options =
    value && !locations.some((l) => locationKey(l) === locationKey(value))
      ? [value, ...locations]
      : locations

  const labelFor = useCallback(
    (location: PickedLocation) => (location.name === '' ? t('location.myLocation') : location.name),
    [t],
  )

  // Not memoized: `options` is rebuilt on every render, so a memo would never hit.
  const handleSelect = (key: string) => {
    const found = options.find((l) => locationKey(l) === key)
    if (found) onChange(found)
  }

  const handleUseMyLocation = useCallback(() => {
    if (typeof navigator === 'undefined' || !navigator.geolocation) {
      setError(t('location.unsupported'))
      return
    }

    setLocating(true)
    setError(null)

    navigator.geolocation.getCurrentPosition(
      (position) => {
        setLocating(false)
        onChange({ name: '', lat: position.coords.latitude, lon: position.coords.longitude })
      },
      (positionError) => {
        setLocating(false)
        if (positionError.code === positionError.PERMISSION_DENIED) {
          setError(t('location.denied'))
        } else if (positionError.code === positionError.TIMEOUT) {
          setError(t('location.timeout'))
        } else {
          setError(t('location.unavailable'))
        }
      },
      { timeout: GEOLOCATION_TIMEOUT_MS, enableHighAccuracy: false },
    )
  }, [onChange, t])

  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-center gap-2">
        <MapPin size={16} className="text-gray-400 shrink-0" aria-hidden="true" />
        <select
          value={value ? locationKey(value) : ''}
          onChange={(e) => handleSelect(e.target.value)}
          disabled={loading}
          aria-label={t('location.select')}
          className="min-w-0 flex-1 sm:flex-none bg-gray-900/70 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500 disabled:opacity-50"
        >
          {!value && <option value="">{t('location.choose')}</option>}
          {options.map((location) => (
            <option key={locationKey(location)} value={locationKey(location)}>
              {labelFor(location)}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={handleUseMyLocation}
          disabled={locating}
          aria-label={t('location.useMyLocationAria')}
          aria-busy={locating}
          className="flex items-center gap-1.5 px-3 py-2 rounded-lg bg-gray-900/70 border border-gray-700 text-sm text-gray-300 hover:text-white hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
        >
          {locating ? (
            <Loader2 size={16} className="animate-spin shrink-0" />
          ) : (
            <LocateFixed size={16} className="shrink-0" />
          )}
          <span className="whitespace-nowrap">
            {locating ? t('location.locating') : t('location.useMyLocation')}
          </span>
        </button>
      </div>

      {error && (
        <p role="alert" className="text-xs text-red-400">
          {error}
        </p>
      )}
    </div>
  )
}
