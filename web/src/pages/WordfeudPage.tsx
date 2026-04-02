import { useState, useRef, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Search, Grid3X3 } from 'lucide-react'
import WordfeudBoard from './WordfeudBoard'

interface FoundWord {
  word: string
  score: number
  blank_positions?: number[]
}

type SearchMode = 'anagram' | 'starts_with' | 'ends_with' | 'contains'

const TAB_KEY = 'wordfeud-tab'

export default function WordfeudPage() {
  const { t } = useTranslation('wordfeud')

  const [activeTab, setActiveTab] = useState<'finder' | 'board'>(() => {
    const stored = localStorage.getItem(TAB_KEY)
    // Migrate legacy tab values to valid ones
    if (stored === 'mygames' || stored === 'games') {
      localStorage.setItem(TAB_KEY, 'board')
      return 'board'
    }
    const valid = ['finder', 'board'] as const
    return (valid as readonly string[]).includes(stored ?? '') ? (stored as 'finder' | 'board') : 'finder'
  })

  const handleTabChange = (tab: 'finder' | 'board') => {
    setActiveTab(tab)
    localStorage.setItem(TAB_KEY, tab)
  }

  return (
    <div className="max-w-6xl mx-auto p-4 md:p-8">
      <h1 className="text-2xl font-bold mb-4">{t('title')}</h1>

      {/* Tabs */}
      <div role="tablist" className="flex gap-1 mb-6 border-b border-gray-700">
        <button
          role="tab"
          id="tab-finder"
          aria-selected={activeTab === 'finder'}
          aria-controls="tabpanel-finder"
          onClick={() => handleTabChange('finder')}
          className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer ${
            activeTab === 'finder'
              ? 'border-blue-500 text-white'
              : 'border-transparent text-gray-400 hover:text-gray-200'
          }`}
        >
          <Search size={16} />
          {t('finder.tab')}
        </button>
        <button
          role="tab"
          id="tab-board"
          aria-selected={activeTab === 'board'}
          aria-controls="tabpanel-board"
          onClick={() => handleTabChange('board')}
          className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer ${
            activeTab === 'board'
              ? 'border-blue-500 text-white'
              : 'border-transparent text-gray-400 hover:text-gray-200'
          }`}
        >
          <Grid3X3 size={16} />
          {t('board.tab')}
        </button>
      </div>

      <div role="tabpanel" id="tabpanel-finder" aria-labelledby="tab-finder" hidden={activeTab !== 'finder'}>
        {activeTab === 'finder' && <WordFinder />}
      </div>
      <div role="tabpanel" id="tabpanel-board" aria-labelledby="tab-board" hidden={activeTab !== 'board'}>
        {activeTab === 'board' && <WordfeudBoard />}
      </div>
    </div>
  )
}

