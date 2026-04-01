import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation, Trans } from 'react-i18next'
import { formatDate, formatDateTime, formatNumber } from '../utils/formatDate'
import { useAuth } from '../auth'
import { ConfirmDialog } from '../components/ui/dialog'

import {
  RefreshCw,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  HelpCircle,
  ToggleLeft,
  ToggleRight,
  ChevronLeft,
  Plus,
  Trash2,
  Clock,
  Shield,
  Activity,
  Server,
  ArrowUpDown,
  Container,
  Eye,
  EyeOff,
  GitBranch,
  Globe,
  Database,
  Cog,
  Loader2,
  Package,
  GitCommitHorizontal,
  Download,
} from 'lucide-react'

interface ModuleInfo {
  name: string
  display_name: string
  description: string
  enabled: boolean
}

interface ModuleResult {
  name: string
  status: 'ok' | 'degraded' | 'down' | 'unknown'
  message?: string
  details?: Record<string, unknown>
  checked_at: string
}

interface StatusResponse {
  overall: 'ok' | 'degraded' | 'down' | 'unknown'
  modules: ModuleResult[]
}

interface HealthService {
  id: number
  name: string
  url: string
  created_at: string
}

interface SSLHost {
  id: number
  name: string
  hostname: string
  port: number
  created_at: string
}

interface UptimeRecord {
  id: number
  module: string
  target: string
  status: string
  message: string
  checked_at: string
}

interface UptimeStats {
  uptime_24h: number
  uptime_7d: number
  uptime_30d: number
  total_checks: number
}

const statusConfig = {
  ok: { icon: CheckCircle2, color: 'text-green-400', bg: 'bg-green-400/10', border: 'border-green-400/20', label: 'Healthy' },
  degraded: { icon: AlertTriangle, color: 'text-yellow-400', bg: 'bg-yellow-400/10', border: 'border-yellow-400/20', label: 'Degraded' },
  down: { icon: XCircle, color: 'text-red-400', bg: 'bg-red-400/10', border: 'border-red-400/20', label: 'Down' },
  unknown: { icon: HelpCircle, color: 'text-gray-400', bg: 'bg-gray-400/10', border: 'border-gray-400/20', label: 'Unknown' },
}

