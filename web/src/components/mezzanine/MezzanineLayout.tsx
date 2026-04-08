import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import StatusBar from './StatusBar'

interface MezzanineLayoutProps {
  sidebar?: ReactNode
  children?: ReactNode
  showToast?: (message: string, type: 'success' | 'error') => void
  headerActions?: ReactNode
  onNeedsAttentionClick?: () => void
}

export default function MezzanineLayout({ sidebar, children, showToast, headerActions, onNeedsAttentionClick }: MezzanineLayoutProps) {
  const { t } = useTranslation('forge')

  return (
    <div
      className={[
        'h-full min-h-0 grid',
        // Row 1: status bar (auto height), Row 2: main area (fills rest)
        'grid-rows-[auto_1fr]',
        // On md+: two columns (sidebar | floor). Below md: single column.
        sidebar ? 'md:grid-cols-[theme(spacing.72)_1fr] lg:grid-cols-[theme(spacing.80)_1fr]' : '',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {/* Status bar — spans full width */}
      <header
        className={[
          'shrink-0 border-b border-gray-700/50 bg-gray-900/80 px-4 py-2',
          sidebar ? 'col-span-1 md:col-span-2' : '',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        <div className="flex items-center justify-between gap-4">
          <h1 className="text-lg font-semibold text-white whitespace-nowrap">
            {t('mezzanine.title')}
          </h1>
          <div className="flex items-center gap-2 flex-wrap">
            {headerActions}
            <StatusBar showToast={showToast} onNeedsAttentionClick={onNeedsAttentionClick} />
          </div>
        </div>
      </header>

      {/* Left sidebar — grid cell, hidden below md */}
      {sidebar && (
        <aside className="hidden md:flex flex-col border-r border-gray-700/50 bg-gray-900/60 overflow-y-auto row-start-2">
          {sidebar}
        </aside>
      )}

      {/* Center floor */}
      <main
        className={[
          'min-w-0 overflow-y-auto p-4 row-start-2',
          sidebar ? 'col-start-1 md:col-start-2' : '',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        {children}
      </main>

      {/* Mobile sidebar — shown below md as a bottom section outside grid flow */}
      {sidebar && (
        <div className="md:hidden border-t border-gray-700/50 bg-gray-900/60 max-h-64 overflow-y-auto col-span-1">
          {sidebar}
        </div>
      )}
    </div>
  )
}
