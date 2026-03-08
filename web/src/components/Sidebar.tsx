import { useState, useEffect } from 'react'
import { NavLink } from 'react-router-dom'
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
  LogIn,
} from 'lucide-react'
import { useAuth } from '../auth'

const COLLAPSED_KEY = 'sidebar-collapsed'

interface NavItem {
  to: string
  icon: React.ReactNode
  label: string
  requiresAuth?: boolean
}

const navItems: NavItem[] = [
  { to: '/', icon: <House size={20} />, label: 'Home' },
  { to: '/dashboard', icon: <LayoutDashboard size={20} />, label: 'Dashboard', requiresAuth: true },
  { to: '/weather', icon: <CloudSun size={20} />, label: 'Weather' },
  { to: '/calendar', icon: <Calendar size={20} />, label: 'Calendar' },
  { to: '/webhooks', icon: <Webhook size={20} />, label: 'Webhooks' },
  { to: '/notes', icon: <FileText size={20} />, label: 'Notes' },
]

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

  const filteredItems = navItems.filter(
    item => !item.requiresAuth || user
  )

  const sidebarContent = (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 h-14 border-b border-gray-800 shrink-0">
        {!collapsed && <h1 className="text-lg font-semibold text-white">Hytte</h1>}
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="ml-auto text-gray-400 hover:text-white transition-colors cursor-pointer hidden md:block"
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed ? <PanelLeftOpen size={20} /> : <PanelLeftClose size={20} />}
        </button>
        {/* Mobile close */}
        <button
          onClick={closeMobile}
          className="ml-auto text-gray-400 hover:text-white transition-colors cursor-pointer md:hidden"
        >
          <X size={20} />
        </button>
      </div>

      {/* Nav items */}
      <nav className="flex-1 py-2 overflow-y-auto">
        {filteredItems.map(item => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            onClick={closeMobile}
            className={({ isActive }) =>
              `flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors ${
                isActive
                  ? 'bg-gray-800 text-white'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
              } ${collapsed ? 'justify-center' : ''}`
            }
            title={collapsed ? item.label : undefined}
          >
            <span className="shrink-0">{item.icon}</span>
            {!collapsed && <span>{item.label}</span>}
          </NavLink>
        ))}
      </nav>

      {/* Bottom section: Profile + Settings */}
      <div className="border-t border-gray-800 py-2 shrink-0">
        {/* User profile or sign in */}
        {!loading && (
          <div className={`px-4 py-2 ${collapsed ? 'flex justify-center' : ''}`}>
            {user ? (
              <div className={`flex items-center gap-3 ${collapsed ? 'justify-center' : ''}`}>
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
              <a
                href="/api/auth/google/login"
                className={`flex items-center gap-3 text-sm text-gray-400 hover:text-white transition-colors ${collapsed ? 'justify-center' : ''}`}
                title={collapsed ? 'Sign in' : undefined}
              >
                <LogIn size={20} className="shrink-0" />
                {!collapsed && <span>Sign in</span>}
              </a>
            )}
          </div>
        )}

        {/* Settings link */}
        <NavLink
          to="/settings"
          onClick={closeMobile}
          className={({ isActive }) =>
            `flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors ${
              isActive
                ? 'bg-gray-800 text-white'
                : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
            } ${collapsed ? 'justify-center' : ''}`
          }
          title={collapsed ? 'Settings' : undefined}
        >
          <Settings size={20} className="shrink-0" />
          {!collapsed && <span>Settings</span>}
        </NavLink>
      </div>
    </div>
  )

  return (
    <>
      {/* Mobile hamburger button */}
      <button
        onClick={() => setMobileOpen(true)}
        className="fixed top-3 left-3 z-50 p-2 rounded-lg bg-gray-800 text-gray-400 hover:text-white transition-colors cursor-pointer md:hidden"
        aria-label="Open menu"
      >
        <Menu size={20} />
      </button>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 bg-black/60 z-40 md:hidden"
          onClick={closeMobile}
        />
      )}

      {/* Mobile slide-out sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-50 w-64 bg-gray-950 transform transition-transform duration-200 md:hidden ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        {sidebarContent}
      </aside>

      {/* Desktop sidebar */}
      <aside
        className={`hidden md:flex flex-col bg-gray-950 border-r border-gray-800 h-screen sticky top-0 shrink-0 transition-[width] duration-200 ${
          collapsed ? 'w-16' : 'w-56'
        }`}
      >
        {sidebarContent}
      </aside>
    </>
  )
}
