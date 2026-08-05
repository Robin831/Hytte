import { Fragment, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronUp, Eye, EyeOff, GripVertical } from 'lucide-react'
import { useAuth } from '../auth'
import { useToast } from '../hooks/useToast'
import ToastList from '../components/ToastList'
import WidgetBoundary from '../components/WidgetBoundary'
import WidgetSkeleton from '../components/WidgetSkeleton'
import DashboardEditBar from '../components/widgets/DashboardEditBar'
import {
  EMPTY_LAYOUT,
  hiddenWidgets,
  moveWidget,
  orderedWidgets,
  parseLayout,
  reorderTo,
  resolveLayout,
  type DashboardLayout,
  type WidgetDef,
} from '../components/widgets/registry'

const PREF_KEY = 'dashboard_widgets'

function Dashboard() {
  const { hasFeature, user } = useAuth()
  const { t } = useTranslation('dashboard')
  const { toasts, showToast } = useToast()

  const [layout, setLayout] = useState<DashboardLayout | null>(null)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [draggingId, setDraggingId] = useState<string | null>(null)
  const mountedRef = useRef(true)

  useEffect(() => {
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    fetch('/api/settings/preferences', { credentials: 'include', signal: controller.signal })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(`${res.status}`))))
      .then((data) => setLayout(parseLayout(data?.preferences?.[PREF_KEY])))
      .catch((err) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        // Keep the default layout — a failed read must not empty the dashboard.
        console.error('Failed to load dashboard layout:', err)
      })
    return () => {
      controller.abort()
    }
  }, [user])

  const featureEnabled = useCallback(
    (def: WidgetDef) => !def.feature || hasFeature(def.feature),
    [hasFeature],
  )

  // The full ordered id list is what gets persisted, so widgets the user cannot
  // currently see keep their place in the layout.
  const fullOrder = useMemo(() => orderedWidgets(layout).map((def) => def.id), [layout])
  const visibleWidgets = useMemo(
    () => resolveLayout(layout).filter(featureEnabled),
    [layout, featureEnabled],
  )
  const hiddenList = useMemo(
    () => hiddenWidgets(layout).filter(featureEnabled),
    [layout, featureEnabled],
  )
  const displayedIds = useMemo(() => visibleWidgets.map((def) => def.id), [visibleWidgets])

  const persist = useCallback(
    async (next: DashboardLayout) => {
      const previous = layout
      setLayout(next)
      setSaving(true)
      try {
        // dashboard_widgets is a JSON-typed preference: send the object itself,
        // not a JSON-encoded string, or the server rejects it with 400.
        const res = await fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: { [PREF_KEY]: next } }),
        })
        if (!res.ok) throw new Error(`save failed (${res.status})`)
      } catch (err) {
        console.error('Failed to save dashboard layout:', err)
        if (mountedRef.current) {
          setLayout(previous)
          showToast(t('layout.saveError'), 'error')
        }
      } finally {
        if (mountedRef.current) setSaving(false)
      }
    },
    [layout, showToast, t],
  )

  const currentHidden = useCallback(() => layout?.hidden ?? [], [layout])

  const handleMove = useCallback(
    (id: string, delta: -1 | 1) => {
      const order = moveWidget(fullOrder, displayedIds, id, delta)
      if (order === fullOrder) return
      void persist({ order, hidden: currentHidden() })
    },
    [fullOrder, displayedIds, persist, currentHidden],
  )

  const handleDrop = useCallback(
    (targetId: string, sourceId: string | null) => {
      setDraggingId(null)
      if (!sourceId) return
      const order = reorderTo(fullOrder, sourceId, targetId)
      if (order === fullOrder) return
      void persist({ order, hidden: currentHidden() })
    },
    [fullOrder, persist, currentHidden],
  )

  const handleHide = useCallback(
    (id: string) => {
      const hidden = currentHidden()
      if (hidden.includes(id)) return
      void persist({ order: fullOrder, hidden: [...hidden, id] })
    },
    [fullOrder, persist, currentHidden],
  )

  const handleShow = useCallback(
    (id: string) => {
      void persist({ order: fullOrder, hidden: currentHidden().filter((x) => x !== id) })
    },
    [fullOrder, persist, currentHidden],
  )

  const handleReset = useCallback(() => {
    void persist(EMPTY_LAYOUT)
  }, [persist])

  const renderWidget = (def: WidgetDef, index: number) => {
    const Component = def.component
    const label = t(def.titleKey)
    // The boundary stays outside Suspense so a lazy chunk that fails to load
    // degrades to the existing error tile instead of blanking the page. On the
    // happy path WidgetBoundary renders a bare Fragment, so the skeleton is the
    // grid cell itself — not a card nested inside another card.
    const boundary = (
      <WidgetBoundary label={label} className={def.colSpanClass}>
        {def.lazy ? (
          <Suspense fallback={<WidgetSkeleton label={label} className={def.colSpanClass} />}>
            <Component />
          </Suspense>
        ) : (
          <Component />
        )}
      </WidgetBoundary>
    )

    if (!editing) return <Fragment key={def.id}>{boundary}</Fragment>

    return (
      <div
        key={def.id}
        className={`relative rounded-xl ring-2 ${
          draggingId === def.id ? 'ring-blue-400 opacity-60' : 'ring-blue-500/30'
        } ${def.colSpanClass ?? ''}`}
        draggable
        onDragStart={(e) => {
          setDraggingId(def.id)
          e.dataTransfer.effectAllowed = 'move'
          e.dataTransfer.setData('text/plain', def.id)
        }}
        onDragEnd={() => setDraggingId(null)}
        onDragOver={(e) => {
          e.preventDefault()
          e.dataTransfer.dropEffect = 'move'
        }}
        onDrop={(e) => {
          e.preventDefault()
          handleDrop(def.id, draggingId || e.dataTransfer.getData('text/plain') || null)
        }}
      >
        <div className="flex flex-wrap items-center gap-1 rounded-t-xl bg-gray-950/80 px-2 py-1.5">
          <span className="text-gray-500" aria-hidden="true">
            <GripVertical size={16} />
          </span>
          <span className="mr-auto truncate text-xs text-gray-400">{label}</span>
          <button
            type="button"
            onClick={() => handleMove(def.id, -1)}
            disabled={saving || index === 0}
            aria-label={t('layout.moveUp', { widget: label })}
            className="rounded p-1.5 text-gray-400 transition-colors hover:bg-gray-800 hover:text-gray-200 disabled:opacity-30"
          >
            <ChevronUp size={16} />
          </button>
          <button
            type="button"
            onClick={() => handleMove(def.id, 1)}
            disabled={saving || index === displayedIds.length - 1}
            aria-label={t('layout.moveDown', { widget: label })}
            className="rounded p-1.5 text-gray-400 transition-colors hover:bg-gray-800 hover:text-gray-200 disabled:opacity-30"
          >
            <ChevronDown size={16} />
          </button>
          {!def.alwaysVisible && (
            <button
              type="button"
              onClick={() => handleHide(def.id)}
              disabled={saving}
              aria-label={t('layout.hide', { widget: label })}
              className="rounded p-1.5 text-gray-400 transition-colors hover:bg-gray-800 hover:text-gray-200 disabled:opacity-30"
            >
              <EyeOff size={16} />
            </button>
          )}
        </div>
        {/* The preview is inert while editing so a tap reorders instead of
            following a link inside the widget. */}
        <div className="pointer-events-none">{boundary}</div>
      </div>
    )
  }

  return (
    <div className="p-6">
      <DashboardEditBar
        editing={editing}
        saving={saving}
        onToggleEditing={() => setEditing((prev) => !prev)}
        onReset={handleReset}
      />
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {visibleWidgets.map(renderWidget)}
      </div>

      {editing && (
        <section className="mt-6">
          <h2 className="text-xs uppercase tracking-wide text-gray-500">{t('layout.hiddenTitle')}</h2>
          {hiddenList.length === 0 ? (
            <p className="mt-2 text-sm text-gray-500">{t('layout.hiddenEmpty')}</p>
          ) : (
            <ul className="mt-2 flex flex-col gap-2">
              {hiddenList.map((def) => {
                const label = t(def.titleKey)
                return (
                  <li
                    key={def.id}
                    className="flex items-center gap-2 rounded-lg bg-gray-800 px-3 py-2"
                  >
                    <span className="mr-auto truncate text-sm text-gray-300">{label}</span>
                    <button
                      type="button"
                      onClick={() => handleShow(def.id)}
                      disabled={saving}
                      aria-label={t('layout.show', { widget: label })}
                      className="inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs text-blue-400 transition-colors hover:bg-gray-700 hover:text-blue-300 disabled:opacity-40"
                    >
                      <Eye size={14} />
                      {t('layout.showShort')}
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      )}

      <ToastList toasts={toasts} />
    </div>
  )
}

export default Dashboard
