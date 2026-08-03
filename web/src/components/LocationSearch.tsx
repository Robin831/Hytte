import { useState, useEffect, useRef, useCallback, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'

interface SearchResult {
  name: string
  context?: string
  country: string
  lat: string
  lon: string
}

interface LocationSearchProps {
  onSelect: (result: { name: string; country: string; lat: number; lon: number }) => void
  inputClassName?: string
  /** Optional override; defaults to the translated placeholder. */
  placeholder?: string
  /** Optional override; defaults to the translated aria-label. */
  ariaLabel?: string
  inputId?: string
}

export default function LocationSearch({ onSelect, inputClassName = 'w-44', placeholder, ariaLabel, inputId }: LocationSearchProps) {
  const { t } = useTranslation('weather')
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [loading, setLoading] = useState(false)
  // Search failures show a single translated message; the underlying error is
  // logged instead of rendered, since the API returns untranslated detail.
  const [failed, setFailed] = useState(false)
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)

  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Dismiss dropdown on outside click.
  useEffect(() => {
    function handleMouseDown(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleMouseDown)
    return () => document.removeEventListener('mousedown', handleMouseDown)
  }, [])

  // Debounced search: fires 300ms after the user stops typing.
  useEffect(() => {
    const trimmed = query.trim()

    const delay = trimmed.length < 2 ? 0 : 300
    const timer = setTimeout(async () => {
      if (trimmed.length < 2) {
        setResults([])
        setOpen(false)
        setFailed(false)
        return
      }
      setLoading(true)
      setFailed(false)
      try {
        const res = await fetch(`/api/weather/search?q=${encodeURIComponent(trimmed)}`)
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          throw new Error((body as { error?: string }).error || `Search request failed with status ${res.status}`)
        }
        const data = await res.json() as { results: SearchResult[] }
        setResults(data.results ?? [])
        setOpen(true)
        setActiveIndex(-1)
      } catch (err) {
        console.warn('Location search failed:', err)
        setFailed(true)
        setResults([])
        setOpen(false)
      } finally {
        setLoading(false)
      }
    }, delay)

    return () => clearTimeout(timer)
  }, [query])

  const handleSelect = useCallback(
    (result: SearchResult) => {
      onSelect({
        name: result.name,
        country: result.country,
        lat: parseFloat(result.lat),
        lon: parseFloat(result.lon),
      })
      setQuery('')
      setResults([])
      setOpen(false)
      setActiveIndex(-1)
    },
    [onSelect],
  )

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (!open || results.length === 0) return

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIndex((i) => Math.min(i + 1, results.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIndex((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (activeIndex >= 0 && activeIndex < results.length) {
        handleSelect(results[activeIndex])
      }
    } else if (e.key === 'Escape') {
      setOpen(false)
      setActiveIndex(-1)
    }
  }

  return (
    <div ref={containerRef} className="relative">
      <div className="relative flex items-center">
        <Search size={14} className="absolute left-2.5 text-gray-400 pointer-events-none" />
        <input
          ref={inputRef}
          id={inputId}
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => {
            if (results.length > 0) setOpen(true)
          }}
          placeholder={placeholder ?? t('locationSearch.placeholder')}
          aria-label={ariaLabel ?? t('locationSearch.ariaLabel')}
          className={`pl-8 pr-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-sm text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 ${inputClassName}`}
        />
        {loading && (
          <span className="absolute right-2.5 text-xs text-gray-400 select-none">…</span>
        )}
      </div>

      {failed && (
        <p role="alert" className="mt-1 text-xs text-red-400">{t('locationSearch.error')}</p>
      )}

      {open && results.length > 0 && (
        <ul
          role="listbox"
          className="absolute z-50 mt-1 w-72 bg-gray-800 border border-gray-600 rounded-lg shadow-lg overflow-hidden"
        >
          {results.map((result, idx) => (
            <li
              key={`${result.lat}-${result.lon}-${idx}`}
              role="option"
              aria-selected={idx === activeIndex}
              onMouseEnter={() => setActiveIndex(idx)}
              onMouseDown={(e) => {
                // Prevent input blur before click registers.
                e.preventDefault()
                handleSelect(result)
              }}
              className={`px-3 py-2 cursor-pointer text-sm ${
                idx === activeIndex
                  ? 'bg-blue-600 text-white'
                  : 'text-gray-200 hover:bg-gray-700'
              }`}
            >
              <span className="font-medium">{result.name}</span>
              {result.context && (
                <span className={`text-sm ${idx === activeIndex ? 'text-blue-200' : 'text-gray-400'}`}>, {result.context}</span>
              )}
              {result.country && (
                <span className={`ml-1 text-xs ${idx === activeIndex ? 'text-blue-200' : 'text-gray-500'}`}>({result.country})</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