export default function Infra() {
  const { t } = useTranslation('infra')
  const [modules, setModules] = useState<ModuleInfo[]>([])
  const [status, setStatus] = useState<StatusResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [toggling, setToggling] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selectedModule, setSelectedModule] = useState<string | null>(null)

  const fetchModules = useCallback(async (signal?: AbortSignal) => {
    const res = await fetch('/api/infra/modules', { credentials: 'include', signal })
    if (!res.ok) {
      throw new Error(`Failed to load modules (${res.status})`)
    }
    const data = await res.json()
    setModules(data.modules || [])
  }, [])

  const fetchStatus = useCallback(async (signal?: AbortSignal) => {
    const res = await fetch('/api/infra/status', { credentials: 'include', signal })
    if (!res.ok) {
      throw new Error(`Failed to load status (${res.status})`)
    }
    const data: StatusResponse = await res.json()
    setStatus(data)
  }, [])

  const loadAll = useCallback(async (background = false) => {
    if (background) {
      setRefreshing(true)
    } else {
      setLoading(true)
    }
    setError(null)
    try {
      await Promise.all([fetchModules(), fetchStatus()])
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToLoad'))
    } finally {
      if (background) {
        setRefreshing(false)
      } else {
        setLoading(false)
      }
    }
  }, [fetchModules, fetchStatus, t])

  useEffect(() => {
    const controller = new AbortController()
    const init = async () => {
      try {
        await Promise.all([fetchModules(controller.signal), fetchStatus(controller.signal)])
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : t('errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
        }
      }
    }
    void init()
    return () => controller.abort()
  }, [fetchModules, fetchStatus, t])

  const handleRefresh = async () => {
    await loadAll(true)
  }

  const handleToggle = async (moduleName: string, currentEnabled: boolean) => {
    setToggling(moduleName)
    try {
      const res = await fetch(`/api/infra/modules/${encodeURIComponent(moduleName)}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !currentEnabled }),
      })
      if (!res.ok) {
        throw new Error(`Failed to toggle module (${res.status})`)
      }
      await loadAll(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToToggle'))
    } finally {
      setToggling(null)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[50vh]">
        <RefreshCw size={24} className="animate-spin text-gray-400" />
      </div>
    )
  }

  const overallStatus = status?.overall || 'unknown'
  const overallCfg = statusConfig[overallStatus]
  const OverallIcon = overallCfg.icon

  // Build a map from module status results for quick lookup.
  const statusByName = new Map<string, ModuleResult>()
  if (status?.modules) {
    for (const m of status.modules) {
      statusByName.set(m.name, m)
    }
  }

  if (selectedModule) {
    const mod = modules.find(m => m.name === selectedModule)
    const modStatus = statusByName.get(selectedModule)
    if (!mod) {
      return (
        <div className="max-w-6xl mx-auto px-4 py-8">
          <button
            onClick={() => setSelectedModule(null)}
            className="flex items-center gap-1 text-gray-400 hover:text-white mb-6 transition-colors cursor-pointer"
          >
            <ChevronLeft size={16} />
            {t('backToOverview')}
          </button>
          <p className="text-sm text-gray-400">{t('moduleNotFound')}</p>
        </div>
      )
    }
    return (
      <div className="max-w-6xl mx-auto px-4 py-8">
        <button
          onClick={() => setSelectedModule(null)}
          className="flex items-center gap-1 text-gray-400 hover:text-white mb-6 transition-colors cursor-pointer"
        >
          <ChevronLeft size={16} />
          {t('backToOverview')}
        </button>
        <ModuleDetail
          module={mod}
          status={modStatus}
          onRefresh={handleRefresh}
          refreshing={refreshing}
        />
      </div>
    )
  }

  return (
    <div className="max-w-6xl mx-auto px-4 py-8">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-white">{t('title')}</h1>
          <p className="text-sm text-gray-400 mt-1">{t('subtitle')}</p>
        </div>
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="flex items-center gap-2 px-4 py-2 rounded-lg bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer disabled:opacity-50"
        >
          <RefreshCw size={16} className={refreshing ? 'animate-spin' : ''} />
          {t('actions.refresh', { ns: 'common' })}
        </button>
      </div>

      {/* Error banner */}
      {error && (
        <div className="flex items-center gap-3 px-4 py-3 rounded-lg border mb-4 bg-red-400/10 border-red-400/20">
          <XCircle size={18} className="text-red-400 shrink-0" />
          <span className="text-sm text-red-400">{error}</span>
          <button
            onClick={() => setError(null)}
            className="ml-auto text-red-400 hover:text-red-300 text-xs cursor-pointer"
            aria-label={t('dismiss')}
          >
            {t('dismiss')}
          </button>
        </div>
      )}

      {/* Overall status banner */}
      <div className={`flex items-center gap-3 px-4 py-3 rounded-lg border mb-8 ${overallCfg.bg} ${overallCfg.border}`}>
        <OverallIcon size={20} className={overallCfg.color} />
        <span className={`text-sm font-medium ${overallCfg.color}`}>
          {t('overall', { label: t(`status.${overallStatus}`) })}
        </span>
        {status?.modules && (
          <span className="text-xs text-gray-500 ml-auto">
            {t('modules', { count: status.modules.length })}
          </span>
        )}
      </div>

      {/* Module cards */}
      {modules.length === 0 ? (
        <div className="text-center py-16">
          <HelpCircle size={48} className="mx-auto text-gray-600 mb-4" />
          <h2 className="text-lg font-medium text-gray-400 mb-2">{t('noModules.heading')}</h2>
          <p className="text-sm text-gray-500">
            {t('noModules.description')}
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {modules.map(mod => {
            const modStatus = statusByName.get(mod.name)
            const cfg = modStatus ? statusConfig[modStatus.status] : statusConfig.unknown
            const StatusIcon = cfg.icon
            const isToggling = toggling === mod.name

            return (
              <div
                key={mod.name}
                className={`rounded-lg border p-4 transition-colors ${
                  mod.enabled
                    ? `${cfg.bg} ${cfg.border}`
                    : 'bg-gray-800/50 border-gray-700/50 opacity-60'
                }`}
              >
                <div className="flex items-start justify-between mb-2">
                  <button
                    onClick={() => mod.enabled && setSelectedModule(mod.name)}
                    className={`flex items-center gap-2 ${mod.enabled ? 'cursor-pointer hover:opacity-80' : 'cursor-default'}`}
                  >
                    <StatusIcon size={18} className={mod.enabled ? cfg.color : 'text-gray-500'} />
                    <h3 className="font-medium text-white">{mod.display_name}</h3>
                  </button>
                  <button
                    onClick={() => handleToggle(mod.name, mod.enabled)}
                    disabled={isToggling}
                    className="text-gray-400 hover:text-white transition-colors cursor-pointer disabled:opacity-50"
                    title={mod.enabled ? t('toggle.disable') : t('toggle.enable')}
                    aria-label={mod.enabled ? t('toggle.disableLabel', { name: mod.display_name }) : t('toggle.enableLabel', { name: mod.display_name })}
                  >
                    {mod.enabled ? (
                      <ToggleRight size={20} className="text-green-400" />
                    ) : (
                      <ToggleLeft size={20} />
                    )}
                  </button>
                </div>

                <p className="text-xs text-gray-400 mb-3">{mod.description}</p>

                {mod.enabled && modStatus && (
                  <div className="text-xs text-gray-500">
                    {modStatus.message && (
                      <p className="mb-1">{modStatus.message}</p>
                    )}
                    <p>
                      {t('lastChecked', { time: formatDateTime(modStatus.checked_at, { dateStyle: 'short', timeStyle: 'medium' }) })}
                    </p>
                  </div>
                )}

                {!mod.enabled && (
                  <p className="text-xs text-gray-600">{t('moduleDisabled')}</p>
                )}
              </div>
            )
          })}
        </div>
      )}

      {/* Tool Versions */}
      <ToolVersionsPanel />
    </div>
  )
}

// --- Tool Versions Panel ---

const TOOL_DISPLAY_ORDER = [
  'forge', 'bd', 'claude', 'go', 'node', 'npm', 'git', 'gh', 'dolt',
]

function toolDisplayName(key: string): string {
  const names: Record<string, string> = {
    claude: 'Claude',
    forge: 'Forge',
    bd: 'bd',
    go: 'Go',
    node: 'Node.js',
    npm: 'npm',
    gh: 'GitHub CLI',
    git: 'Git',
    dolt: 'Dolt',
  }
  return names[key] ?? key
}

function parseVersion(raw: string): string {
  // Strip common prefixes: "go version go1.22.0 linux/amd64" -> "1.22.0"
  // "git version 2.43.0" -> "2.43.0"
  // "gh version 2.40.0 (2024-01-01)" -> "2.40.0"
  // "v20.0.0" -> "20.0.0"
  // "forge 2.0.0" -> "2.0.0"
  const m = raw.match(/(\d+\.\d+[\w.-]*)/)
  return m ? m[1] : raw
}

const UPDATABLE_TOOLS = new Set(['forge', 'bd', 'claude', 'go', 'node', 'npm', 'git', 'gh', 'dolt'])

function ToolVersionsPanel() {
  const { t } = useTranslation('infra')
  const { user } = useAuth()
  const isAdmin = user?.is_admin ?? false
  const [versions, setVersions] = useState<Record<string, string> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Latest versions state
  const [latestVersions, setLatestVersions] = useState<Record<string, string>>({})
  const [latestLoading, setLatestLoading] = useState(true)

  // Update state
  const [confirmTool, setConfirmTool] = useState<string | null>(null)
  const [updatingTool, setUpdatingTool] = useState<string | null>(null)
  const [updateResult, setUpdateResult] = useState<{
    tool: string
    success: boolean
    stdout: string
    stderr: string
  } | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      try {
        const res = await fetch('/api/infra/versions', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = await res.json()
        setVersions(data)
      } catch (err) {
        if ((err as Error).name !== 'AbortError') {
          setError('LOAD_FAILED')
        }
      } finally {
        setLoading(false)
      }
    }
    load()
    return () => controller.abort()
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    async function loadLatest() {
      try {
        const res = await fetch('/api/infra/latest-versions', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data: Array<{ name: string; version: string }> = await res.json()
        const map: Record<string, string> = {}
        for (const entry of data) {
          map[entry.name] = entry.version
        }
        setLatestVersions(map)
      } catch (err) {
        if ((err as Error).name !== 'AbortError') {
          // Silently fail — latestVersions stays empty, column shows '—'
        }
      } finally {
        setLatestLoading(false)
      }
    }
    loadLatest()
    return () => controller.abort()
  }, [])

  const handleUpdate = async (tool: string) => {
    setConfirmTool(null)
    setUpdatingTool(tool)
    setUpdateResult(null)
    try {
      const res = await fetch(`/api/infra/update/${tool === 'bd' ? 'beads' : tool}`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
        setUpdateResult({
          tool,
          success: false,
          stdout: '',
          stderr: data.error || `HTTP ${res.status}`,
        })
        return
      }
      const data = await res.json()
      setUpdateResult({
        tool,
        success: data.success,
        stdout: data.stdout || '',
        stderr: data.stderr || '',
      })
    } catch (err) {
      setUpdateResult({
        tool,
        success: false,
        stdout: '',
        stderr: err instanceof Error ? err.message : 'Request failed',
      })
    } finally {
      setUpdatingTool(null)
    }
  }

  const forgeHead = versions?.forge_head

  // Build sorted tool entries (exclude forge_head — it's shown inline with forge)
  const tools = versions
    ? TOOL_DISPLAY_ORDER.filter(k => k in versions).map(k => ({
        key: k,
        name: toolDisplayName(k),
        version: versions[k],
        available: versions[k] !== 'unavailable',
      }))
    : []

  return (
    <div className="mt-8">
      <div className="flex items-center gap-2 mb-4">
        <Package size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('versions.title')}</h2>
      </div>

      <div className="rounded-lg border border-gray-700/50 bg-gray-800/50 overflow-hidden">
        {loading ? (
          <div className="flex items-center justify-center py-12" role="status" aria-label={t('versions.title')}>
            <Loader2 size={20} className="animate-spin text-gray-400" />
          </div>
        ) : error ? (
          <div className="flex items-center gap-3 px-4 py-3 bg-red-400/10">
            <XCircle size={16} className="text-red-400 shrink-0" />
            <span className="text-sm text-red-400">{t('versions.failedToLoad')}</span>
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-700/50">
                <th className="text-left px-4 py-2 text-gray-400 font-medium">{t('versions.colTool')}</th>
                <th className="text-left px-4 py-2 text-gray-400 font-medium">{t('versions.colVersion')}</th>
                <th className="text-left px-4 py-2 text-gray-400 font-medium">{t('versions.colLatest')}</th>
                {isAdmin && (
                  <th className="text-right px-4 py-2 text-gray-400 font-medium">{t('versions.colActions')}</th>
                )}
              </tr>
            </thead>
            <tbody>
              {tools.map(tool => (
                <tr
                  key={tool.key}
                  className={`border-b border-gray-700/30 last:border-b-0 ${
                    !tool.available ? 'opacity-50' : ''
                  }`}
                >
                  <td className="px-4 py-2 text-white">{tool.name}</td>
                  <td className="px-4 py-2">
                    {tool.available ? (
                      <span className="text-gray-300">
                        {parseVersion(tool.version)}
                      </span>
                    ) : (
                      <span className="text-gray-500 italic">{t('versions.unavailable')}</span>
                    )}
                    {tool.key === 'forge' && forgeHead && forgeHead !== 'unavailable' && (
                      <span className="ml-2 inline-flex items-center gap-1 text-xs text-gray-500">
                        <GitCommitHorizontal size={12} />
                        <span className="sr-only">{t('versions.commit')}</span>
                        {forgeHead}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2">
                    {latestLoading ? (
                      <Loader2 size={14} className="animate-spin text-gray-500" />
                    ) : (() => {
                      const latest = latestVersions[tool.key]
                      if (!latest || latest === 'unknown') {
                        return <span className="text-gray-500">—</span>
                      }
                      const installed = tool.available ? parseVersion(tool.version) : null
                      const isUpToDate = installed !== null && installed === parseVersion(latest)
                      return (
                        <span className={isUpToDate ? 'text-green-400' : 'text-amber-400'}>
                          {parseVersion(latest)}
                        </span>
                      )
                    })()}
                  </td>
                  {isAdmin && (
                    <td className="px-4 py-2 text-right">
                      {UPDATABLE_TOOLS.has(tool.key) && (
                        <button
                          onClick={() => setConfirmTool(tool.key)}
                          disabled={updatingTool !== null}
                          className="inline-flex items-center gap-1 px-2 py-1 text-xs font-medium text-gray-300 hover:text-white bg-gray-700 hover:bg-gray-600 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                          aria-label={t('versions.updateLabel', { tool: tool.name })}
                        >
                          {updatingTool === tool.key ? (
                            <Loader2 size={12} className="animate-spin" />
                          ) : (
                            <Download size={12} />
                          )}
                          {t('versions.update')}
                        </button>
                      )}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {updateResult && (
          <div className={`px-4 py-3 border-t border-gray-700/50 ${
            updateResult.success ? 'bg-green-400/10' : 'bg-red-400/10'
          }`}>
            <div className="flex items-center justify-between mb-1">
              <span className={`text-sm font-medium ${
                updateResult.success ? 'text-green-400' : 'text-red-400'
              }`}>
                {updateResult.success
                  ? t('versions.updateSuccess', { tool: toolDisplayName(updateResult.tool) })
                  : t('versions.updateFailed', { tool: toolDisplayName(updateResult.tool) })}
              </span>
              <button
                onClick={() => setUpdateResult(null)}
                className="text-gray-400 hover:text-white text-xs"
                aria-label={t('dismiss')}
              >
                {t('dismiss')}
              </button>
            </div>
            {(updateResult.stdout || updateResult.stderr) && (
              <pre className="text-xs text-gray-400 whitespace-pre-wrap max-h-40 overflow-y-auto mt-1">
                {updateResult.stdout}
                {updateResult.stderr && (
                  <span className="text-red-400">{updateResult.stderr}</span>
                )}
              </pre>
            )}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirmTool !== null}
        onClose={() => setConfirmTool(null)}
        onConfirm={() => confirmTool && handleUpdate(confirmTool)}
        title={t('versions.updateConfirmTitle', { tool: confirmTool ? toolDisplayName(confirmTool) : '' })}
        message={t('versions.updateConfirmMessage', { tool: confirmTool ? toolDisplayName(confirmTool) : '' })}
        confirmLabel={t('versions.update')}
        variant="default"
      />
    </div>
  )
}

// --- Module Detail Views ---

function ModuleDetail({ module, status, onRefresh, refreshing }: {
  module: ModuleInfo
  status?: ModuleResult
  onRefresh: () => Promise<void>
  refreshing: boolean
}) {
  const { t } = useTranslation('infra')
  const cfg = status ? statusConfig[status.status] : statusConfig.unknown
  const StatusIcon = cfg.icon

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <StatusIcon size={24} className={cfg.color} />
          <div>
            <h1 className="text-xl font-bold text-white">{module.display_name}</h1>
            <p className="text-sm text-gray-400">{module.description}</p>
          </div>
        </div>
        <button
          onClick={onRefresh}
          disabled={refreshing}
          className="flex items-center gap-2 px-4 py-2 rounded-lg bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer disabled:opacity-50"
        >
          <RefreshCw size={16} className={refreshing ? 'animate-spin' : ''} />
          {t('actions.refresh', { ns: 'common' })}
        </button>
      </div>

      {status?.message && (
        <div className={`flex items-center gap-3 px-4 py-3 rounded-lg border mb-6 ${cfg.bg} ${cfg.border}`}>
          <StatusIcon size={18} className={cfg.color} />
          <span className={`text-sm ${cfg.color}`}>{status.message}</span>
        </div>
      )}

      {module.name === 'health_checks' && <HealthChecksDetail details={status?.details} />}
      {module.name === 'ssl_certs' && <SSLCertsDetail details={status?.details} />}
      {module.name === 'uptime' && <UptimeDetail details={status?.details} />}
      {module.name === 'hetzner_vps' && <HetznerVPSDetail details={status?.details} />}
      {module.name === 'bandwidth' && <BandwidthDetail details={status?.details} />}
      {module.name === 'docker' && <DockerDetail details={status?.details} />}
      {module.name === 'github_actions' && <GitHubActionsDetail details={status?.details} />}
      {module.name === 'dns' && <DNSDetail details={status?.details} />}
      {module.name === 'db_stats' && <DBStatsDetail details={status?.details} />}
      {module.name === 'systemd' && <SystemdDetail details={status?.details} />}
    </div>
  )
}

// --- Health Checks Detail ---

function HealthChecksDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const [services, setServices] = useState<HealthService[]>([])
  const [newName, setNewName] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const serviceResults = (details?.services ?? []) as Array<{
    id: number; name: string; url: string; status: string
    status_code?: number; response_time_ms?: number; error?: string
  }>

  const loadServices = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/health-checks', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load services (${res.status})`)
      const data = await res.json()
      setServices(data.services || [])
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load services')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => { await loadServices(controller.signal) })()
    return () => controller.abort()
  }, [loadServices])

  const handleAdd = async () => {
    if (!newName.trim() || !newUrl.trim()) return
    setAdding(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/health-checks', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName.trim(), url: newUrl.trim() }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || `Failed (${res.status})`)
      }
      setNewName('')
      setNewUrl('')
      await loadServices()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add service')
    } finally {
      setAdding(false)
    }
  }

  const handleDelete = async (id: number) => {
    try {
      const res = await fetch(`/api/infra/health-checks/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete')
      await loadServices()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete service')
    }
  }

  // Build results map by service id for stable matching.
  const resultsById = new Map(serviceResults.map(r => [r.id, r]))

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Activity size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('healthChecks.title')}</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer" aria-label={t('dismiss')}>{t('dismiss')}</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder={t('healthChecks.namePlaceholder')}
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Service name"
        />
        <input
          type="text"
          placeholder={t('healthChecks.urlPlaceholder')}
          value={newUrl}
          onChange={e => setNewUrl(e.target.value)}
          className="flex-[2] px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Service URL"
        />
        <button
          onClick={handleAdd}
          disabled={adding || !newName.trim() || !newUrl.trim()}
          className="flex items-center gap-1 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
        >
          <Plus size={14} />
          {t('add')}
        </button>
      </div>

      {/* Service list */}
      {services.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">{t('healthChecks.noServices')}</p>
      ) : (
        <div className="space-y-2">
          {services.map(svc => {
            const result = resultsById.get(svc.id)
            const svcStatus = result?.status as 'ok' | 'degraded' | 'down' | 'unknown' | undefined
            const cfg = svcStatus ? statusConfig[svcStatus] : statusConfig.unknown
            const SvcIcon = cfg.icon

            return (
              <div
                key={svc.id}
                className={`flex items-center gap-3 px-4 py-3 rounded-lg border ${cfg.bg} ${cfg.border}`}
              >
                <SvcIcon size={16} className={cfg.color} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-white truncate">{svc.name}</p>
                  <p className="text-xs text-gray-400 truncate">{svc.url}</p>
                </div>
                {result && (
                  <div className="text-xs text-gray-400 text-right shrink-0">
                    {result.status_code && <span>HTTP {result.status_code}</span>}
                    {result.response_time_ms !== undefined && (
                      <span className="ml-2">{result.response_time_ms}ms</span>
                    )}
                    {result.error && (
                      <p className="text-red-400 truncate max-w-[12rem]" title={result.error}>{result.error}</p>
                    )}
                  </div>
                )}
                <button
                  onClick={() => handleDelete(svc.id)}
                  className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer shrink-0"
                  title={t('healthChecks.removeLabel', { name: svc.name })}
                  aria-label={t('healthChecks.removeLabel', { name: svc.name })}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// --- SSL Certs Detail ---

function SSLCertsDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const [hosts, setHosts] = useState<SSLHost[]>([])
  const [newName, setNewName] = useState('')
  const [newHostname, setNewHostname] = useState('')
  const [newPort, setNewPort] = useState('443')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const certResults = (details?.certificates ?? []) as Array<{
    id: number; name: string; hostname: string; port: number; status: string
    issuer?: string; expires_at?: string; days_remaining?: number; error?: string
  }>

  const loadHosts = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/ssl-certs', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load hosts (${res.status})`)
      const data = await res.json()
      setHosts(data.hosts || [])
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load hosts')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => { await loadHosts(controller.signal) })()
    return () => controller.abort()
  }, [loadHosts])

  const handleAdd = async () => {
    if (!newName.trim() || !newHostname.trim()) return
    setAdding(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/ssl-certs', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: newName.trim(),
          hostname: newHostname.trim(),
          port: parseInt(newPort) || 443,
        }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || `Failed (${res.status})`)
      }
      setNewName('')
      setNewHostname('')
      setNewPort('443')
      await loadHosts()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add host')
    } finally {
      setAdding(false)
    }
  }

  const handleDelete = async (id: number) => {
    try {
      const res = await fetch(`/api/infra/ssl-certs/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete')
      await loadHosts()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete host')
    }
  }

  const resultsById = new Map(certResults.map(r => [r.id, r]))

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Shield size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('sslCerts.title')}</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer" aria-label={t('dismiss')}>{t('dismiss')}</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder={t('sslCerts.namePlaceholder')}
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Host display name"
        />
        <input
          type="text"
          placeholder={t('sslCerts.hostnamePlaceholder')}
          value={newHostname}
          onChange={e => setNewHostname(e.target.value)}
          className="flex-[2] px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Hostname"
        />
        <input
          type="number"
          placeholder="Port"
          value={newPort}
          onChange={e => setNewPort(e.target.value)}
          className="w-20 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Port"
        />
        <button
          onClick={handleAdd}
          disabled={adding || !newName.trim() || !newHostname.trim()}
          className="flex items-center gap-1 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
        >
          <Plus size={14} />
          {t('add')}
        </button>
      </div>

      {/* Host list */}
      {hosts.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">{t('sslCerts.noHosts')}</p>
      ) : (
        <div className="space-y-2">
          {hosts.map(host => {
            const result = resultsById.get(host.id)
            const hostStatus = result?.status as 'ok' | 'degraded' | 'down' | 'unknown' | undefined
            const cfg = hostStatus ? statusConfig[hostStatus] : statusConfig.unknown
            const HostIcon = cfg.icon

            return (
              <div
                key={host.id}
                className={`flex items-center gap-3 px-4 py-3 rounded-lg border ${cfg.bg} ${cfg.border}`}
              >
                <HostIcon size={16} className={cfg.color} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-white truncate">{host.name}</p>
                  <p className="text-xs text-gray-400 truncate">{host.hostname}:{host.port}</p>
                </div>
                {result && (
                  <div className="text-xs text-gray-400 text-right shrink-0">
                    {result.days_remaining !== undefined && (
                      <span className={result.days_remaining <= 7 ? 'text-red-400' : result.days_remaining <= 30 ? 'text-yellow-400' : 'text-green-400'}>
                        {t('sslCerts.daysRemaining', { days: result.days_remaining })}
                      </span>
                    )}
                    {result.issuer && <p className="text-gray-500">{result.issuer}</p>}
                    {result.expires_at && (
                      <p>{formatDate(result.expires_at, { dateStyle: 'medium' })}</p>
                    )}
                    {result.error && (
                      <p className="text-red-400 truncate max-w-[12rem]" title={result.error}>{result.error}</p>
                    )}
                  </div>
                )}
                <button
                  onClick={() => handleDelete(host.id)}
                  className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer shrink-0"
                  title={t('sslCerts.removeLabel', { name: host.name })}
                  aria-label={t('sslCerts.removeLabel', { name: host.name })}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// --- Uptime Detail ---

function UptimeDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const stats = (details?.stats ?? null) as UptimeStats | null
  const recent = (details?.recent ?? []) as UptimeRecord[]

  const uptimeColor = (pct: number) => {
    if (pct >= 99) return 'text-green-400'
    if (pct >= 90) return 'text-yellow-400'
    return 'text-red-400'
  }

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Clock size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('uptime.title')}</h2>
      </div>

      {stats && stats.total_checks > 0 ? (
        <>
          {/* Stats cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">{t('uptime.last24h')}</p>
              <p className={`text-2xl font-bold ${uptimeColor(stats.uptime_24h)}`}>
                {formatNumber(stats.uptime_24h, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%
              </p>
            </div>
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">{t('uptime.last7d')}</p>
              <p className={`text-2xl font-bold ${uptimeColor(stats.uptime_7d)}`}>
                {formatNumber(stats.uptime_7d, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%
              </p>
            </div>
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">{t('uptime.last30d')}</p>
              <p className={`text-2xl font-bold ${uptimeColor(stats.uptime_30d)}`}>
                {formatNumber(stats.uptime_30d, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%
              </p>
            </div>
          </div>

          <p className="text-xs text-gray-500 mb-4">{t('uptime.checksRecorded', { count: stats.total_checks })}</p>

          {/* Recent checks table */}
          {recent.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-300 mb-2">{t('uptime.recentChecks')}</h3>
              <div className="rounded-lg border border-gray-700 overflow-hidden">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="bg-gray-800/80 text-gray-400 text-xs">
                      <th className="px-3 py-2 text-left">{t('uptime.colStatus')}</th>
                      <th className="px-3 py-2 text-left">{t('uptime.colModule')}</th>
                      <th className="px-3 py-2 text-left">{t('uptime.colTarget')}</th>
                      <th className="px-3 py-2 text-left">{t('uptime.colMessage')}</th>
                      <th className="px-3 py-2 text-right">{t('uptime.colTime')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {recent.map(rec => {
                      const recStatus = rec.status as 'ok' | 'degraded' | 'down' | 'unknown'
                      const cfg = statusConfig[recStatus] || statusConfig.unknown
                      const RecIcon = cfg.icon
                      return (
                        <tr key={rec.id} className="border-t border-gray-700/50">
                          <td className="px-3 py-2">
                            <RecIcon size={14} className={cfg.color} />
                          </td>
                          <td className="px-3 py-2 text-gray-300">{rec.module}</td>
                          <td className="px-3 py-2 text-gray-300">{rec.target}</td>
                          <td className="px-3 py-2 text-gray-500 truncate max-w-[8rem]">{rec.message || '-'}</td>
                          <td className="px-3 py-2 text-gray-500 text-right whitespace-nowrap">
                            {formatDateTime(rec.checked_at, {
                              dateStyle: 'short',
                              timeStyle: 'medium',
                            })}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      ) : (
        <p className="text-sm text-gray-500 text-center py-8">
          {t('uptime.noData')}
        </p>
      )}
    </div>
  )
}

// --- Hetzner VPS Detail ---

interface HetznerTokenState {
  configured: boolean
  masked: string
}

function HetznerVPSDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const { t: tCommon } = useTranslation('common')
  const [tokenState, setTokenState] = useState<HetznerTokenState | null>(null)
  const [tokenLoadError, setTokenLoadError] = useState(false)

  const servers = (details?.servers ?? []) as Array<{
    id: number; name: string; status: string; server_type: string
    datacenter: string; public_ipv4?: string; cpu_count: number
    memory_gb: number; disk_gb: number
  }>

  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      try {
        const res = await fetch('/api/infra/hetzner/token', { credentials: 'include', signal: controller.signal })
        if (!res.ok) {
          setTokenLoadError(true)
          return
        }
        setTokenState(await res.json())
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setTokenLoadError(true)
      }
    }
    load()
    return () => controller.abort()
  }, [])

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Server size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('hetzner.title')}</h2>
      </div>

      {/* API Token status */}
      <div className="mb-6 p-4 rounded-lg border border-gray-700 bg-gray-800/50">
        <h3 className="text-sm font-medium text-gray-300 mb-2">{t('hetzner.apiToken')}</h3>
        <div className="flex items-center gap-2">
          {tokenLoadError ? (
            <>
              <XCircle size={14} className="text-yellow-500" />
              <span className="text-sm text-yellow-400">{t('hetzner.unableToLoad')}</span>
            </>
          ) : tokenState === null ? (
            <span className="text-sm text-gray-500">{tCommon('status.loading')}</span>
          ) : tokenState.configured ? (
            <>
              <CheckCircle2 size={14} className="text-green-400" />
              <span className="text-sm text-green-400">{t('hetzner.configured')}</span>
              <span className="text-xs text-gray-500 font-mono ml-1">{tokenState.masked}</span>
            </>
          ) : (
            <>
              <XCircle size={14} className="text-gray-500" />
              <span className="text-sm text-gray-400">{t('hetzner.notConfigured')}</span>
            </>
          )}
          <span className="text-gray-600 mx-1">&middot;</span>
          <Link to="/settings" className="text-sm text-blue-400 hover:text-blue-300 underline">
            {t('hetzner.manageInSettings')}
          </Link>
        </div>
      </div>

      {/* Server list */}
      {servers.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">
          {tokenState?.configured
            ? t('hetzner.noServers')
            : <Trans t={t} i18nKey="hetzner.configureToken" components={{ link: <Link to="/settings" className="text-blue-400 hover:text-blue-300 underline" /> }} />}
        </p>
      ) : (
        <div className="space-y-2">
          {servers.map(srv => {
            const isRunning = srv.status === 'running'
            const cfg = isRunning ? statusConfig.ok : statusConfig.down
            const SrvIcon = cfg.icon

            return (
              <div
                key={srv.id}
                className={`flex items-center gap-3 px-4 py-3 rounded-lg border ${cfg.bg} ${cfg.border}`}
              >
                <SrvIcon size={16} className={cfg.color} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-white truncate">{srv.name}</p>
                  <p className="text-xs text-gray-400 truncate">
                    {srv.server_type} &middot; {srv.datacenter}
                    {srv.public_ipv4 && <span> &middot; {srv.public_ipv4}</span>}
                  </p>
                </div>
                <div className="text-xs text-gray-400 text-right shrink-0">
                  <p>{srv.cpu_count} vCPU &middot; {srv.memory_gb} GB RAM &middot; {srv.disk_gb} GB Disk</p>
                  <p className={isRunning ? 'text-green-400' : 'text-red-400'}>{srv.status}</p>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// --- Bandwidth / Transfer Usage Detail ---

function BandwidthDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const servers = (details?.servers ?? []) as Array<{
    id: number; name: string; included_traffic_tb: number
    ingoing_traffic_tb: number; outgoing_traffic_tb: number; usage_percent: number
  }>

  const usageColor = (pct: number) => {
    if (pct >= 95) return 'text-red-400'
    if (pct >= 80) return 'text-yellow-400'
    return 'text-green-400'
  }

  const usageBarColor = (pct: number) => {
    if (pct >= 95) return 'bg-red-400'
    if (pct >= 80) return 'bg-yellow-400'
    return 'bg-green-400'
  }

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <ArrowUpDown size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('bandwidth.title')}</h2>
      </div>

      <p className="text-xs text-gray-500 mb-4">
        {t('bandwidth.note')}
      </p>

      {servers.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">
          <Trans t={t} i18nKey="bandwidth.noData" components={{ link: <Link to="/settings" className="text-blue-400 hover:text-blue-300 underline" /> }} />
        </p>
      ) : (
        <div className="space-y-3">
          {servers.map(srv => (
            <div key={srv.id} className="rounded-lg border border-gray-700 bg-gray-800/50 p-4">
              <div className="flex items-center justify-between mb-2">
                <p className="text-sm font-medium text-white">{srv.name}</p>
                <span className={`text-sm font-bold ${usageColor(srv.usage_percent)}`}>
                  {formatNumber(srv.usage_percent, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%
                </span>
              </div>

              {/* Usage bar */}
              <div className="h-2 bg-gray-700 rounded-full mb-3 overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all ${usageBarColor(srv.usage_percent)}`}
                  style={{ width: `${Math.min(srv.usage_percent, 100)}%` }}
                />
              </div>

              <div className="grid grid-cols-3 gap-4 text-xs text-gray-400">
                <div>
                  <p className="text-gray-500">{t('bandwidth.included')}</p>
                  <p>{formatNumber(srv.included_traffic_tb, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} TB</p>
                </div>
                <div>
                  <p className="text-gray-500">{t('bandwidth.outgoing')}</p>
                  <p>{formatNumber(srv.outgoing_traffic_tb, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} TB</p>
                </div>
                <div>
                  <p className="text-gray-500">{t('bandwidth.ingoing')}</p>
                  <p>{formatNumber(srv.ingoing_traffic_tb, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} TB</p>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// --- Docker Containers Detail ---

interface DockerHostConfig {
  id: number
  name: string
  url: string
  created_at: string
}

function DockerDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const [hosts, setHosts] = useState<DockerHostConfig[]>([])
  const [newName, setNewName] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [adding, setAdding] = useState(false)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const hostResults = (details?.hosts ?? []) as Array<{
    host_id: number; host_name: string; status: string; error?: string
    containers: Array<{
      id: string; name: string; image: string; state: string; status: string
      host_id: number; host: string
    }>
  }>

  const loadHosts = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/docker-hosts', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load hosts (${res.status})`)
      const data = await res.json()
      setHosts(data.hosts || [])
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load Docker hosts')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/infra/docker-hosts', { credentials: 'include', signal: controller.signal })
        if (!res.ok) throw new Error(`Failed to load hosts (${res.status})`)
        const data = await res.json()
        setHosts(data.hosts || [])
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load Docker hosts')
      }
    })()
    return () => controller.abort()
  }, [])

  const handleAdd = async () => {
    if (!newName.trim() || !newUrl.trim()) return
    setAdding(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/docker-hosts', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName.trim(), url: newUrl.trim() }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || `Failed (${res.status})`)
      }
      setNewName('')
      setNewUrl('')
      await loadHosts()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add Docker host')
    } finally {
      setAdding(false)
    }
  }

  const handleDelete = async (id: number) => {
    setDeletingId(id)
    try {
      const res = await fetch(`/api/infra/docker-hosts/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete')
      await loadHosts()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete Docker host')
    } finally {
      setDeletingId(null)
    }
  }

  const resultsById = new Map(hostResults.map(r => [r.host_id, r]))

  const containerStateColor = (state: string) => {
    if (state === 'running') return 'text-green-400'
    if (state === 'exited' || state === 'dead') return 'text-red-400'
    return 'text-yellow-400'
  }

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Container size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('docker.title')}</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer" aria-label={t('dismiss')}>{t('dismiss')}</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder={t('docker.hostPlaceholder')}
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Docker host name"
        />
        <input
          type="text"
          placeholder={t('docker.urlPlaceholder')}
          value={newUrl}
          onChange={e => setNewUrl(e.target.value)}
          className="flex-[2] px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Docker API URL"
        />
        <button
          onClick={handleAdd}
          disabled={adding || !newName.trim() || !newUrl.trim()}
          className="flex items-center gap-1 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
        >
          <Plus size={14} />
          {t('add')}
        </button>
      </div>

      {/* Host list with containers */}
      {hosts.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">{t('docker.noHosts')}</p>
      ) : (
        <div className="space-y-4">
          {hosts.map(host => {
            const result = resultsById.get(host.id)
            const hostStatus = result?.status as 'ok' | 'down' | undefined
            const cfg = hostStatus === 'ok' ? statusConfig.ok : hostStatus === 'down' ? statusConfig.down : statusConfig.unknown
            const HostIcon = cfg.icon

            return (
              <div key={host.id} className="rounded-lg border border-gray-700 bg-gray-800/50 overflow-hidden">
                {/* Host header */}
                <div className={`flex items-center gap-3 px-4 py-3 border-b border-gray-700 ${cfg.bg}`}>
                  <HostIcon size={16} className={cfg.color} />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-white truncate">{host.name}</p>
                    <p className="text-xs text-gray-400 truncate">{host.url}</p>
                  </div>
                  {result?.error && (
                    <span className="text-xs text-red-400 truncate max-w-[12rem]" title={result.error}>
                      {result.error}
                    </span>
                  )}
                  <button
                    onClick={() => handleDelete(host.id)}
                    disabled={deletingId === host.id}
                    className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer shrink-0 disabled:opacity-50"
                    title={t('docker.removeLabel', { name: host.name })}
                    aria-label={t('docker.removeLabel', { name: host.name })}
                  >
                    <Trash2 size={14} className={deletingId === host.id ? 'animate-spin' : ''} />
                  </button>
                </div>

                {/* Container list */}
                {result?.containers && result.containers.length > 0 ? (
                  <div className="divide-y divide-gray-700/50">
                    {result.containers.map(c => (
                      <div key={c.id} className="flex items-center gap-3 px-4 py-2">
                        <span className={`text-xs font-medium ${containerStateColor(c.state)}`}>
                          {c.state}
                        </span>
                        <div className="flex-1 min-w-0">
                          <p className="text-sm text-white truncate">{c.name || c.id}</p>
                          <p className="text-xs text-gray-500 truncate">{c.image}</p>
                        </div>
                        <span className="text-xs text-gray-500 shrink-0">{c.status}</span>
                      </div>
                    ))}
                  </div>
                ) : result && !result.error ? (
                  <p className="text-xs text-gray-500 px-4 py-3">{t('docker.noContainers')}</p>
                ) : null}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// --- GitHub Actions Detail ---

interface GitHubRepoConfig {
  id: number
  owner: string
  repo: string
  created_at: string
}

function GitHubActionsDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const [tokenState, setTokenState] = useState<{ configured: boolean; masked: string } | null>(null)
  const [newToken, setNewToken] = useState('')
  const [showToken, setShowToken] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [repos, setRepos] = useState<GitHubRepoConfig[]>([])
  const [newOwner, setNewOwner] = useState('')
  const [newRepo, setNewRepo] = useState('')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const repoResults = (details?.repos ?? []) as Array<{
    owner: string; repo: string; status: string; error?: string
    runs: Array<{
      id: number; name: string; status: string; conclusion: string
      branch: string; event: string; created_at: string; html_url: string
    }>
  }>

  const loadToken = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/github/token', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load token status (${res.status})`)
      const data = await res.json()
      setTokenState(data)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load token status')
    }
  }, [])

  const loadRepos = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/github/repos', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load repos (${res.status})`)
      const data = await res.json()
      setRepos(data.repos || [])
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load repositories')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      await Promise.all([loadToken(controller.signal), loadRepos(controller.signal)])
    })()
    return () => controller.abort()
  }, [loadToken, loadRepos])

  const handleSaveToken = async () => {
    if (!newToken.trim()) return
    setSaving(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/github/token', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: newToken.trim() }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || `Failed (${res.status})`)
      }
      setNewToken('')
      setShowToken(false)
      await loadToken()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save token')
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteToken = async () => {
    setDeleting(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/github/token', {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete token')
      await loadToken()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete token')
    } finally {
      setDeleting(false)
    }
  }

  const handleAddRepo = async () => {
    if (!newOwner.trim() || !newRepo.trim()) return
    setAdding(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/github/repos', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ owner: newOwner.trim(), repo: newRepo.trim() }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || `Failed (${res.status})`)
      }
      setNewOwner('')
      setNewRepo('')
      await loadRepos()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add repository')
    } finally {
      setAdding(false)
    }
  }

  const handleDeleteRepo = async (id: number) => {
    try {
      const res = await fetch(`/api/infra/github/repos/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete')
      await loadRepos()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete repository')
    }
  }

  const resultsMap = new Map(repoResults.map(r => [`${r.owner}/${r.repo}`, r]))

  const conclusionColor = (conclusion: string) => {
    if (conclusion === 'success') return 'text-green-400'
    if (conclusion === 'failure') return 'text-red-400'
    if (conclusion === 'cancelled') return 'text-gray-400'
    return 'text-yellow-400'
  }

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <GitBranch size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('github.title')}</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer" aria-label={t('dismiss')}>{t('dismiss')}</button>
        </div>
      )}

      {/* API Token configuration */}
      <div className="mb-6 p-4 rounded-lg border border-gray-700 bg-gray-800/50">
        <h3 className="text-sm font-medium text-gray-300 mb-2">{t('github.tokenTitle')}</h3>
        <p className="text-xs text-gray-500 mb-2">
          <Trans t={t} i18nKey="github.tokenScope" components={{ code: <code className="font-mono bg-gray-700 px-1 rounded text-gray-300" /> }} />
        </p>
        {tokenState?.configured ? (
          <div className="flex items-center gap-3">
            <span className="text-xs text-gray-400 font-mono">{tokenState.masked}</span>
            <button
              onClick={handleDeleteToken}
              disabled={deleting}
              className="text-xs text-red-400 hover:text-red-300 underline cursor-pointer disabled:opacity-50"
              aria-label={t('github.removeTokenLabel')}
            >
              {deleting ? t('removing') : t('remove')}
            </button>
          </div>
        ) : (
          <div className="flex gap-2">
            <div className="relative flex-1">
              <input
                type={showToken ? 'text' : 'password'}
                placeholder="ghp_..."
                value={newToken}
                onChange={e => setNewToken(e.target.value)}
                className="w-full px-3 py-2 pr-10 rounded-lg bg-gray-900 border border-gray-600 text-white text-sm focus:outline-none focus:border-blue-500"
                aria-label="GitHub token"
              />
              <button
                type="button"
                onClick={() => setShowToken(!showToken)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 cursor-pointer"
                aria-label={showToken ? 'Hide token' : 'Show token'}
              >
                {showToken ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
            </div>
            <button
              onClick={handleSaveToken}
              disabled={saving || !newToken.trim()}
              className="px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
            >
              {t('save')}
            </button>
          </div>
        )}
      </div>

      {/* Add repo form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder={t('github.ownerPlaceholder')}
          value={newOwner}
          onChange={e => setNewOwner(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Repository owner"
        />
        <input
          type="text"
          placeholder={t('github.repoPlaceholder')}
          value={newRepo}
          onChange={e => setNewRepo(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Repository name"
        />
        <button
          onClick={handleAddRepo}
          disabled={adding || !newOwner.trim() || !newRepo.trim()}
          className="flex items-center gap-1 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
        >
          <Plus size={14} />
          {t('add')}
        </button>
      </div>

      {/* Repository list with workflow runs */}
      {repos.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">
          {tokenState?.configured
            ? t('github.noReposWithToken')
            : t('github.noReposWithoutToken')}
        </p>
      ) : (
        <div className="space-y-4">
          {repos.map(repo => {
            const result = resultsMap.get(`${repo.owner}/${repo.repo}`)
            const repoStatus = result?.status as 'ok' | 'degraded' | 'down' | undefined
            const cfg = repoStatus ? statusConfig[repoStatus] || statusConfig.unknown : statusConfig.unknown
            const RepoIcon = cfg.icon

            return (
              <div key={repo.id} className="rounded-lg border border-gray-700 bg-gray-800/50 overflow-hidden">
                <div className={`flex items-center gap-3 px-4 py-3 border-b border-gray-700 ${cfg.bg}`}>
                  <RepoIcon size={16} className={cfg.color} />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-white truncate">{repo.owner}/{repo.repo}</p>
                  </div>
                  {result?.error && (
                    <span className="text-xs text-red-400 truncate max-w-[12rem]" title={result.error}>
                      {result.error}
                    </span>
                  )}
                  <button
                    onClick={() => handleDeleteRepo(repo.id)}
                    className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer shrink-0"
                    title={t('github.removeLabel', { owner: repo.owner, repo: repo.repo })}
                    aria-label={t('github.removeLabel', { owner: repo.owner, repo: repo.repo })}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>

                {result?.runs && result.runs.length > 0 ? (
                  <div className="divide-y divide-gray-700/50">
                    {result.runs.map(run => (
                      <div key={run.id} className="flex items-center gap-3 px-4 py-2">
                        <span className={`text-xs font-medium ${conclusionColor(run.conclusion)}`}>
                          {run.conclusion || run.status}
                        </span>
                        <div className="flex-1 min-w-0">
                          <p className="text-sm text-white truncate">{run.name}</p>
                          <p className="text-xs text-gray-500 truncate">
                            {run.branch} &middot; {run.event}
                          </p>
                        </div>
                        <span className="text-xs text-gray-500 shrink-0">
                          {formatDateTime(run.created_at, {
                            dateStyle: 'short',
                            timeStyle: 'short',
                          })}
                        </span>
                      </div>
                    ))}
                  </div>
                ) : result && !result.error ? (
                  <p className="text-xs text-gray-500 px-4 py-3">{t('github.noWorkflowRuns')}</p>
                ) : null}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// --- DNS Monitoring Detail ---

interface DNSMonitorConfig {
  id: number
  name: string
  hostname: string
  record_type: string
  created_at: string
}

function DNSDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const [monitors, setMonitors] = useState<DNSMonitorConfig[]>([])
  const [newName, setNewName] = useState('')
  const [newHostname, setNewHostname] = useState('')
  const [newRecordType, setNewRecordType] = useState('A')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const monitorResults = (details?.monitors ?? []) as Array<{
    id: number; name: string; hostname: string; record_type: string; status: string
    resolved_values?: string[]; response_time_ms: number; error?: string
  }>

  const loadMonitors = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/dns-monitors', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load monitors (${res.status})`)
      const data = await res.json()
      setMonitors(data.monitors || [])
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load DNS monitors')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => { await loadMonitors(controller.signal) })()
    return () => controller.abort()
  }, [loadMonitors])

  const handleAdd = async () => {
    if (!newName.trim() || !newHostname.trim()) return
    setAdding(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/dns-monitors', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: newName.trim(),
          hostname: newHostname.trim(),
          record_type: newRecordType,
        }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || `Failed (${res.status})`)
      }
      setNewName('')
      setNewHostname('')
      setNewRecordType('A')
      await loadMonitors()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add DNS monitor')
    } finally {
      setAdding(false)
    }
  }

  const handleDelete = async (id: number) => {
    try {
      const res = await fetch(`/api/infra/dns-monitors/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete')
      await loadMonitors()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete DNS monitor')
    }
  }

  const resultsById = new Map(monitorResults.map(r => [r.id, r]))

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Globe size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('dns.title')}</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer" aria-label={t('dismiss')}>{t('dismiss')}</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder={t('dns.namePlaceholder')}
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Monitor display name"
        />
        <input
          type="text"
          placeholder={t('dns.hostnamePlaceholder')}
          value={newHostname}
          onChange={e => setNewHostname(e.target.value)}
          className="flex-[2] px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Hostname"
        />
        <select
          value={newRecordType}
          onChange={e => setNewRecordType(e.target.value)}
          className="w-24 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Record type"
        >
          <option value="A">A</option>
          <option value="AAAA">AAAA</option>
          <option value="CNAME">CNAME</option>
          <option value="MX">MX</option>
          <option value="TXT">TXT</option>
          <option value="NS">NS</option>
        </select>
        <button
          onClick={handleAdd}
          disabled={adding || !newName.trim() || !newHostname.trim()}
          className="flex items-center gap-1 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
        >
          <Plus size={14} />
          {t('add')}
        </button>
      </div>

      {/* Monitor list */}
      {monitors.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">{t('dns.noMonitors')}</p>
      ) : (
        <div className="space-y-2">
          {monitors.map(mon => {
            const result = resultsById.get(mon.id)
            const monStatus = result?.status as 'ok' | 'degraded' | 'down' | 'unknown' | undefined
            const cfg = monStatus ? statusConfig[monStatus] : statusConfig.unknown
            const MonIcon = cfg.icon

            return (
              <div
                key={mon.id}
                className={`flex items-center gap-3 px-4 py-3 rounded-lg border ${cfg.bg} ${cfg.border}`}
              >
                <MonIcon size={16} className={cfg.color} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-white truncate">{mon.name}</p>
                  <p className="text-xs text-gray-400 truncate">
                    {mon.hostname} &middot; {mon.record_type}
                  </p>
                </div>
                {result && (
                  <div className="text-xs text-gray-400 text-right shrink-0">
                    {result.resolved_values && result.resolved_values.length > 0 && (
                      <p className="text-green-400 truncate max-w-[16rem]" title={result.resolved_values.join(', ')}>
                        {result.resolved_values.slice(0, 3).join(', ')}
                        {result.resolved_values.length > 3 && ` +${result.resolved_values.length - 3}`}
                      </p>
                    )}
                    {result.response_time_ms !== undefined && (
                      <span>{result.response_time_ms}ms</span>
                    )}
                    {result.error && (
                      <p className="text-red-400 truncate max-w-[12rem]" title={result.error}>{result.error}</p>
                    )}
                  </div>
                )}
                <button
                  onClick={() => handleDelete(mon.id)}
                  className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer shrink-0"
                  title={t('dns.removeLabel', { name: mon.name })}
                  aria-label={t('dns.removeLabel', { name: mon.name })}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// --- Database Stats Detail ---

function DBStatsDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const overview = details?.overview as {
    page_count: number
    page_size: number
    size_bytes: number
    size_mb: number
    tables: Array<{ name: string; row_count: number }>
  } | undefined

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Database size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('dbStats.title')}</h2>
      </div>

      {overview ? (
        <>
          {/* Size stats */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">{t('dbStats.dbSize')}</p>
              <p className="text-2xl font-bold text-blue-400">{formatNumber(overview.size_mb, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} MB</p>
            </div>
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">{t('dbStats.pages')}</p>
              <p className="text-2xl font-bold text-gray-300">{formatNumber(overview.page_count)}</p>
            </div>
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">{t('dbStats.pageSize')}</p>
              <p className="text-2xl font-bold text-gray-300">{formatNumber(overview.page_size / 1024, { maximumFractionDigits: 0 })} KB</p>
            </div>
          </div>

          {/* Table row counts */}
          <h3 className="text-sm font-medium text-gray-300 mb-2">{t('dbStats.tableRowCounts')}</h3>
          <div className="rounded-lg border border-gray-700 overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-gray-800/80 text-gray-400 text-xs">
                  <th className="px-3 py-2 text-left">{t('dbStats.colTable')}</th>
                  <th className="px-3 py-2 text-right">{t('dbStats.colRows')}</th>
                </tr>
              </thead>
              <tbody>
                {overview.tables.map(tbl => (
                  <tr key={tbl.name} className="border-t border-gray-700/50">
                    <td className="px-3 py-2 text-gray-300 font-mono text-xs">{tbl.name}</td>
                    <td className="px-3 py-2 text-gray-400 text-right">{formatNumber(tbl.row_count)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : (
        <p className="text-sm text-gray-500 text-center py-8">{t('dbStats.noData')}</p>
      )}
    </div>
  )
}

// --- System Services (systemd) Detail ---

interface SystemdServiceConfig {
  id: number
  name: string
  unit: string
  created_at: string
}

function SystemdDetail({ details }: { details?: Record<string, unknown> }) {
  const { t } = useTranslation('infra')
  const [services, setServices] = useState<SystemdServiceConfig[]>([])
  const [newName, setNewName] = useState('')
  const [newUnit, setNewUnit] = useState('')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const serviceResults = (details?.services ?? []) as Array<{
    id: number; name: string; unit: string; active_state: string
    sub_state?: string; status: string; error?: string
  }>

  const loadServices = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/systemd-services', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load services (${res.status})`)
      const data = await res.json()
      setServices(data.services || [])
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load systemd services')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => { await loadServices(controller.signal) })()
    return () => controller.abort()
  }, [loadServices])

  const handleAdd = async () => {
    if (!newName.trim() || !newUnit.trim()) return
    setAdding(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/systemd-services', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: newName.trim(),
          unit: newUnit.trim(),
        }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || `Failed (${res.status})`)
      }
      setNewName('')
      setNewUnit('')
      await loadServices()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add systemd service')
    } finally {
      setAdding(false)
    }
  }

  const handleDelete = async (id: number) => {
    try {
      const res = await fetch(`/api/infra/systemd-services/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete')
      await loadServices()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete systemd service')
    }
  }

  const resultsById = new Map(serviceResults.map(r => [r.id, r]))

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Cog size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">{t('systemd.title')}</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer" aria-label={t('dismiss')}>{t('dismiss')}</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder={t('systemd.namePlaceholder')}
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label={t('systemd.namePlaceholder')}
        />
        <input
          type="text"
          placeholder={t('systemd.unitPlaceholder')}
          value={newUnit}
          onChange={e => setNewUnit(e.target.value)}
          className="flex-[2] px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label={t('systemd.unitPlaceholder')}
        />
        <button
          onClick={handleAdd}
          disabled={adding || !newName.trim() || !newUnit.trim()}
          className="flex items-center gap-1 px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
        >
          <Plus size={14} />
          {t('add')}
        </button>
      </div>

      {/* Service list */}
      {services.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">{t('systemd.noServices')}</p>
      ) : (
        <div className="space-y-2">
          {services.map(svc => {
            const result = resultsById.get(svc.id)
            const svcStatus = result?.status as 'ok' | 'degraded' | 'down' | 'unknown' | undefined
            const cfg = svcStatus ? statusConfig[svcStatus] : statusConfig.unknown
            const SvcIcon = cfg.icon

            return (
              <div
                key={svc.id}
                className={`flex items-center gap-3 px-4 py-3 rounded-lg border ${cfg.bg} ${cfg.border}`}
              >
                <SvcIcon size={16} className={cfg.color} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-white truncate">{svc.name}</p>
                  <p className="text-xs text-gray-400 truncate">{svc.unit}</p>
                </div>
                {result && (
                  <div className="text-xs text-gray-400 text-right shrink-0">
                    {result.active_state && (
                      <p className={result.status === 'ok' ? 'text-green-400' : result.status === 'degraded' ? 'text-yellow-400' : 'text-red-400'}>
                        {result.active_state}{result.sub_state ? ` (${result.sub_state})` : ''}
                      </p>
                    )}
                    {result.error && (
                      <p className="text-red-400 truncate max-w-[12rem]" title={result.error}>{result.error}</p>
                    )}
                  </div>
                )}
                <button
                  onClick={() => handleDelete(svc.id)}
                  className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer shrink-0"
                  title={t('systemd.removeLabel', { name: svc.name })}
                  aria-label={t('systemd.removeLabel', { name: svc.name })}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
