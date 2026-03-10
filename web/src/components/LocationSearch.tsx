import { useState, useEffect, useRef, useCallback } from 'react'
import { Search } from 'lucide-react'

interface SearchResult {
  name: string
  country: string
  lat: string
  lon: string
}

interface LocationSearchProps {
  onSelect: (result: { name: string; country: string; lat: number; lon: number }) => void
}

export default function LocationSearch({ onSelect }: LocationSearchProps) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
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
    if (trimmed.length < 2) {
      setResults([])
      setOpen(false)
      setError(null)
      return
    }

    const timer = setTimeout(async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetch(`/api/weather/search?q=${encodeURIComponent(trimmed)}`)
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          throw new Error((body as { error?: string }).error || 'Search failed')
        }
        const data = await res.json() as { results: SearchResult[] }
        setResults(data.results ?? [])
        setOpen(true)
        setActiveIndex(-1)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Search failed')
        setResults([])
        setOpen(false)
      } finally {
        setLoading(false)
      }
    }, 300)

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

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
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
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => {
            if (results.length > 0) setOpen(true)
          }}
          placeholder="Search location…"
          aria-label="Search for a location"
          className="pl-8 pr-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-sm text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 w-44"
        />
        {loading && (
          <span className="absolute right-2.5 text-xs text-gray-400 select-none">…</span>
        )}
      </div>

      {error && (
        <p className="mt-1 text-xs text-red-400">{error}</p>
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
              {result.country && (
                <span className="text-gray-400 ml-1">({result.country})</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
