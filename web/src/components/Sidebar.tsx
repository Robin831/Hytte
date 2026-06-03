import { useState, useEffect } from 'react'
import { NavLink } from 'react-router-dom'
import {
  House,
  LayoutDashboard,
  CloudSun,
  Calendar,
  Webhook,
  Newspaper,
  FileText,
  Link2,
  MessageSquare,
  MessageCircle,
  Activity,
  Dumbbell,
  Server,
  Shield,
  Settings,
  PanelLeftClose,
  PanelLeftOpen,
  Menu,
  X,
  LogIn,
  LogOut,
  Users,
  Star,
  Bus,
  Coins,
  Clock,
  Hammer,
  Gamepad2,
  PiggyBank,
  Banknote,
  Zap,
  Lock,
  Moon,
  ShoppingCart,
  BookOpen,
  ClipboardList,
  ListTodo,
  Calculator,
  Lightbulb,
  Sparkles,
} from 'lucide-react'
import type { ParseKeys } from 'i18next'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import LanguageSwitcher from './LanguageSwitcher'

const COLLAPSED_KEY = 'sidebar-collapsed'

interface NavItem {
  to: string
  icon: React.ReactNode
  label: ParseKeys<'common'>
  requiresAuth?: boolean
  feature?: string
  requireAdmin?: boolean
  /** When set, item is only shown if the user has this family role. */
  familyRole?: 'parent' | 'child'
}

const navItems: NavItem[] = [
  { to: '/', icon: <House size={20} />, label: 'nav.home' },
  { to: '/dashboard', icon: <LayoutDashboard size={20} />, label: 'nav.dashboard', requiresAuth: true },
  { to: '/weather', icon: <CloudSun size={20} />, label: 'nav.weather' },
  { to: '/calendar', icon: <Calendar size={20} />, label: 'nav.calendar', requiresAuth: true, feature: 'calendar' },
  { to: '/news', icon: <Newspaper size={20} />, label: 'nav.news', requiresAuth: true, feature: 'news' },
  { to: '/webhooks', icon: <Webhook size={20} />, label: 'nav.webhooks', requiresAuth: true, feature: 'webhooks' },
  { to: '/notes', icon: <FileText size={20} />, label: 'nav.notes', requiresAuth: true, feature: 'notes' },
  { to: '/tasks', icon: <ListTodo size={20} />, label: 'nav.tasks', requiresAuth: true, feature: 'tasks' },
  { to: '/vault', icon: <Lock size={20} />, label: 'nav.vault', requiresAuth: true, feature: 'vault' },
  { to: '/chat', icon: <MessageSquare size={20} />, label: 'nav.chat', requiresAuth: true, feature: 'chat' },
  { to: '/family-chat', icon: <MessageCircle size={20} />, label: 'nav.familyChat', requiresAuth: true, feature: 'family_chat' },
  { to: '/training', icon: <Dumbbell size={20} />, label: 'nav.training', requiresAuth: true, feature: 'training' },
  { to: '/training/stride', icon: <Zap size={20} />, label: 'nav.stride', requiresAuth: true, feature: 'stride' },
  { to: '/lactate', icon: <Activity size={20} />, label: 'nav.lactate', requiresAuth: true, feature: 'lactate' },
  { to: '/infra', icon: <Server size={20} />, label: 'nav.infra', requiresAuth: true, feature: 'infra' },
  { to: '/links', icon: <Link2 size={20} />, label: 'nav.links', requiresAuth: true, feature: 'links' },
  { to: '/transit', icon: <Bus size={20} />, label: 'nav.transit', requiresAuth: true, feature: 'transit' },
  { to: '/workhours', icon: <Clock size={20} />, label: 'nav.workhours', requiresAuth: true, feature: 'work_hours' },
  { to: '/budget', icon: <PiggyBank size={20} />, label: 'nav.budget', requiresAuth: true, feature: 'budget' },
  { to: '/salary', icon: <Banknote size={20} />, label: 'nav.salary', requiresAuth: true, feature: 'salary' },
  { to: '/allowance', icon: <Coins size={20} />, label: 'nav.allowance', requiresAuth: true, feature: 'kids_allowance', familyRole: 'parent' },
  { to: '/chores', icon: <Coins size={20} />, label: 'nav.chores', requiresAuth: true, feature: 'kids_allowance', familyRole: 'child' },
  { to: '/family', icon: <Users size={20} />, label: 'nav.family', requiresAuth: true, feature: 'kids_stars' },
  { to: '/stars', icon: <Star size={20} />, label: 'nav.stars', requiresAuth: true, feature: 'kids_stars', familyRole: 'child' },
  { to: '/skywatch', icon: <Moon size={20} />, label: 'nav.skywatch', requiresAuth: true, feature: 'skywatch' },
  { to: '/grocery', icon: <ShoppingCart size={20} />, label: 'nav.grocery', requiresAuth: true, feature: 'grocery' },
  { to: '/wordfeud', icon: <Gamepad2 size={20} />, label: 'nav.wordfeud', requiresAuth: true, feature: 'wordfeud' },
  { to: '/math', icon: <Calculator size={20} />, label: 'nav.regnemester', requiresAuth: true, feature: 'regnemester' },
  { to: '/pokemon', icon: <Sparkles size={20} />, label: 'nav.pokemon', requiresAuth: true, feature: 'pokemon' },
  { to: '/homework', icon: <BookOpen size={20} />, label: 'nav.homework', requiresAuth: true, feature: 'homework', familyRole: 'child' },
  { to: '/homework/review', icon: <ClipboardList size={20} />, label: 'nav.homeworkReview', requiresAuth: true, feature: 'homework', familyRole: 'parent' },
  { to: '/forge/mezzanine', icon: <Hammer size={20} />, label: 'nav.forge', requiresAuth: true, requireAdmin: true },
]

