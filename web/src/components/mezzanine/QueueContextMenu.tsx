import { useState, useRef, useEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { MoreVertical, Play, MessageCircle, Tag, XCircle } from 'lucide-react'
import ConfirmDialog from '../ConfirmDialog'
import { useBeadActions } from '../../hooks/useBeadActions'

interface QueueContextMenuProps {
  beadId: string
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
  onActionComplete?: () => void
}

type ConfirmableAction = 'runNow' | 'dismiss'

export default function QueueContextMenu({
  beadId,
  showToast,
  onBeadClick,
  onActionComplete,
}: QueueContextMenuProps) {
  const { t } = useTranslation('forge')
  const [menuOpen, setMenuOpen] = useState(false)
  const [dropdownPos, setDropdownPos] = useState<{ top: number; right: number } | null>(null)
  const [confirmAction, setConfirmAction] = useState<ConfirmableAction | null>(null)
  const [tagging, setTagging] = useState(false)
  const btnRef = useRef<HTMLButtonElement>(null)
  const portalRef = useRef<HTMLDivElement>(null)
  const onActionCompleteRef = useRef(onActionComplete)

  useEffect(() => {
    onActionCompleteRef.current = onActionComplete
  })

  const { acting, handleAction } = useBeadActions({ showToast })

  const isActing = !!acting[beadId] || tagging

  const openMenu = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    if (!btnRef.current) return
    const rect = btnRef.current.getBoundingClientRect()
    setDropdownPos({ top: rect.bottom + 4, right: window.innerWidth - rect.right })
    setMenuOpen(true)
  }, [])

  const closeMenu = useCallback(() => {
    setMenuOpen(false)
    setDropdownPos(null)
    btnRef.current?.focus()
  }, [])

  // Focus first menu item when menu opens
  useEffect(() => {
    if (menuOpen) {
      requestAnimationFrame(() => {
        portalRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus()
      })
    }
  }, [menuOpen])

  // Close on click outside
  useEffect(() => {
    if (!menuOpen) return
    const handleMouseDown = (e: MouseEvent) => {
      if (
        portalRef.current && !portalRef.current.contains(e.target as Node) &&
        btnRef.current && !btnRef.current.contains(e.target as Node)
      ) {
        closeMenu()
      }
    }
    document.addEventListener('mousedown', handleMouseDown)
    return () => document.removeEventListener('mousedown', handleMouseDown)
  }, [menuOpen, closeMenu])

  // Close on scroll
  useEffect(() => {
    if (!menuOpen) return
    const handleScroll = () => closeMenu()
    window.addEventListener('scroll', handleScroll, true)
    return () => window.removeEventListener('scroll', handleScroll, true)
  }, [menuOpen, closeMenu])

  const handleMenuKeyDown = useCallback((e: React.KeyboardEvent) => {
    const items = Array.from(
      portalRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? []
    )
    const currentIdx = items.indexOf(e.target as HTMLButtonElement)

    switch (e.key) {
      case 'ArrowDown': {
        e.preventDefault()
        const next = currentIdx < items.length - 1 ? currentIdx + 1 : 0
        items[next]?.focus()
        break
      }
      case 'ArrowUp': {
        e.preventDefault()
        const prev = currentIdx > 0 ? currentIdx - 1 : items.length - 1
        items[prev]?.focus()
        break
      }
      case 'Home':
        e.preventDefault()
        items[0]?.focus()
        break
      case 'End':
        e.preventDefault()
        items[items.length - 1]?.focus()
        break
      case 'Escape':
        e.preventDefault()
        closeMenu()
        break
      case 'Tab':
        closeMenu()
        break
    }
  }, [closeMenu])

  const handleRunNow = () => {
    closeMenu()
    setConfirmAction('runNow')
  }

  const handleClarify = () => {
    closeMenu()
    onBeadClick?.(beadId)
  }

  const handleTag = async () => {
    closeMenu()
    setTagging(true)
    try {
      const res = await fetch(
        `/api/forge/beads/${encodeURIComponent(beadId)}/labels`,
        {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ label: 'forgeReady' }),
        }
      )
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('mezzanine.pipeline.queueMenu.tagSuccess', { id: beadId }), 'success')
        onActionCompleteRef.current?.()
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setTagging(false)
    }
  }

  const handleDismiss = () => {
    closeMenu()
    setConfirmAction('dismiss')
  }

  const executeConfirmedAction = async () => {
    if (!confirmAction) return
    const action = confirmAction
    setConfirmAction(null)
    let success: boolean
    if (action === 'runNow') {
      success = await handleAction('forceSmith', beadId)
    } else {
      success = await handleAction('dismiss', beadId)
    }
    if (success) {
      onActionCompleteRef.current?.()
    }
  }

  const menuItems = [
    { key: 'runNow', icon: Play, label: t('mezzanine.pipeline.queueMenu.runNow'), onClick: handleRunNow, className: 'text-green-300 hover:bg-green-900/30' },
    { key: 'clarify', icon: MessageCircle, label: t('mezzanine.pipeline.queueMenu.clarify'), onClick: handleClarify, className: 'text-blue-300 hover:bg-blue-900/30' },
    { key: 'tag', icon: Tag, label: t('mezzanine.pipeline.queueMenu.tag'), onClick: () => void handleTag(), className: 'text-purple-300 hover:bg-purple-900/30' },
    { key: 'dismiss', icon: XCircle, label: t('mezzanine.pipeline.queueMenu.dismiss'), onClick: handleDismiss, className: 'text-red-300 hover:bg-red-900/30' },
  ]

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        onClick={openMenu}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.stopPropagation()
          }
        }}
        disabled={isActing}
        aria-label={t('mezzanine.pipeline.queueMenu.actionsLabel', { id: beadId })}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        className="flex items-center justify-center h-5 w-5 rounded text-gray-500 hover:text-gray-300 hover:bg-gray-700/50 transition-colors opacity-0 group-hover:opacity-100 focus:opacity-100 disabled:opacity-50 disabled:cursor-not-allowed"
      >
        <MoreVertical size={14} aria-hidden="true" />
      </button>

      {menuOpen && dropdownPos && createPortal(
        <div
          ref={portalRef}
          style={{ position: 'fixed', top: dropdownPos.top, right: dropdownPos.right, zIndex: 9999 }}
          className="w-44 rounded-lg bg-gray-800 border border-gray-600 shadow-xl py-1 overflow-hidden"
          role="menu"
          aria-label={t('mezzanine.pipeline.queueMenu.actionsLabel', { id: beadId })}
          onKeyDown={handleMenuKeyDown}
        >
          {menuItems.map((item) => (
            <button
              key={item.key}
              type="button"
              role="menuitem"
              tabIndex={-1}
              onClick={item.onClick}
              disabled={isActing}
              className={`w-full flex items-center gap-2 px-3 py-2 text-xs transition-colors disabled:opacity-50 focus:outline-none focus:bg-gray-700/60 ${item.className}`}
            >
              <item.icon size={14} aria-hidden="true" />
              <span>{item.label}</span>
            </button>
          ))}
        </div>,
        document.body
      )}

      <ConfirmDialog
        open={confirmAction === 'runNow'}
        title={t('mezzanine.pipeline.queueMenu.runNowConfirmTitle')}
        message={t('mezzanine.pipeline.queueMenu.runNowConfirmMessage', { id: beadId })}
        confirmLabel={t('mezzanine.pipeline.queueMenu.runNow')}
        onConfirm={() => void executeConfirmedAction()}
        onCancel={() => setConfirmAction(null)}
      />

      <ConfirmDialog
        open={confirmAction === 'dismiss'}
        title={t('attention.dismissConfirmTitle')}
        message={t('attention.dismissConfirmMessage', { id: beadId })}
        confirmLabel={t('attention.dismiss')}
        destructive
        onConfirm={() => void executeConfirmedAction()}
        onCancel={() => setConfirmAction(null)}
      />
    </>
  )
}
