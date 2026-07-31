import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { LocateFixed, Loader2 } from 'lucide-react'

interface UseMyLocationButtonProps {
  /** Same shape as LocationSearch.onSelect, so both feed the identical selection flow. */
  onSelect: (result: { name: string; country: string; lat: number; lon: number }) => void
}

interface ReverseResult {
  name: string
  context?: string
  country: string
  lat: string
  lon: string
}

const GEOLOCATION_TIMEOUT_MS = 10_000

/** Format coordinates as a human-readable label, used when reverse geocoding fails. */
function formatCoordinates(lat: number, lon: number, t: TFunction<'weather'>): string {
  return t('geolocation.coordinates', {
    lat: Math.abs(lat).toFixed(2),
    latDir: lat >= 0 ? t('geolocation.north') : t('geolocation.south'),
    lon: Math.abs(lon).toFixed(2),
    lonDir: lon >= 0 ? t('geolocation.east') : t('geolocation.west'),
  })
}

/**
 * Resolves the device position on tap and hands it to `onSelect`. When reverse
 * geocoding fails the coordinates are still selected, labelled with a formatted
 * coordinate string. Geolocation failures show an inline message and select nothing.
 */
export default function UseMyLocationButton({ onSelect }: UseMyLocationButtonProps) {
  const { t } = useTranslation('weather')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleClick = useCallback(() => {
    if (typeof navigator === 'undefined' || !navigator.geolocation) {
      setError(t('geolocation.unsupported'))
      return
    }

    setLoading(true)
    setError(null)

    navigator.geolocation.getCurrentPosition(
      async (position) => {
        const { latitude, longitude } = position.coords
        let name = formatCoordinates(latitude, longitude, t)
        let country = ''
        try {
          const res = await fetch(
            `/api/weather/reverse?lat=${encodeURIComponent(latitude)}&lon=${encodeURIComponent(longitude)}`,
          )
          if (res.ok) {
            const data = (await res.json()) as { result?: ReverseResult }
            if (data.result?.name) {
              name = data.result.name
              country = data.result.country ?? ''
            }
          }
        } catch (err) {
          // Reverse geocoding is best-effort — fall back to formatted coordinates.
          console.warn('Failed to reverse geocode position:', err)
        } finally {
          setLoading(false)
        }
        onSelect({ name, country, lat: latitude, lon: longitude })
      },
      (positionError) => {
        setLoading(false)
        if (positionError.code === positionError.PERMISSION_DENIED) {
          setError(t('geolocation.denied'))
        } else if (positionError.code === positionError.TIMEOUT) {
          setError(t('geolocation.timeout'))
        } else {
          setError(t('geolocation.unavailable'))
        }
      },
      { timeout: GEOLOCATION_TIMEOUT_MS, enableHighAccuracy: false },
    )
  }, [onSelect, t])

  return (
    // items-start keeps the button at its natural width; the error paragraph below
    // is what widens this flex item, and it wraps onto its own row at narrow widths.
    <div className="flex flex-col items-start">
      <button
        type="button"
        onClick={handleClick}
        disabled={loading}
        aria-label={t('geolocation.buttonAria')}
        aria-busy={loading}
        className="flex items-center gap-1.5 px-3 py-2 rounded-lg bg-gray-700 border border-gray-600 text-sm text-gray-300 hover:text-white hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
      >
        {loading ? (
          <Loader2 size={16} className="animate-spin shrink-0" />
        ) : (
          <LocateFixed size={16} className="shrink-0" />
        )}
        <span className="whitespace-nowrap">
          {loading ? t('geolocation.locating') : t('geolocation.button')}
        </span>
      </button>

      {error && (
        <p role="alert" className="mt-1 w-56 max-w-full text-xs text-red-400">
          {error}
        </p>
      )}
    </div>
  )
}
