import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Filter, Check, Eye, EyeOff } from 'lucide-react'

interface AnvilFilterDropdownProps {
  anvils: string[]
  hiddenAnvils: Set<string>
  onToggle: (anvil: string) => void
  onShowAll: () => void
  onHideAll: (anvils: string[]) => void
  hasFilter: boolean
}

export default function AnvilFilterDropdown({
  anvils,
  hiddenAnvils,
  onToggle,
  onShowAll,
  onHideAll,
  hasFilter,
}: AnvilFilterDropdownProps) {
  const { t } = useTranslation('forge')
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function handleClickOutside(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    function handleEscape(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('keydown', handleEscape)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        aria-label={t('mezzanine.anvilFilter.label')}
        className={[
          'flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs transition-colors border',
          hasFilter
            ? 'bg-cyan-500/20 text-cyan-300 border-cyan-600/30 hover:bg-cyan-500/30'
            : 'bg-gray-800 text-gray-400 border-gray-700 hover:bg-gray-700',
        ].join(' ')}
      >
        <Filter size={12} />
        {t('mezzanine.anvilFilter.button')}
        {hasFilter && (
          <span className="ml-1 flex items-center justify-center min-w-[16px] h-4 px-1 rounded-full bg-cyan-500/30 text-[10px] font-medium">
            {anvils.length - hiddenAnvils.size}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 top-full mt-1 w-48 rounded-lg bg-gray-800 border border-gray-700 shadow-xl z-20 py-1">
          <div className="flex items-center justify-between px-3 py-1.5 border-b border-gray-700/50">
            <span className="text-[10px] text-gray-500 uppercase tracking-wide">
              {t('mezzanine.anvilFilter.heading')}
            </span>
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={onShowAll}
                className="text-[10px] text-blue-400 hover:text-blue-300"
                aria-label={t('mezzanine.anvilFilter.showAll')}
              >
                <Eye size={12} />
              </button>
              <button
                type="button"
                onClick={() => onHideAll(anvils)}
                className="text-[10px] text-gray-500 hover:text-gray-300"
                aria-label={t('mezzanine.anvilFilter.hideAll')}
              >
                <EyeOff size={12} />
              </button>
            </div>
          </div>
          {anvils.map(anvil => {
            const visible = !hiddenAnvils.has(anvil)
            return (
              <button
                key={anvil}
                type="button"
                onClick={() => onToggle(anvil)}
                aria-pressed={visible}
                aria-label={anvil}
                className="flex items-center gap-2 w-full px-3 py-1.5 text-xs text-left hover:bg-gray-700/50 transition-colors"
              >
                <span className={`w-3.5 h-3.5 rounded border flex items-center justify-center ${
                  visible ? 'bg-cyan-500 border-cyan-500' : 'bg-gray-700 border-gray-600'
                }`}>
                  {visible && <Check size={10} className="text-white" />}
                </span>
                <span className={visible ? 'text-gray-200' : 'text-gray-500'}>{anvil}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
