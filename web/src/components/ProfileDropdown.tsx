import { useState, useRef, useEffect } from 'react'
import { useAuth } from '../auth/useAuth.ts'

export function ProfileDropdown() {
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  if (!user) return null

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 rounded-full bg-gray-800 p-1 pr-3 transition hover:bg-gray-700"
      >
        {user.avatarUrl ? (
          <img
            src={user.avatarUrl}
            alt={user.name}
            className="h-8 w-8 rounded-full"
            referrerPolicy="no-referrer"
          />
        ) : (
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-blue-600 text-sm font-bold text-white">
            {user.name.charAt(0).toUpperCase()}
          </div>
        )}
        <span className="text-sm text-gray-200">{user.name}</span>
      </button>

      {open && (
        <div className="absolute right-0 mt-2 w-56 rounded-lg bg-gray-800 py-2 shadow-lg ring-1 ring-gray-700">
          <div className="border-b border-gray-700 px-4 py-2">
            <p className="text-sm font-medium text-white">{user.name}</p>
            <p className="text-xs text-gray-400">{user.email}</p>
          </div>
          <button
            onClick={() => {
              setOpen(false)
              logout()
            }}
            className="w-full px-4 py-2 text-left text-sm text-gray-300 transition hover:bg-gray-700 hover:text-white"
          >
            Sign out
          </button>
        </div>
      )}
    </div>
  )
}