function WordFinder() {
  const { t } = useTranslation('wordfeud')

  const [letters, setLetters] = useState('')
  const [pattern, setPattern] = useState('')
  const [mode, setMode] = useState<SearchMode>('anagram')
  const [results, setResults] = useState<FoundWord[]>([])
  const [totalMatches, setTotalMatches] = useState(0)
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [hasSearched, setHasSearched] = useState(false)
  const controllerRef = useRef<AbortController | null>(null)

  const handleSearch = useCallback(async () => {
    const trimmedLetters = letters.trim().toUpperCase()
    const trimmedPattern = pattern.trim().toUpperCase()

    if (mode === 'anagram') {
      if (!trimmedLetters || trimmedLetters.length > 7) return
    } else {
      if (!trimmedPattern || trimmedPattern.length > 15) return
    }

    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller

    setSearching(true)
    setError(null)
    setHasSearched(true)

    try {
      let url: string
      let body: string

      if (mode === 'anagram') {
        url = '/api/wordfeud/find'
        body = JSON.stringify({ letters: trimmedLetters })
      } else {
        url = '/api/wordfeud/search'
        body = JSON.stringify({
          pattern: trimmedPattern,
          mode,
          letters: trimmedLetters || undefined,
        })
      }

      const res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body,
        signal: controller.signal,
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('finder.errors.searchFailed'))
      }

      const data = await res.json()
      if (!controller.signal.aborted) {
        setResults(data.words ?? [])
        setTotalMatches(data.total ?? 0)
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (!controller.signal.aborted) {
        setError(err instanceof Error ? err.message : t('finder.errors.searchFailed'))
        setResults([])
        setTotalMatches(0)
      }
    } finally {
      if (!controller.signal.aborted) {
        setSearching(false)
      }
    }
  }, [letters, pattern, mode, t])

  useEffect(() => {
    return () => { controllerRef.current?.abort() }
  }, [])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSearch()
    }
  }

  const handleLetterInput = (value: string) => {
    // Allow letters, Norwegian characters, and * for blanks
    const filtered = value.toUpperCase().replace(/[^A-ZÆØÅ*]/g, '')
    if (filtered.length <= 7) {
      setLetters(filtered)
    }
  }

  const handlePatternInput = (value: string) => {
    // Pattern: letters only, no blanks
    const filtered = value.toUpperCase().replace(/[^A-ZÆØÅ]/g, '')
    if (filtered.length <= 15) {
      setPattern(filtered)
    }
  }

  const modes: { value: SearchMode; label: string }[] = [
    { value: 'anagram', label: t('finder.modeAnagram') },
    { value: 'contains', label: t('finder.modeContains') },
    { value: 'starts_with', label: t('finder.modeStartsWith') },
    { value: 'ends_with', label: t('finder.modeEndsWith') },
  ]

  return (
    <div>
      {/* Search mode selector */}
      <div className="flex flex-wrap gap-2 mb-4">
        {modes.map(m => (
          <button
            key={m.value}
            onClick={() => {
              controllerRef.current?.abort()
              controllerRef.current = null
              setSearching(false)
              setMode(m.value)
              setResults([])
              setHasSearched(false)
              setError(null)
            }}
            className={`px-3 py-1.5 text-sm rounded-lg transition-colors cursor-pointer ${
              mode === m.value
                ? 'bg-blue-600 text-white'
                : 'bg-gray-800 text-gray-300 hover:bg-gray-700'
            }`}
          >
            {m.label}
          </button>
        ))}
      </div>

      {/* Input fields */}
      <div className="flex flex-col gap-2 mb-4">
        {/* Rack letters input — always visible */}
        <div className="flex gap-2">
          <div className="flex-1 relative">
            <input
              type="text"
              value={letters}
              onChange={e => handleLetterInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t('finder.placeholderAnagram')}
              maxLength={7}
              aria-label={t('finder.rackLabel')}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 uppercase tracking-wider font-mono"
            />
            <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-gray-500">
              {letters.length}/7
            </span>
          </div>
          {mode === 'anagram' && (
            <button
              onClick={handleSearch}
              disabled={searching || !letters.trim()}
              className="px-4 py-2.5 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg text-sm font-medium transition-colors cursor-pointer flex items-center gap-2"
            >
              <Search size={16} />
              <span className="hidden sm:inline">{t('finder.search')}</span>
            </button>
          )}
        </div>

        {/* Pattern input — only for non-anagram modes */}
        {mode !== 'anagram' && (
          <div className="flex gap-2">
            <div className="flex-1 relative">
              <input
                type="text"
                value={pattern}
                onChange={e => handlePatternInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={t('finder.placeholderPattern')}
                maxLength={15}
                aria-label={t('finder.patternLabel')}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 uppercase tracking-wider font-mono"
              />
              <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-gray-500">
                {pattern.length}/15
              </span>
            </div>
            <button
              onClick={handleSearch}
              disabled={searching || !pattern.trim()}
              className="px-4 py-2.5 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg text-sm font-medium transition-colors cursor-pointer flex items-center gap-2"
            >
              <Search size={16} />
              <span className="hidden sm:inline">{t('finder.search')}</span>
            </button>
          </div>
        )}
      </div>

      {/* Help text */}
      <p className="text-xs text-gray-500 mb-4">
        {mode === 'anagram' ? t('finder.blankHint') : t('finder.patternHint')}
      </p>

      {/* Error */}
      {error && (
        <div className="bg-red-900/50 border border-red-700 text-red-200 rounded-lg p-3 mb-4 text-sm">
          {error}
        </div>
      )}

      {/* Loading */}
      {searching && (
        <div className="flex items-center justify-center h-24">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
        </div>
      )}

      {/* Results */}
      {!searching && hasSearched && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <p className="text-sm text-gray-400">
              {t('finder.resultsCount', { count: totalMatches })}
            </p>
          </div>

          {results.length > 0 ? (
            <div className="bg-gray-800/50 rounded-lg border border-gray-700 overflow-hidden">
              {/* Header */}
              <div className="grid grid-cols-[1fr_auto_auto] gap-4 px-4 py-2.5 bg-gray-800 border-b border-gray-700 text-xs font-medium text-gray-400 uppercase tracking-wide">
                <span>{t('finder.colWord')}</span>
                <span className="text-right w-12">{t('finder.colLength')}</span>
                <span className="text-right w-14">{t('finder.colPoints')}</span>
              </div>

              {/* Rows */}
              <div className="max-h-[60vh] overflow-y-auto">
                {results.map((result, i) => (
                  <div
                    key={`${result.word}-${i}`}
                    className={`grid grid-cols-[1fr_auto_auto] gap-4 px-4 py-2 text-sm ${
                      i % 2 === 0 ? 'bg-gray-800/30' : ''
                    } hover:bg-gray-700/50 transition-colors`}
                  >
                    <span className="font-mono tracking-wider text-white">
                      {renderWord(result)}
                    </span>
                    <span className="text-right w-12 text-gray-400 tabular-nums">
                      {[...result.word].length}
                    </span>
                    <span className="text-right w-14 font-medium text-amber-400 tabular-nums">
                      {result.score}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <p className="text-gray-500 text-sm">{t('finder.noResults')}</p>
          )}
        </div>
      )}

      {/* Initial state */}
      {!searching && !hasSearched && (
        <p className="text-gray-500 text-sm">{t('finder.hint')}</p>
      )}
    </div>
  )
}

function renderWord(result: FoundWord): React.ReactNode {
  if (!result.blank_positions || result.blank_positions.length === 0) {
    return result.word
  }
  const blanks = new Set(result.blank_positions)
  return (
    <>
      {[...result.word].map((ch, i) => (
        <span key={i} className={blanks.has(i) ? 'text-purple-400' : ''}>
          {ch}
        </span>
      ))}
    </>
  )
}

