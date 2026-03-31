import { useState, useRef, useEffect, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { X, Copy, Check, Plus } from 'lucide-react'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../ui/dialog'
import LocationSearch from '../LocationSearch'

interface StopResult {
  id: string
  name: string
}

interface SelectedLocation {
  name: string
  lat: number
  lon: number
}

interface Props {
  open: boolean
  onClose: () => void
  onSuccess: () => void
}

export default function TokenCreateDialog({ open, onClose, onSuccess }: Props) {
  const { t } = useTranslation('settings')
  const titleId = useId()

  const [name, setName] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [stopQuery, setStopQuery] = useState('')
  const [stopResults, setStopResults] = useState<StopResult[]>([])
  const [selectedStops, setSelectedStops] = useState<StopResult[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
  const [showDropdown, setShowDropdown] = useState(false)

  const [selectedLocation, setSelectedLocation] = useState<SelectedLocation | null>(null)

  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const searchTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)

  // Reset state when dialog opens/closes
  useEffect(() => {
    if (!open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setName('')
      setExpiresAt('')
      setStopQuery('')
      setStopResults([])
      setSelectedStops([])
      setShowDropdown(false)
      setSelectedLocation(null)
      setSubmitting(false)
      setError('')
      setCreatedToken(null)
      setCopied(false)
    }
  }, [open])

  // Debounced stop search
  useEffect(() => {
    if (searchTimeoutRef.current) clearTimeout(searchTimeoutRef.current)
    if (stopQuery.trim().length < 2) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setStopResults([])
      setShowDropdown(false)
      return
    }
    const controller = new AbortController()
    searchTimeoutRef.current = setTimeout(async () => {
      setSearchLoading(true)
      try {
        const res = await fetch(`/api/transit/search?q=${encodeURIComponent(stopQuery.trim())}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (res.ok && !controller.signal.aborted) {
          const data = await res.json()
          setStopResults(data.results ?? [])
          setShowDropdown(true)
        }
      } catch (err) {
        if ((err as Error).name !== 'AbortError') {
          // ignore non-abort search errors
        }
      } finally {
        if (!controller.signal.aborted) {
          setSearchLoading(false)
        }
      }
    }, 300)
    return () => {
      if (searchTimeoutRef.current) clearTimeout(searchTimeoutRef.current)
      controller.abort()
    }
  }, [stopQuery])

  // Close dropdown on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setShowDropdown(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  function addStop(stop: StopResult) {
    if (!selectedStops.some((s) => s.id === stop.id)) {
      setSelectedStops((prev) => [...prev, stop])
    }
    setStopQuery('')
    setStopResults([])
    setShowDropdown(false)
  }

  function removeStop(id: string) {
    setSelectedStops((prev) => prev.filter((s) => s.id !== id))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    if (!name.trim()) {
      setError(t('kioskTokens.errorNameRequired'))
      return
    }

    const config: Record<string, unknown> = {}
    if (selectedStops.length > 0) {
      config.stop_ids = selectedStops.map((s) => s.id)
    }
    if (selectedLocation) {
      config.lat = selectedLocation.lat
      config.lon = selectedLocation.lon
      config.location = selectedLocation.name
    }

    // date input gives YYYY-MM-DD; convert to RFC3339 at end of day UTC, or omit if unset
    const expiresAtRFC = expiresAt ? `${expiresAt}T23:59:59Z` : undefined

    setSubmitting(true)
    try {
      const body: Record<string, unknown> = { name: name.trim(), config }
      if (expiresAtRFC) body.expires_at = expiresAtRFC
      const res = await fetch('/api/kiosk/tokens', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setError(data.error ?? t('kioskTokens.errorCreate'))
        return
      }
      const data = await res.json()
      setCreatedToken(data.token)
      onSuccess()
    } catch {
      setError(t('kioskTokens.errorCreate'))
    } finally {
      setSubmitting(false)
    }
  }

  async function copyToken() {
    if (!createdToken) return
    try {
      await navigator.clipboard.writeText(createdToken)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // fallback: select text
    }
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="max-w-lg" aria-labelledby={titleId}>
      <DialogHeader id={titleId} title={t('kioskTokens.createTitle')} onClose={onClose} />

      {createdToken ? (
        <>
          <DialogBody>
            <p className="text-sm text-gray-300 mb-3">{t('kioskTokens.tokenOnceWarning')}</p>
            <div className="flex items-center gap-2 bg-gray-800 rounded-lg px-3 py-2 border border-gray-600">
              <code className="flex-1 text-sm text-green-400 font-mono break-all select-all">
                {createdToken}
              </code>
              <button
                type="button"
                onClick={copyToken}
                aria-label={t('kioskTokens.copyToken')}
                className="text-gray-400 hover:text-white transition-colors shrink-0 cursor-pointer"
              >
                {copied ? <Check size={18} className="text-green-400" /> : <Copy size={18} />}
              </button>
            </div>
            <p className="text-xs text-gray-500 mt-2">{t('kioskTokens.tokenQueryParam')}</p>
          </DialogBody>
          <DialogFooter>
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors cursor-pointer"
            >
              {t('kioskTokens.done')}
            </button>
          </DialogFooter>
        </>
      ) : (
        <form onSubmit={handleSubmit}>
          <DialogBody>
            <div className="space-y-4">
              {/* Name */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-1" htmlFor="kiosk-token-name">
                  {t('kioskTokens.labelName')}
                </label>
                <input
                  id="kiosk-token-name"
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t('kioskTokens.namePlaceholder')}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  required
                />
              </div>

              {/* Stop search */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-1" htmlFor="kiosk-stop-search">
                  {t('kioskTokens.labelStops')}
                </label>
                {selectedStops.length > 0 && (
                  <div className="flex flex-wrap gap-2 mb-2">
                    {selectedStops.map((stop) => (
                      <span
                        key={stop.id}
                        className="inline-flex items-center gap-1 bg-blue-900/50 border border-blue-700 text-blue-200 text-xs rounded-full px-2 py-1"
                      >
                        {stop.name}
                        <button
                          type="button"
                          onClick={() => removeStop(stop.id)}
                          aria-label={t('kioskTokens.removeStop', { name: stop.name })}
                          className="text-blue-300 hover:text-white cursor-pointer"
                        >
                          <X size={12} />
                        </button>
                      </span>
                    ))}
                  </div>
                )}
                <div className="relative" ref={dropdownRef}>
                  <div className="relative">
                    <input
                      id="kiosk-stop-search"
                      type="text"
                      value={stopQuery}
                      onChange={(e) => setStopQuery(e.target.value)}
                      onFocus={() => stopResults.length > 0 && setShowDropdown(true)}
                      placeholder={t('kioskTokens.stopSearchPlaceholder')}
                      className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                      autoComplete="off"
                    />
                    {searchLoading && (
                      <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-gray-400">
                        {t('kioskTokens.searching')}
                      </span>
                    )}
                  </div>
                  {showDropdown && stopResults.length > 0 && (
                    <ul
                      className="absolute z-50 mt-1 w-full bg-gray-800 border border-gray-600 rounded-lg shadow-lg overflow-hidden"
                      role="listbox"
                    >
                      {stopResults.map((result) => {
                        const alreadySelected = selectedStops.some((s) => s.id === result.id)
                        return (
                          <li key={result.id} role="option" aria-selected={alreadySelected}>
                            <button
                              type="button"
                              onClick={() => addStop(result)}
                              disabled={alreadySelected}
                              className="w-full flex items-center gap-2 px-3 py-2 text-sm text-left hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
                            >
                              <Plus size={14} className="shrink-0 text-gray-400" />
                              <span className="truncate">{result.name}</span>
                              <span className="ml-auto text-xs text-gray-500 shrink-0">{result.id}</span>
                            </button>
                          </li>
                        )
                      })}
                    </ul>
                  )}
                </div>
              </div>

              {/* Weather location */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-1">
                  {t('kioskTokens.labelLocation')}
                </label>
                {selectedLocation ? (
                  <div className="flex items-center gap-2 bg-gray-800 border border-gray-600 rounded-lg px-3 py-2">
                    <span className="flex-1 text-sm text-white">{selectedLocation.name}</span>
                    <button
                      type="button"
                      onClick={() => setSelectedLocation(null)}
                      aria-label={t('kioskTokens.locationClear')}
                      className="text-gray-400 hover:text-white transition-colors cursor-pointer"
                    >
                      <X size={16} />
                    </button>
                  </div>
                ) : (
                  <LocationSearch
                    onSelect={(loc) => setSelectedLocation({ name: loc.name, lat: loc.lat, lon: loc.lon })}
                    inputClassName="w-full"
                  />
                )}
              </div>

              {/* Expiry date */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-1" htmlFor="kiosk-token-expiry">
                  {t('kioskTokens.labelExpiry')}
                </label>
                <input
                  id="kiosk-token-expiry"
                  type="date"
                  value={expiresAt}
                  onChange={(e) => setExpiresAt(e.target.value)}
                  min={(() => {
                    const d = new Date()
                    const year = d.getFullYear()
                    const month = String(d.getMonth() + 1).padStart(2, '0')
                    const day = String(d.getDate()).padStart(2, '0')
                    return `${year}-${month}-${day}`
                  })()}
                  className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 [color-scheme:dark]"
                />
                <p className="text-xs text-gray-500 mt-1">{t('kioskTokens.expiryHint')}</p>
              </div>

              {error && (
                <p className="text-sm text-red-400">{error}</p>
              )}
            </div>
          </DialogBody>
          <DialogFooter>
            <button
              type="button"
              onClick={onClose}
              disabled={submitting}
              className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors disabled:opacity-50 cursor-pointer"
            >
              {t('kioskTokens.cancel')}
            </button>
            <button
              type="submit"
              disabled={submitting || !name.trim()}
              className="px-4 py-2 text-sm font-medium rounded bg-blue-600 hover:bg-blue-500 text-white transition-colors disabled:opacity-50 cursor-pointer"
            >
              {submitting ? t('kioskTokens.creating') : t('kioskTokens.create')}
            </button>
          </DialogFooter>
        </form>
      )}
    </Dialog>
  )
}
