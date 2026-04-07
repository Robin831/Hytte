import { useTranslation } from 'react-i18next'
import StatusBar from './StatusBar'

interface MezzanineLayoutProps {
  sidebar?: React.ReactNode
  children?: React.ReactNode
}

export default function MezzanineLayout({ sidebar, children }: MezzanineLayoutProps) {
  const { t } = useTranslation('forge')

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Status bar — top zone */}
      <header className="shrink-0 border-b border-gray-700/50 bg-gray-900/80 px-4 py-2">
        <div className="flex items-center justify-between gap-4">
          <h1 className="text-lg font-semibold text-white whitespace-nowrap">
            {t('mezzanine.title')}
          </h1>
          <StatusBar />
        </div>
      </header>

      {/* Main area: sidebar + floor */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Left sidebar — collapses below md */}
        {sidebar && (
          <aside className="hidden md:flex md:w-72 lg:w-80 shrink-0 flex-col border-r border-gray-700/50 bg-gray-900/60 overflow-y-auto">
            {sidebar}
          </aside>
        )}

        {/* Center floor */}
        <main className="flex-1 min-w-0 overflow-y-auto p-4">
          {children}
        </main>
      </div>

      {/* Mobile sidebar — shown below md as a bottom section */}
      {sidebar && (
        <div className="md:hidden border-t border-gray-700/50 bg-gray-900/60 max-h-64 overflow-y-auto">
          {sidebar}
        </div>
      )}
    </div>
  )
}
