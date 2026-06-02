import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { DEDUCTION_EMOJIS, getPresetIconDisplay } from '../presetIcons'

interface EmojiPickerDropdownProps {
  value: string
  onChange: (emoji: string) => void
  customInputId: string
  buttonClassName?: string
}

export default function EmojiPickerDropdown({ value, onChange, customInputId, buttonClassName }: EmojiPickerDropdownProps) {
  const { t } = useTranslation(['workhours'])
  const [showPicker, setShowPicker] = useState(false)
  const pickerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (showPicker) pickerRef.current?.focus()
  }, [showPicker])

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setShowPicker(p => !p)}
        onKeyDown={e => { if (e.key === 'Escape') setShowPicker(false) }}
        className={`w-14 text-white rounded px-2 py-1.5 text-lg text-center border border-gray-600 focus:border-blue-500 focus:outline-none cursor-pointer ${buttonClassName ?? 'bg-gray-700'}`}
        aria-label={t('workhours:chooseIcon')}
        aria-haspopup="dialog"
        aria-expanded={showPicker}
      >
        {getPresetIconDisplay(value)}
      </button>
      {showPicker && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setShowPicker(false)} />
          <div
            role="dialog"
            aria-modal="true"
            aria-label={t('workhours:chooseIcon')}
            tabIndex={-1}
            ref={pickerRef}
            onKeyDown={e => { if (e.key === 'Escape') setShowPicker(false) }}
            className="absolute right-0 top-full mt-1 bg-gray-800 border border-gray-600 rounded-xl p-3 z-20 w-64 shadow-xl focus:outline-none"
          >
            {DEDUCTION_EMOJIS.map(({ key, emojis }) => (
              <div key={key} className="mb-3 last:mb-0">
                <p className="text-xs text-gray-400 mb-1">{t(`workhours:emojiCategories.${key}` as never, { defaultValue: key })}</p>
                <div className="flex flex-wrap gap-1">
                  {emojis.map(emoji => (
                    <button
                      key={emoji}
                      type="button"
                      onClick={() => { onChange(emoji); setShowPicker(false) }}
                      className={`text-xl p-1.5 rounded-lg transition-colors cursor-pointer ${value === emoji ? 'bg-blue-600' : 'hover:bg-gray-600'}`}
                    >
                      {emoji}
                    </button>
                  ))}
                </div>
              </div>
            ))}
            <div className="mt-2 border-t border-gray-600 pt-2">
              <label htmlFor={customInputId} className="block text-xs text-gray-400 mb-1">{t('workhours:customEmoji')}</label>
              <input
                id={customInputId}
                type="text"
                value={value ?? ''}
                onChange={e => onChange(e.target.value)}
                className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm text-center focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
        </>
      )}
    </div>
  )
}
