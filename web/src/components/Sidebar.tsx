import { useState, useEffect } from 'react'
import { NavLink, Link } from 'react-router-dom'
import {
  House,
  LayoutDashboard,
  CloudSun,
  Calendar,
  Webhook,
  FileText,
  Settings,
  PanelLeftClose,
  PanelLeftOpen,
  Menu,
  X,
} from 'lucide-react'
import { useAuth } from '../auth'
import LoginButton from './LoginButton'

const COLLAPSED_KEY = 'sidebar-collapsed'

interface NavItem {
  to: string
  label: string
  icon: React.ReactNode
  authRequired?: boolean
}

const navItems: NavItem[] = [
  { to: '/', label: 'Home', icon: <House size={20} /> },
  { to: '/dashboard', label: 'Dashboard', icon: <LayoutDashboard size={20} />, authRequired: true },
  { to: '/weather', label: 'Weather', icon: <CloudSun size={20} /> },
  { to: '/calendar', label: 'Calendar', icon: <Calendar size={20} /> },
  { to: '/webhooks', label: 'Webhooks', icon: <Webhook size={20} /> },
  { to: '/notes', label: 'Notes', icon: <FileText size={20} /> },
]

const settingsItem: NavItem = {
  to: '/settings',
  label: 'Settings',
  icon: <Settings size={20} />,
}

export default function Sidebar() {
  const { user, loading } = useAuth()
  const [collapsed, setCollapsed] = useState(() => {
    return localStorage.getItem(COLLAPSED_KEY) === 'true'
  })
  const [mobileOpen, setMobileOpen] = useState(false)

  useEffect(() => {
    localStorage.setItem(COLLAPSED_KEY, String(collapsed))
  }, [collapsed])

  // Close mobile menu on route change via click
  const closeMobile = () => setMobileOpen(false)

  const visibleItems = navItems.filter(
    item => !item.authRequired || user
  )

  const sidebarContent = (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between px-4 h-14 border-b border-gray-800">
        {!collapsed && (
          <Link to="/" onClick={closeMobile} className="text-lg font-semibold text-white">
            Hytte
          </Link>
        )}
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="hidden md:flex items-center justify-center w-8 h-8 rounded-md text-gray-400 hover:text-white hover:bg-gray-800 transition-colors cursor-pointer"
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed ? <PanelLeftOpen size={18} /> : <PanelLeftClose size={18} />}
        </button>
        <button
          onClick={closeMobile}
          className="md:hidden flex items-center justify-center w-8 h-8 rounded-md text-gray-400 hover:text-white hover:bg-gray-800 transition-colors cursor-pointer"
        >
          <X size={18} />
        </button>
      </div>

      {/* Nav items */}
      <nav className="flex-1 px-2 py-3 space-y-1 overflow-y-auto">
        {visibleItems.map(item => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            onClick={closeMobile}
            className={({ isActive }) =>
              `flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-gray-800 text-white'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
              } ${collapsed ? 'justify-center' : ''}`
            }
            title={collapsed ? item.label : undefined}
          >
            {item.icon}
            {!collapsed && <span>{item.label}</span>}
          </NavLink>
        ))}
      </nav>

      {/* Bottom section */}
      <div className="px-2 pb-3 space-y-1 border-t border-gray-800 pt-3">
        {/* User profile or sign in */}
        {!loading && (
          user ? (
            <div
              className={`flex items-center gap-3 px-3 py-2 rounded-lg ${
                collapsed ? 'justify-center' : ''
              }`}
            >
              {user.picture ? (
                <img
                  src={user.picture}
                  alt={user.name}
                  className="w-8 h-8 rounded-full shrink-0"
                  referrerPolicy="no-referrer"
                />
              ) : (
                <div className="w-8 h-8 rounded-full bg-blue-600 flex items-center justify-center text-sm font-medium shrink-0">
                  {user.name.charAt(0).toUpperCase()}
                </div>
              )}
              {!collapsed && (
                <div className="min-w-0">
                  <p className="text-sm font-medium text-white truncate">{user.name}</p>
                  <p className="text-xs text-gray-500 truncate">{user.email}</p>
                </div>
              )}
            </div>
          ) : (
            <div className={`px-3 py-2 ${collapsed ? 'flex justify-center' : ''}`}>
              {collapsed ? (
                <a
                  href="/api/auth/google/login"
                  className="flex items-center justify-center w-8 h-8 rounded-full bg-gray-800 text-gray-400 hover:text-white hover:bg-gray-700 transition-colors"
                  title="Sign in"
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24">
                    <path
                      d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"
                      fill="#4285F4"
                    />
                    <path
                      d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                      fill="#34A853"
                    />
                    <path
                      d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
                      fill="#FBBC05"
                    />
                    <path
                      d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
                      fill="#EA4335"
                    />
                  </svg>
                </a>
              ) : (
                <LoginButton />
              )}
            </div>
          )
        )}

        {/* Settings */}
        <NavLink
          to={settingsItem.to}
          onClick={closeMobile}
          className={({ isActive }) =>
            `flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
              isActive
                ? 'bg-gray-800 text-white'
                : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
            } ${collapsed ? 'justify-center' : ''}`
          }
          title={collapsed ? settingsItem.label : undefined}
        >
          {settingsItem.icon}
          {!collapsed && <span>{settingsItem.label}</span>}
        </NavLink>
      </div>
    </div>
  )

  return (
    <>
      {/* Mobile hamburger button */}
      <button
        onClick={() => setMobileOpen(true)}
        className="md:hidden fixed top-3 left-3 z-50 flex items-center justify-center w-10 h-10 rounded-lg bg-gray-800 text-gray-400 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer"
        aria-label="Open menu"
      >
        <Menu size={20} />
      </button>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="md:hidden fixed inset-0 z-40 bg-black/60"
          onClick={closeMobile}
        />
      )}

      {/* Mobile sidebar */}
      <aside
        className={`md:hidden fixed inset-y-0 left-0 z-50 w-64 bg-gray-950 transform transition-transform duration-200 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        {sidebarContent}
      </aside>

      {/* Desktop sidebar */}
      <aside
        className={`hidden md:flex flex-col shrink-0 bg-gray-950 h-screen sticky top-0 transition-[width] duration-200 ${
          collapsed ? 'w-16' : 'w-56'
        }`}
      >
        {sidebarContent}
      </aside>
    </>
  )
}
