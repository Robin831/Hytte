import { useState, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2, Check, X } from 'lucide-react'
import { useAuth } from '../auth'

interface GroceryItem {
  id: number
  household_id: number
  content: string
  original_text: string
  source_language: string
  checked: boolean
  sort_order: number
  added_by: number
  created_at: string
}

export default function GroceryPage() {
  const { t } = useTranslation('grocery')
  const { user } = useAuth()
  const [items, setItems] = useState<GroceryItem[]>([])
  const [newItem, setNewItem] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const fetchItems = useCallback(async (signal?: AbortSignal) => {
    const res = await fetch('/api/grocery/items', { credentials: 'include', signal })
    if (!res.ok) throw new Error('fetch failed')
    const data = await res.json()
    return data.items as GroceryItem[]
  }, [])

  // Initial load
  useEffect(() => {
    if (!user) return
    let cancelled = false
    ;(async () => {
      try {
        const fetched = await fetchItems()
        if (!cancelled) setItems(fetched)
      } catch {
        if (!cancelled) setError(t('errors.failedToLoad'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [user, fetchItems, t])

  // Poll every 5 seconds
  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    const poll = async () => {
      if (document.hidden) return
      try {
        const fetched = await fetchItems(controller.signal)
        setItems(fetched)
      } catch {
        // silently ignore polling errors
      }
    }
    const intervalId = setInterval(poll, 5000)
    return () => { clearInterval(intervalId); controller.abort() }
  }, [user, fetchItems])

  const handleAdd = async () => {
    const text = newItem.trim()
    if (!text || adding) return
    setAdding(true)
    try {
      const res = await fetch('/api/grocery/items', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: text }),
      })
      if (!res.ok) throw new Error('add failed')
      const data = await res.json()
      setItems(prev => [...prev.filter(i => !i.checked), data.item, ...prev.filter(i => i.checked)])
      setNewItem('')
      inputRef.current?.focus()
    } catch {
      setError(t('errors.failedToAdd'))
    } finally {
      setAdding(false)
    }
  }

  const handleToggle = async (item: GroceryItem) => {
    const newChecked = !item.checked
    // Optimistic update
    setItems(prev => {
      const updated = prev.map(i => i.id === item.id ? { ...i, checked: newChecked } : i)
      return [...updated.filter(i => !i.checked), ...updated.filter(i => i.checked)]
    })
    try {
      const res = await fetch(`/api/grocery/items/${item.id}/check`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ checked: newChecked }),
      })
      if (!res.ok) throw new Error('toggle failed')
    } catch {
      // Revert on failure
      setItems(prev => {
        const reverted = prev.map(i => i.id === item.id ? { ...i, checked: item.checked } : i)
        return [...reverted.filter(i => !i.checked), ...reverted.filter(i => i.checked)]
      })
      setError(t('errors.failedToUpdate'))
    }
  }

  const handleClearCompleted = async () => {
    const checked = items.filter(i => i.checked)
    if (checked.length === 0) return
    // Optimistic remove
    setItems(prev => prev.filter(i => !i.checked))
    try {
      const res = await fetch('/api/grocery/completed', {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('clear failed')
    } catch {
      // Revert on failure
      setItems(prev => [...prev, ...checked])
      setError(t('errors.failedToClear'))
    }
  }

  const unchecked = items.filter(i => !i.checked)
  const checked = items.filter(i => i.checked)

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500" />
      </div>
    )
  }

  return (
    <div className="max-w-lg mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">{t('title')}</h1>

      {error && (
        <div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-200 text-sm flex items-center justify-between">
          <span>{error}</span>
          <button onClick={() => setError('')} className="ml-2 text-red-400 hover:text-red-200 cursor-pointer">
            <X size={16} />
          </button>
        </div>
      )}

      {/* Add item input */}
      <div className="flex gap-2 mb-6">
        <input
          ref={inputRef}
          type="text"
          value={newItem}
          onChange={e => setNewItem(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') handleAdd() }}
          placeholder={t('addPlaceholder')}
          className="flex-1 min-w-0 bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
          aria-label={t('addPlaceholder')}
        />
        {/* Insertion point for future microphone button (sub-task 3) */}
        <button
          onClick={handleAdd}
          disabled={!newItem.trim() || adding}
          className="shrink-0 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg px-4 py-3 flex items-center gap-2 cursor-pointer transition-colors"
          aria-label={t('add')}
        >
          <Plus size={20} />
          <span className="hidden sm:inline">{t('add')}</span>
        </button>
      </div>

      {/* Item list */}
      {items.length === 0 ? (
        <div className="text-center py-12 text-gray-500">
          <p className="text-lg">{t('empty')}</p>
          <p className="text-sm mt-1">{t('emptyHint')}</p>
        </div>
      ) : (
        <div className="space-y-1">
          {/* Unchecked items */}
          {unchecked.map(item => (
            <GroceryItemRow key={item.id} item={item} onToggle={handleToggle} />
          ))}

          {/* Checked items section */}
          {checked.length > 0 && (
            <>
              <div className="flex items-center justify-between pt-4 pb-2">
                <span className="text-sm text-gray-500">{t('checkedSection')} ({checked.length})</span>
                <button
                  onClick={handleClearCompleted}
                  className="flex items-center gap-1.5 text-sm text-red-400 hover:text-red-300 cursor-pointer transition-colors"
                >
                  <Trash2 size={14} />
                  {t('clearCompleted')}
                </button>
              </div>
              {checked.map(item => (
                <GroceryItemRow key={item.id} item={item} onToggle={handleToggle} />
              ))}
            </>
          )}
        </div>
      )}
    </div>
  )
}

function GroceryItemRow({ item, onToggle }: { item: GroceryItem; onToggle: (item: GroceryItem) => void }) {
  const { t } = useTranslation('grocery')
  const showOriginal = item.original_text && item.original_text !== item.content

  return (
    <button
      onClick={() => onToggle(item)}
      className="flex items-start gap-3 w-full px-3 py-3 rounded-lg hover:bg-gray-800/50 transition-colors cursor-pointer text-left"
    >
      <span
        className={`shrink-0 mt-0.5 w-6 h-6 rounded border-2 flex items-center justify-center transition-colors ${
          item.checked
            ? 'bg-green-600 border-green-600'
            : 'border-gray-600 hover:border-gray-400'
        }`}
      >
        {item.checked && <Check size={14} className="text-white" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className={`block text-sm ${item.checked ? 'line-through text-gray-500' : 'text-white'}`}>
          {item.content}
        </span>
        {showOriginal && (
          <span className="block text-xs text-gray-500 mt-0.5">
            {t('item.original', { text: item.original_text })}
          </span>
        )}
      </span>
    </button>
  )
}
