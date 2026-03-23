import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Globe } from 'lucide-react'

const languages = [
  { code: 'en', label: 'English', flag: 'EN' },
  { code: 'nb', label: 'Norsk (Bokmål)', flag: 'NO' },
  { code: 'th', label: 'ไทย', flag: 'TH' },
]

interface LanguageSwitcherProps {
  compact?: boolean
  collapsed?: boolean
}

export default function LanguageSwitcher({ compact = false, collapsed = false }: LanguageSwitcherProps) {
  const { i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const current = languages.find((l) => l.code === i18n.language) ?? languages[0]

  const changeLanguage = (lng: string) => {
    i18n.changeLanguage(lng)
    document.documentElement.lang = lng
    setOpen(false)
  }

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  if (compact) {
    return (
      <div ref={ref} className="relative">
        <button
          onClick={() => setOpen(!open)}
          className={`flex items-center gap-2 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors text-gray-400 hover:text-white hover:bg-gray-800/50 w-[calc(100%-1rem)] cursor-pointer ${collapsed ? 'justify-center' : ''}`}
          title={collapsed ? `Language: ${current.label}` : undefined}
        >
          <Globe size={20} className="shrink-0" />
          {!collapsed && <span className="text-xs font-medium">{current.flag}</span>}
        </button>
        {open && (
          <div className="absolute bottom-full left-2 mb-1 w-44 bg-gray-800 border border-gray-700 rounded-lg shadow-lg overflow-hidden z-50">
            {languages.map((lang) => (
              <button
                key={lang.code}
                onClick={() => changeLanguage(lang.code)}
                className={`flex items-center gap-3 w-full px-3 py-2 text-sm transition-colors cursor-pointer ${
                  lang.code === i18n.language
                    ? 'bg-gray-700 text-white'
                    : 'text-gray-300 hover:bg-gray-700 hover:text-white'
                }`}
              >
                <span className="text-xs font-mono font-medium text-gray-400 w-6">{lang.flag}</span>
                <span>{lang.label}</span>
              </button>
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 cursor-pointer hover:bg-gray-600 transition-colors"
      >
        <Globe size={16} className="text-gray-400 shrink-0" />
        <span className="flex-1 text-left">{current.label}</span>
        <svg className="w-4 h-4 text-gray-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>
      {open && (
        <div className="absolute top-full left-0 mt-1 w-full bg-gray-800 border border-gray-700 rounded-lg shadow-lg overflow-hidden z-50">
          {languages.map((lang) => (
            <button
              key={lang.code}
              onClick={() => changeLanguage(lang.code)}
              className={`flex items-center gap-3 w-full px-3 py-2.5 text-sm transition-colors cursor-pointer ${
                lang.code === i18n.language
                  ? 'bg-gray-700 text-white'
                  : 'text-gray-300 hover:bg-gray-700 hover:text-white'
              }`}
            >
              <span className="text-xs font-mono font-medium text-gray-400 w-6">{lang.flag}</span>
              <span>{lang.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
