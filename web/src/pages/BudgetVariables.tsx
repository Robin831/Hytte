import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Plus, Trash2, ChevronLeft, ChevronRight, Check, X, Pencil, Copy, ChevronDown, ChevronUp } from 'lucide-react'
import { formatDate, formatNumber } from '../utils/formatDate'

// ── Types ────────────────────────────────────────────────────────────────────

interface VariableEntry {
  id: number
  variable_id: number
  month: string
  sub_name: string
  amount: number
}

interface VariableBill {
  id: number
  user_id: number
  name: string
  recurring_id: number | null
  entries: VariableEntry[]
}

interface DraftEntry {
  id: number // 0 for new entries
  uid: string // stable React key
  sub_name: string
  amountStr: string
}

interface EntryDiff {
  sub_name: string
  type: 'added' | 'changed' | 'removed'
  oldAmount?: number
  newAmount?: number
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function currentMonth(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

function prevMonth(m: string): string {
  const [year, mon] = m.split('-').map(Number)
  const d = new Date(year, mon - 2)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function nextMonth(m: string): string {
  const [year, mon] = m.split('-').map(Number)
  const d = new Date(year, mon)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function formatMonthLabel(m: string): string {
  const [year, mon] = m.split('-').map(Number)
  return formatDate(new Date(year, mon - 1), { month: 'long', year: 'numeric' })
}

function formatAmount(amount: number): string {
  return formatNumber(amount, {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

function billTotal(entries: VariableEntry[]): number {
  return entries.reduce((sum, e) => sum + e.amount, 0)
}

function entriesToDraft(entries: VariableEntry[]): DraftEntry[] {
  return entries.map(e => ({ id: e.id, uid: String(e.id), sub_name: e.sub_name, amountStr: String(e.amount) }))
}

function computeDiff(oldEntries: VariableEntry[], newEntries: VariableEntry[]): EntryDiff[] {
  const diffs: EntryDiff[] = []
  const oldMap = new Map(oldEntries.map(e => [e.sub_name, e.amount]))
  const newMap = new Map(newEntries.map(e => [e.sub_name, e.amount]))

  for (const [name, newAmt] of newMap) {
    if (!oldMap.has(name)) {
      diffs.push({ sub_name: name, type: 'added', newAmount: newAmt })
    } else if (oldMap.get(name) !== newAmt) {
      diffs.push({ sub_name: name, type: 'changed', oldAmount: oldMap.get(name), newAmount: newAmt })
    }
  }
  for (const [name, oldAmt] of oldMap) {
    if (!newMap.has(name)) {
      diffs.push({ sub_name: name, type: 'removed', oldAmount: oldAmt })
    }
  }
  return diffs
}

// ── Component ────────────────────────────────────────────────────────────────

export default function BudgetVariables() {
  const { t } = useTranslation('budget')

  const [month, setMonth] = useState(currentMonth)
  const [bills, setBills] = useState<VariableBill[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Expand state per bill
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set())

  // Draft entries per bill (bill id → DraftEntry[])
  const [draftEntries, setDraftEntries] = useState<Record<number, DraftEntry[]>>({})

  // Saving state per bill
  const [saving, setSaving] = useState<Record<number, boolean>>({})

  // Copy diff per bill (shown after copy)
  const [copyDiff, setCopyDiff] = useState<Record<number, EntryDiff[]>>({})

  // Copy saving state per bill
  const [copying, setCopying] = useState<Record<number, boolean>>({})

  // New bill form
  const [showNewBill, setShowNewBill] = useState(false)
  const [newBillName, setNewBillName] = useState('')
  const [creatingBill, setCreatingBill] = useState(false)

  // Editing bill name
  const [editingBillId, setEditingBillId] = useState<number | null>(null)
  const [editingBillName, setEditingBillName] = useState('')

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`/api/budget/variables?month=${month}`, { credentials: 'include', signal })
      if (!res.ok) throw new Error('load failed')
      const data = await res.json()
      const loaded: VariableBill[] = data.variable_bills ?? []
      setBills(loaded)
      // Sync draft entries for already-expanded bills
      setDraftEntries(prev => {
        const next = { ...prev }
        for (const bill of loaded) {
          if (expandedIds.has(bill.id)) {
            next[bill.id] = entriesToDraft(bill.entries)
          }
        }
        return next
      })
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(t('variables.errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  // expandedIds intentionally omitted — only sync on full reload
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [month, t])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  // Reset copy diffs when month changes
  useEffect(() => {
    setCopyDiff({})
  }, [month])

  function toggleExpand(bill: VariableBill) {
    setExpandedIds(prev => {
      const next = new Set(prev)
      if (next.has(bill.id)) {
        next.delete(bill.id)
      } else {
        next.add(bill.id)
        // Initialize draft entries if not already set
        setDraftEntries(d => {
          if (d[bill.id]) return d
          return { ...d, [bill.id]: entriesToDraft(bill.entries) }
        })
      }
      return next
    })
  }

  // ── Bill CRUD ────────────────────────────────────────────────────────────────

  async function handleCreateBill() {
    const name = newBillName.trim()
    if (!name) return
    setCreatingBill(true)
    setError(null)
    try {
      const res = await fetch('/api/budget/variables', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      })
      if (!res.ok) throw new Error('create failed')
      const data = await res.json()
      const created: VariableBill = { ...data.variable_bill, entries: [] }
      setBills(prev => [...prev, created])
      setNewBillName('')
      setShowNewBill(false)
      // Auto-expand new bill
      setExpandedIds(prev => new Set(prev).add(created.id))
      setDraftEntries(prev => ({ ...prev, [created.id]: [] }))
    } catch {
      setError(t('variables.errors.createFailed'))
    } finally {
      setCreatingBill(false)
    }
  }

  function startEditName(bill: VariableBill) {
    setEditingBillId(bill.id)
    setEditingBillName(bill.name)
  }

  async function saveEditName(id: number) {
    const name = editingBillName.trim()
    if (!name) return
    setError(null)
    try {
      const res = await fetch(`/api/budget/variables/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      })
      if (!res.ok) throw new Error('update failed')
      setBills(prev => prev.map(b => b.id === id ? { ...b, name } : b))
      setEditingBillId(null)
    } catch {
      setError(t('variables.errors.createFailed'))
    }
  }

  async function handleDeleteBill(id: number) {
    setError(null)
    try {
      const res = await fetch(`/api/budget/variables/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('delete failed')
      setBills(prev => prev.filter(b => b.id !== id))
      setExpandedIds(prev => { const s = new Set(prev); s.delete(id); return s })
      setDraftEntries(prev => { const d = { ...prev }; delete d[id]; return d })
      setCopyDiff(prev => { const d = { ...prev }; delete d[id]; return d })
    } catch {
      setError(t('variables.errors.deleteFailed'))
    }
  }

  // ── Entry editing ────────────────────────────────────────────────────────────

  function addEntry(billId: number) {
    setDraftEntries(prev => ({
      ...prev,
      [billId]: [...(prev[billId] ?? []), { id: 0, uid: `new-${Date.now()}-${Math.random()}`, sub_name: '', amountStr: '' }],
    }))
  }

  function removeEntry(billId: number, uid: string) {
    setDraftEntries(prev => ({
      ...prev,
      [billId]: (prev[billId] ?? []).filter(e => e.uid !== uid),
    }))
  }

  function updateEntryField(billId: number, uid: string, field: 'sub_name' | 'amountStr', value: string) {
    setDraftEntries(prev => {
      const entries = (prev[billId] ?? []).map(e =>
        e.uid === uid ? { ...e, [field]: value } : e
      )
      return { ...prev, [billId]: entries }
    })
  }

  async function saveEntries(billId: number) {
    const draft = draftEntries[billId] ?? []
    const entries = draft
      .filter(e => e.sub_name.trim() !== '')
      .map(e => ({
        sub_name: e.sub_name.trim(),
        amount: parseFloat(e.amountStr.replace(',', '.')) || 0,
      }))
    setSaving(prev => ({ ...prev, [billId]: true }))
    setError(null)
    try {
      const res = await fetch(`/api/budget/variables/${billId}/entries?month=${month}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(entries),
      })
      if (!res.ok) throw new Error('save failed')
      // Reload to get updated entries with IDs
      await load()
    } catch {
      setError(t('variables.errors.saveFailed'))
    } finally {
      setSaving(prev => ({ ...prev, [billId]: false }))
    }
  }

  // ── Copy from last month ─────────────────────────────────────────────────────

  async function copyFromLastMonth(bill: VariableBill) {
    const from = prevMonth(month)
    const oldEntries = [...bill.entries]
    setCopying(prev => ({ ...prev, [bill.id]: true }))
    setError(null)
    try {
      const res = await fetch(`/api/budget/variables/${bill.id}/copy?from=${from}&to=${month}`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('copy failed')
      const data = await res.json()
      const newEntries: VariableEntry[] = data.entries ?? []
      const diff = computeDiff(oldEntries, newEntries)
      setCopyDiff(prev => ({ ...prev, [bill.id]: diff }))
      setBills(prev => prev.map(b => b.id === bill.id ? { ...b, entries: newEntries } : b))
      setDraftEntries(prev => ({ ...prev, [bill.id]: entriesToDraft(newEntries) }))
    } catch {
      setError(t('variables.errors.copyFailed'))
    } finally {
      setCopying(prev => ({ ...prev, [bill.id]: false }))
    }
  }

  // ── Render ───────────────────────────────────────────────────────────────────

  if (loading) {
    return <div className="p-6 text-gray-400">{t('loading')}</div>
  }

  return (
    <div className="p-4 md:p-6 max-w-2xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-4">
        <Link
          to="/budget"
          className="text-gray-400 hover:text-white transition-colors"
          aria-label={t('import.backToBudget')}
        >
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-xl font-semibold text-white">{t('variables.title')}</h1>
        <button
          type="button"
          onClick={() => { setShowNewBill(true); setNewBillName('') }}
          className="ml-auto flex items-center gap-1.5 bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-3 py-1.5 rounded-lg transition-colors"
        >
          <Plus size={16} />
          {t('variables.add')}
        </button>
      </div>

      {/* Month navigation */}
      <div className="flex items-center gap-2 mb-5">
        <button
          type="button"
          onClick={() => setMonth(m => prevMonth(m))}
          className="p-1 rounded hover:bg-gray-700 text-gray-300"
          aria-label={t('variables.month.prev')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-gray-200 min-w-32 text-center capitalize">
          {formatMonthLabel(month)}
        </span>
        <button
          type="button"
          onClick={() => setMonth(m => nextMonth(m))}
          className="p-1 rounded hover:bg-gray-700 text-gray-300"
          aria-label={t('variables.month.next')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* New bill form */}
      {showNewBill && (
        <div className="mb-4 p-4 bg-gray-800 rounded-xl border border-gray-700 flex items-center gap-2">
          <input
            type="text"
            value={newBillName}
            onChange={e => setNewBillName(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') handleCreateBill(); if (e.key === 'Escape') setShowNewBill(false) }}
            placeholder={t('variables.namePlaceholder')}
            autoFocus
            className="flex-1 bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
          />
          <button
            type="button"
            onClick={handleCreateBill}
            disabled={creatingBill || !newBillName.trim()}
            className="flex items-center gap-1 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors"
          >
            <Check size={14} />
          </button>
          <button
            type="button"
            onClick={() => setShowNewBill(false)}
            className="flex items-center gap-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm px-3 py-1.5 rounded-lg transition-colors"
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Bills list */}
      {bills.length === 0 ? (
        <div className="text-center py-12 text-gray-500 text-sm">
          {t('variables.empty')}
        </div>
      ) : (
        <ul className="space-y-3">
          {bills.map(bill => {
            const isExpanded = expandedIds.has(bill.id)
            const draft = draftEntries[bill.id] ?? entriesToDraft(bill.entries)
            const total = billTotal(bill.entries)
            const diff = copyDiff[bill.id]
            const isEditingName = editingBillId === bill.id

            return (
              <li key={bill.id} className="bg-gray-800 rounded-xl border border-gray-700 overflow-hidden">
                {/* Bill header row */}
                <div
                  className="flex items-center gap-2 px-4 py-3 cursor-pointer select-none hover:bg-gray-700/50"
                  onClick={() => !isEditingName && toggleExpand(bill)}
                >
                  {/* Name or edit input */}
                  {isEditingName ? (
                    <input
                      type="text"
                      value={editingBillName}
                      onChange={e => setEditingBillName(e.target.value)}
                      onKeyDown={e => {
                        e.stopPropagation()
                        if (e.key === 'Enter') saveEditName(bill.id)
                        if (e.key === 'Escape') setEditingBillId(null)
                      }}
                      onClick={e => e.stopPropagation()}
                      autoFocus
                      className="flex-1 bg-gray-700 text-white text-sm rounded-lg px-2 py-1 border border-gray-600 focus:border-indigo-500 focus:outline-none"
                    />
                  ) : (
                    <span className="flex-1 font-medium text-white text-sm">{bill.name}</span>
                  )}

                  {/* Total */}
                  {!isEditingName && (
                    <span className={`text-sm font-semibold tabular-nums ${total < 0 ? 'text-red-400' : total > 0 ? 'text-green-400' : 'text-gray-400'}`}>
                      {formatAmount(total)}
                    </span>
                  )}

                  {/* Name edit actions */}
                  {isEditingName ? (
                    <>
                      <button
                        type="button"
                        onClick={e => { e.stopPropagation(); saveEditName(bill.id) }}
                        className="p-1 text-indigo-400 hover:text-white transition-colors"
                        aria-label={t('variables.save')}
                      >
                        <Check size={15} />
                      </button>
                      <button
                        type="button"
                        onClick={e => { e.stopPropagation(); setEditingBillId(null) }}
                        className="p-1 text-gray-400 hover:text-white transition-colors"
                        aria-label={t('quickAdd.cancel')}
                      >
                        <X size={15} />
                      </button>
                    </>
                  ) : (
                    <>
                      <button
                        type="button"
                        onClick={e => { e.stopPropagation(); startEditName(bill) }}
                        className="p-1 text-gray-400 hover:text-white transition-colors"
                        aria-label={t('variables.editName')}
                      >
                        <Pencil size={14} />
                      </button>
                      <button
                        type="button"
                        onClick={e => { e.stopPropagation(); handleDeleteBill(bill.id) }}
                        className="p-1 text-gray-400 hover:text-red-400 transition-colors"
                        aria-label={t('variables.delete')}
                      >
                        <Trash2 size={14} />
                      </button>
                      {isExpanded ? <ChevronUp size={16} className="text-gray-400" /> : <ChevronDown size={16} className="text-gray-400" />}
                    </>
                  )}
                </div>

                {/* Expanded: entries */}
                {isExpanded && (
                  <div className="border-t border-gray-700 px-4 py-3 space-y-2">
                    {/* Entry rows */}
                    {draft.length === 0 && (
                      <p className="text-xs text-gray-500 italic">{t('variables.noEntries')}</p>
                    )}
                    {draft.map(entry => (
                      <div key={entry.uid} className="flex items-center gap-2">
                        <input
                          type="text"
                          value={entry.sub_name}
                          onChange={e => updateEntryField(bill.id, entry.uid, 'sub_name', e.target.value)}
                          placeholder={t('variables.subNamePlaceholder')}
                          aria-label={t('variables.subName')}
                          className="flex-1 bg-gray-700 text-white text-sm rounded-lg px-2 py-1.5 border border-gray-600 focus:border-indigo-500 focus:outline-none"
                        />
                        <input
                          type="number"
                          value={entry.amountStr}
                          onChange={e => updateEntryField(bill.id, entry.uid, 'amountStr', e.target.value)}
                          placeholder="0"
                          step="any"
                          aria-label={t('quickAdd.amount')}
                          className="w-28 bg-gray-700 text-white text-sm rounded-lg px-2 py-1.5 border border-gray-600 focus:border-indigo-500 focus:outline-none text-right tabular-nums"
                        />
                        <button
                          type="button"
                          onClick={() => removeEntry(bill.id, entry.uid)}
                          className="p-1 text-gray-400 hover:text-red-400 transition-colors flex-shrink-0"
                          aria-label={t('variables.removeEntry')}
                        >
                          <X size={14} />
                        </button>
                      </div>
                    ))}

                    {/* Add entry + total row */}
                    <div className="flex items-center justify-between pt-1">
                      <button
                        type="button"
                        onClick={() => addEntry(bill.id)}
                        className="flex items-center gap-1 text-indigo-400 hover:text-indigo-300 text-xs transition-colors"
                      >
                        <Plus size={13} />
                        {t('variables.addEntry')}
                      </button>
                      <span className="text-xs text-gray-400">
                        {t('variables.total')}:{' '}
                        <span className="font-semibold text-white tabular-nums">
                          {formatAmount(
                            draft.reduce((sum, e) => sum + (parseFloat(e.amountStr.replace(',', '.')) || 0), 0)
                          )}
                        </span>
                      </span>
                    </div>

                    {/* Actions row */}
                    <div className="flex items-center gap-2 pt-1 flex-wrap">
                      <button
                        type="button"
                        onClick={() => saveEntries(bill.id)}
                        disabled={saving[bill.id]}
                        className="flex items-center gap-1 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-xs px-2.5 py-1.5 rounded-lg transition-colors"
                      >
                        <Check size={13} />
                        {saving[bill.id] ? t('quickAdd.saving') : t('variables.save')}
                      </button>
                      <button
                        type="button"
                        onClick={() => copyFromLastMonth(bill)}
                        disabled={copying[bill.id]}
                        className="flex items-center gap-1 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-xs px-2.5 py-1.5 rounded-lg transition-colors"
                      >
                        <Copy size={13} />
                        {copying[bill.id] ? t('quickAdd.saving') : t('variables.copyFromLastMonth')}
                      </button>
                    </div>

                    {/* Copy diff */}
                    {diff && diff.length > 0 && (
                      <div className="mt-2 p-2 bg-gray-900 rounded-lg text-xs space-y-0.5">
                        {diff.map(d => (
                          <div key={d.sub_name} className="flex items-center gap-1.5">
                            <span className={
                              d.type === 'added' ? 'text-green-400' :
                              d.type === 'removed' ? 'text-red-400' :
                              'text-yellow-400'
                            }>
                              {d.type === 'added' ? '+' : d.type === 'removed' ? '−' : '~'}
                            </span>
                            <span className="text-gray-300">{d.sub_name}</span>
                            {d.type === 'changed' && d.oldAmount !== undefined && d.newAmount !== undefined && (
                              <span className="text-gray-500">
                                {formatAmount(d.oldAmount)} → {formatAmount(d.newAmount)}
                              </span>
                            )}
                            {d.type === 'added' && d.newAmount !== undefined && (
                              <span className="text-gray-500">{formatAmount(d.newAmount)}</span>
                            )}
                            {d.type === 'removed' && d.oldAmount !== undefined && (
                              <span className="text-gray-500">{formatAmount(d.oldAmount)}</span>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                    {diff && diff.length === 0 && (
                      <p className="text-xs text-gray-500 italic mt-1">{t('variables.noPreviousEntries')}</p>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