// POKEMON_COUNTS_POLL_MS controls how often the sidebar refreshes the Pokémon
// pending-resolution count. 30 s keeps the badge fresh while a kid is actively
// scanning and resolving without hammering the server in the background.
const POKEMON_COUNTS_POLL_MS = 30000

export default function Sidebar() {
  const { t } = useTranslation('common')
  const { t: tPokemon } = useTranslation('pokemon')
  const { user, loading, logout, hasFeature, familyStatus } = useAuth()
  const [collapsed, setCollapsed] = useState(() => {
    return localStorage.getItem(COLLAPSED_KEY) === 'true'
  })
  const [mobileOpen, setMobileOpen] = useState(false)
  const [pendingClaimsCount, setPendingClaimsCount] = useState(0)
  const [pokemonUnresolvedCount, setPokemonUnresolvedCount] = useState(0)

  useEffect(() => {
    if (!user || !familyStatus?.is_parent) return
    let cancelled = false
    fetch('/api/family/claims?status=pending', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : { claims: [] }))
      .then((data: { claims: unknown[] }) => {
        if (!cancelled) setPendingClaimsCount(data.claims?.length ?? 0)
      })
      .catch(() => { /* badge is non-critical */ })
    return () => { cancelled = true }
  }, [user, familyStatus])

  // Poll the Pokémon scan counts endpoint while the sidebar is mounted so the
  // pending-resolution badge stays roughly in sync with the worker's progress.
  // Only fires when the user actually has the pokemon feature enabled — no
  // sense paying a 401/403 round trip for users without access.
  useEffect(() => {
    if (!user || !hasFeature('pokemon')) {
      return
    }
    let cancelled = false
    const fetchCounts = () => {
      fetch('/api/pokemon/scans/counts', { credentials: 'include' })
        .then(res => (res.ok ? res.json() : { unresolved: 0 }))
        .then((data: { unresolved?: number }) => {
          if (!cancelled) setPokemonUnresolvedCount(data.unresolved ?? 0)
        })
        .catch(() => { /* badge is non-critical */ })
    }
    fetchCounts()
    const interval = window.setInterval(fetchCounts, POKEMON_COUNTS_POLL_MS)
    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [user, hasFeature])

  useEffect(() => {
    localStorage.setItem(COLLAPSED_KEY, String(collapsed))
  }, [collapsed])

  // Close mobile menu on route change via click
  const closeMobile = () => setMobileOpen(false)

  const filteredItems = navItems.filter(item => {
    if (item.requiresAuth && !user) return false
    if (item.requireAdmin && !user?.is_admin) return false
    if (item.feature && !hasFeature(item.feature)) return false
    if (item.familyRole === 'parent' && !familyStatus?.is_parent && !user?.is_admin) return false
    if (item.familyRole === 'child' && !familyStatus?.is_child) return false
    return true
  })

  const renderSidebar = (isCollapsed: boolean, isMobile: boolean) => (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 h-14 border-b border-gray-800 shrink-0">
        {!isCollapsed && <h1 className="text-lg font-semibold text-white">{t('appName')}</h1>}
        {!isMobile && (
          <button
            onClick={() => setCollapsed(!collapsed)}
            className="ml-auto text-gray-400 hover:text-white transition-colors cursor-pointer"
            title={isCollapsed ? t('sidebar.expandSidebar') : t('sidebar.collapseSidebar')}
          >
            {isCollapsed ? <PanelLeftOpen size={20} /> : <PanelLeftClose size={20} />}
          </button>
        )}
        {isMobile && (
          <button
            onClick={closeMobile}
            className="ml-auto text-gray-400 hover:text-white transition-colors cursor-pointer"
          >
            <X size={20} />
          </button>
        )}
      </div>

      {/* Nav items */}
      <nav className="flex-1 py-2 overflow-y-auto">
        {filteredItems.map(item => {
          const showPokemonBadge = item.to === '/pokemon' && pokemonUnresolvedCount > 0
          const pokemonBadgeText = pokemonUnresolvedCount > 9 ? '9+' : String(pokemonUnresolvedCount)
          const pokemonBadgeAria = tPokemon('nav.pendingBadgeAria', { count: pokemonUnresolvedCount })
          return (
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
                } ${isCollapsed ? 'justify-center' : ''}`
              }
              title={isCollapsed ? t(item.label) : undefined}
            >
              <span className="relative shrink-0">
                {item.icon}
                {item.to === '/family' && pendingClaimsCount > 0 && (
                  <span className="absolute -top-1.5 -right-1.5 min-w-[14px] h-[14px] flex items-center justify-center rounded-full bg-red-500 text-white text-[9px] font-bold leading-none px-0.5">
                    {pendingClaimsCount > 99 ? '99+' : pendingClaimsCount}
                  </span>
                )}
                {showPokemonBadge && (
                  <span
                    aria-label={pokemonBadgeAria}
                    data-testid="sidebar-pokemon-badge"
                    className="absolute -top-1.5 -right-1.5 min-w-[14px] h-[14px] flex items-center justify-center rounded-full bg-red-500 text-white text-[9px] font-bold leading-none px-0.5"
                  >
                    {pokemonBadgeText}
                  </span>
                )}
              </span>
              {!isCollapsed && <span>{t(item.label)}</span>}
              {!isCollapsed && item.to === '/family' && pendingClaimsCount > 0 && (
                <span className="ml-auto min-w-[18px] h-[18px] flex items-center justify-center rounded-full bg-red-500 text-white text-[10px] font-bold leading-none px-1">
                  {pendingClaimsCount > 99 ? '99+' : pendingClaimsCount}
                </span>
              )}
              {!isCollapsed && showPokemonBadge && (
                <span
                  aria-label={pokemonBadgeAria}
                  className="ml-auto min-w-[18px] h-[18px] flex items-center justify-center rounded-full bg-red-500 text-white text-[10px] font-bold leading-none px-1"
                >
                  {pokemonBadgeText}
                </span>
              )}
            </NavLink>
          )
        })}
      </nav>

      {/* Bottom section: Profile + Settings */}
      <div className="border-t border-gray-800 py-2 shrink-0">
        {/* User profile or sign in */}
        {!loading && (
          <div className={`px-4 py-2 ${isCollapsed ? 'flex justify-center' : ''}`}>
            {user ? (
              <div className={`flex items-center gap-3 ${isCollapsed ? 'justify-center' : ''}`}>
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
                {!isCollapsed && (
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium text-white truncate">{user.name}</p>
                    <p className="text-xs text-gray-500 truncate">{user.email}</p>
                  </div>
                )}
              </div>
            ) : (
              <a
                href="/api/auth/google/login"
                className={`flex items-center gap-3 text-sm text-gray-400 hover:text-white transition-colors ${isCollapsed ? 'justify-center' : ''}`}
                title={isCollapsed ? t('sidebar.signIn') : undefined}
              >
                <LogIn size={20} className="shrink-0" />
                {!isCollapsed && <span>{t('sidebar.signIn')}</span>}
              </a>
            )}
          </div>
        )}

        {/* Language switcher */}
        <LanguageSwitcher compact collapsed={isCollapsed} />

        {/* Sign out button */}
        {!loading && user && (
          <button
            onClick={async () => {
              await logout()
              closeMobile()
            }}
            className={`flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors text-gray-400 hover:text-white hover:bg-gray-800/50 w-[calc(100%-1rem)] cursor-pointer ${isCollapsed ? 'justify-center' : ''}`}
            title={isCollapsed ? t('sidebar.signOut') : undefined}
          >
            <LogOut size={20} className="shrink-0" />
            {!isCollapsed && <span>{t('sidebar.signOut')}</span>}
          </button>
        )}

        {/* Admin link (only for admin users) */}
        {!loading && user?.is_admin && (
          <NavLink
            to="/admin"
            onClick={closeMobile}
            className={({ isActive }) =>
              `flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors ${
                isActive
                  ? 'bg-gray-800 text-white'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
              } ${isCollapsed ? 'justify-center' : ''}`
            }
            title={isCollapsed ? t('nav.admin') : undefined}
          >
            <Shield size={20} className="shrink-0" />
            {!isCollapsed && <span>{t('nav.admin')}</span>}
          </NavLink>
        )}

        {/* Suggestions link (only for admin users) */}
        {!loading && user?.is_admin && (
          <NavLink
            to="/suggestions"
            onClick={closeMobile}
            className={({ isActive }) =>
              `flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors ${
                isActive
                  ? 'bg-gray-800 text-white'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
              } ${isCollapsed ? 'justify-center' : ''}`
            }
            title={isCollapsed ? t('nav.suggestions') : undefined}
          >
            <Lightbulb size={20} className="shrink-0" />
            {!isCollapsed && <span>{t('nav.suggestions')}</span>}
          </NavLink>
        )}

        {/* Settings link (only for authenticated users) */}
        {!loading && user && (
          <NavLink
            to="/settings"
            onClick={closeMobile}
            className={({ isActive }) =>
              `flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors ${
                isActive
                  ? 'bg-gray-800 text-white'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
              } ${isCollapsed ? 'justify-center' : ''}`
            }
            title={isCollapsed ? t('nav.settings') : undefined}
          >
            <Settings size={20} className="shrink-0" />
            {!isCollapsed && <span>{t('nav.settings')}</span>}
          </NavLink>
        )}
      </div>
    </div>
  )

  return (
    <>
      {/* Mobile hamburger button */}
      <button
        onClick={() => setMobileOpen(true)}
        className="fixed top-3 left-3 z-50 p-2 rounded-lg bg-gray-800 text-gray-400 hover:text-white transition-colors cursor-pointer md:hidden"
        aria-label={t('sidebar.openMenu')}
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

      {/* Mobile slide-out sidebar — always expanded */}
      <aside
        className={`fixed inset-y-0 left-0 z-50 w-64 bg-gray-950 transform transition-transform duration-200 md:hidden ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        {renderSidebar(false, true)}
      </aside>

      {/* Desktop sidebar */}
      <aside
        className={`hidden md:flex flex-col bg-gray-950 border-r border-gray-800 h-screen sticky top-0 shrink-0 transition-[width] duration-200 ${
          collapsed ? 'w-16' : 'w-56'
        }`}
      >
        {renderSidebar(collapsed, false)}
      </aside>
    </>
  )
}
