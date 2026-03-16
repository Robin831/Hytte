import { useState, useEffect, useCallback } from 'react'

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
  const [modules, setModules] = useState<ModuleInfo[]>([])
  const [status, setStatus] = useState<StatusResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [toggling, setToggling] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selectedModule, setSelectedModule] = useState<string | null>(null)

  const fetchModules = useCallback(async () => {
    const res = await fetch('/api/infra/modules', { credentials: 'include' })
    if (!res.ok) {
      throw new Error(`Failed to load modules (${res.status})`)
    }
    const data = await res.json()
    setModules(data.modules || [])
  }, [])

  const fetchStatus = useCallback(async () => {
    const res = await fetch('/api/infra/status', { credentials: 'include' })
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
      setError(err instanceof Error ? err.message : 'Failed to load infrastructure data')
    } finally {
      if (background) {
        setRefreshing(false)
      } else {
        setLoading(false)
      }
    }
  }, [fetchModules, fetchStatus])

  useEffect(() => {
    let cancelled = false
    const init = async () => {
      try {
        await Promise.all([fetchModules(), fetchStatus()])
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load infrastructure data')
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }
    void init()
    return () => { cancelled = true }
  }, [fetchModules, fetchStatus])

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
      setError(err instanceof Error ? err.message : 'Failed to toggle module')
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
            Back to overview
          </button>
          <p className="text-sm text-gray-400">Module not found.</p>
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
          Back to overview
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
          <h1 className="text-2xl font-bold text-white">Infrastructure</h1>
          <p className="text-sm text-gray-400 mt-1">Monitor your services and infrastructure</p>
        </div>
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="flex items-center gap-2 px-4 py-2 rounded-lg bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer disabled:opacity-50"
        >
          <RefreshCw size={16} className={refreshing ? 'animate-spin' : ''} />
          Refresh
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
          >
            Dismiss
          </button>
        </div>
      )}

      {/* Overall status banner */}
      <div className={`flex items-center gap-3 px-4 py-3 rounded-lg border mb-8 ${overallCfg.bg} ${overallCfg.border}`}>
        <OverallIcon size={20} className={overallCfg.color} />
        <span className={`text-sm font-medium ${overallCfg.color}`}>
          Overall: {overallCfg.label}
        </span>
        {status?.modules && (
          <span className="text-xs text-gray-500 ml-auto">
            {status.modules.length} module{status.modules.length !== 1 ? 's' : ''} active
          </span>
        )}
      </div>

      {/* Module cards */}
      {modules.length === 0 ? (
        <div className="text-center py-16">
          <HelpCircle size={48} className="mx-auto text-gray-600 mb-4" />
          <h2 className="text-lg font-medium text-gray-400 mb-2">No modules configured</h2>
          <p className="text-sm text-gray-500">
            Infrastructure modules will appear here once configured.
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
                    title={mod.enabled ? 'Disable module' : 'Enable module'}
                    aria-label={mod.enabled ? `Disable ${mod.display_name}` : `Enable ${mod.display_name}`}
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
                      Last checked:{' '}
                      {new Date(modStatus.checked_at).toLocaleString(undefined, {
                        dateStyle: 'short',
                        timeStyle: 'medium',
                      })}
                    </p>
                  </div>
                )}

                {!mod.enabled && (
                  <p className="text-xs text-gray-600">Module disabled</p>
                )}
              </div>
            )
          })}
        </div>
      )}
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
          Refresh
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
    </div>
  )
}

// --- Health Checks Detail ---

function HealthChecksDetail({ details }: { details?: Record<string, unknown> }) {
  const [services, setServices] = useState<HealthService[]>([])
  const [newName, setNewName] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const serviceResults = (details?.services ?? []) as Array<{
    id: number; name: string; url: string; status: string
    status_code?: number; response_time_ms?: number; error?: string
  }>

  const loadServices = useCallback(async () => {
    try {
      const res = await fetch('/api/infra/health-checks', { credentials: 'include' })
      if (!res.ok) throw new Error(`Failed to load services (${res.status})`)
      const data = await res.json()
      setServices(data.services || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load services')
    }
  }, [])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadServices()
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
        <h2 className="text-lg font-semibold text-white">Monitored Services</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer">dismiss</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder="Service name"
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Service name"
        />
        <input
          type="text"
          placeholder="URL (e.g. https://api.example.com/health)"
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
          Add
        </button>
      </div>

      {/* Service list */}
      {services.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">No services configured yet. Add one above to start monitoring.</p>
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
                  title="Remove service"
                  aria-label={`Remove ${svc.name}`}
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

  const loadHosts = useCallback(async () => {
    try {
      const res = await fetch('/api/infra/ssl-certs', { credentials: 'include' })
      if (!res.ok) throw new Error(`Failed to load hosts (${res.status})`)
      const data = await res.json()
      setHosts(data.hosts || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load hosts')
    }
  }, [])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadHosts()
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
        <h2 className="text-lg font-semibold text-white">SSL Certificate Hosts</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer">dismiss</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder="Display name"
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Host display name"
        />
        <input
          type="text"
          placeholder="Hostname (e.g. example.com)"
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
          Add
        </button>
      </div>

      {/* Host list */}
      {hosts.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">No hosts configured yet. Add one above to start monitoring certificates.</p>
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
                        {result.days_remaining}d remaining
                      </span>
                    )}
                    {result.issuer && <p className="text-gray-500">{result.issuer}</p>}
                    {result.expires_at && (
                      <p>{new Date(result.expires_at).toLocaleDateString(undefined, { dateStyle: 'medium' })}</p>
                    )}
                    {result.error && (
                      <p className="text-red-400 truncate max-w-[12rem]" title={result.error}>{result.error}</p>
                    )}
                  </div>
                )}
                <button
                  onClick={() => handleDelete(host.id)}
                  className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer shrink-0"
                  title="Remove host"
                  aria-label={`Remove ${host.name}`}
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
        <h2 className="text-lg font-semibold text-white">Uptime Statistics</h2>
      </div>

      {stats && stats.total_checks > 0 ? (
        <>
          {/* Stats cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">Last 24 hours</p>
              <p className={`text-2xl font-bold ${uptimeColor(stats.uptime_24h)}`}>
                {stats.uptime_24h.toFixed(1)}%
              </p>
            </div>
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">Last 7 days</p>
              <p className={`text-2xl font-bold ${uptimeColor(stats.uptime_7d)}`}>
                {stats.uptime_7d.toFixed(1)}%
              </p>
            </div>
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-4 text-center">
              <p className="text-xs text-gray-400 mb-1">Last 30 days</p>
              <p className={`text-2xl font-bold ${uptimeColor(stats.uptime_30d)}`}>
                {stats.uptime_30d.toFixed(1)}%
              </p>
            </div>
          </div>

          <p className="text-xs text-gray-500 mb-4">{stats.total_checks} checks recorded (last 30 days)</p>

          {/* Recent checks table */}
          {recent.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-300 mb-2">Recent Checks</h3>
              <div className="rounded-lg border border-gray-700 overflow-hidden">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="bg-gray-800/80 text-gray-400 text-xs">
                      <th className="px-3 py-2 text-left">Status</th>
                      <th className="px-3 py-2 text-left">Module</th>
                      <th className="px-3 py-2 text-left">Target</th>
                      <th className="px-3 py-2 text-left">Message</th>
                      <th className="px-3 py-2 text-right">Time</th>
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
                            {new Date(rec.checked_at).toLocaleString(undefined, {
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
          No uptime data recorded yet. Check results are recorded when health checks or SSL checks run.
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
  const [tokenState, setTokenState] = useState<HetznerTokenState | null>(null)
  const [newToken, setNewToken] = useState('')
  const [showToken, setShowToken] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const servers = (details?.servers ?? []) as Array<{
    id: number; name: string; status: string; server_type: string
    datacenter: string; public_ipv4?: string; cpu_count: number
    memory_gb: number; disk_gb: number
  }>

  const loadToken = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/hetzner/token', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load token status (${res.status})`)
      const data = await res.json()
      setTokenState(data)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : 'Failed to load token status')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void loadToken(controller.signal)
    return () => controller.abort()
  }, [loadToken])

  const handleSaveToken = async () => {
    if (!newToken.trim()) return
    setSaving(true)
    setError(null)
    try {
      const res = await fetch('/api/infra/hetzner/token', {
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
      const res = await fetch('/api/infra/hetzner/token', {
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

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Server size={18} className="text-gray-400" />
        <h2 className="text-lg font-semibold text-white">Hetzner Cloud Servers</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer">dismiss</button>
        </div>
      )}

      {/* API Token configuration */}
      <div className="mb-6 p-4 rounded-lg border border-gray-700 bg-gray-800/50">
        <h3 className="text-sm font-medium text-gray-300 mb-2">API Token</h3>
        {tokenState?.configured ? (
          <div className="flex items-center gap-3">
            <span className="text-xs text-gray-400 font-mono">{tokenState.masked}</span>
            <button
              onClick={handleDeleteToken}
              disabled={deleting}
              className="text-xs text-red-400 hover:text-red-300 underline cursor-pointer disabled:opacity-50"
            >
              {deleting ? 'Removing...' : 'Remove'}
            </button>
          </div>
        ) : (
          <div className="flex gap-2">
            <div className="relative flex-1">
              <input
                type={showToken ? 'text' : 'password'}
                placeholder="Hetzner Cloud API token"
                value={newToken}
                onChange={e => setNewToken(e.target.value)}
                className="w-full px-3 py-2 pr-10 rounded-lg bg-gray-900 border border-gray-600 text-white text-sm focus:outline-none focus:border-blue-500"
                aria-label="Hetzner API token"
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
              Save
            </button>
          </div>
        )}
      </div>

      {/* Server list */}
      {servers.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">
          {tokenState?.configured
            ? 'No servers found in your Hetzner Cloud account.'
            : 'Configure your Hetzner API token above to see your servers.'}
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
        <h2 className="text-lg font-semibold text-white">Bandwidth / Transfer Usage</h2>
      </div>

      <p className="text-xs text-gray-500 mb-4">
        Uses the same Hetzner API token as the VPS Stats module. Hetzner only bills outgoing traffic.
      </p>

      {servers.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">
          No server traffic data available. Make sure your Hetzner API token is configured in the VPS Stats module.
        </p>
      ) : (
        <div className="space-y-3">
          {servers.map(srv => (
            <div key={srv.id} className="rounded-lg border border-gray-700 bg-gray-800/50 p-4">
              <div className="flex items-center justify-between mb-2">
                <p className="text-sm font-medium text-white">{srv.name}</p>
                <span className={`text-sm font-bold ${usageColor(srv.usage_percent)}`}>
                  {srv.usage_percent.toFixed(1)}%
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
                  <p className="text-gray-500">Included</p>
                  <p>{srv.included_traffic_tb.toFixed(2)} TB</p>
                </div>
                <div>
                  <p className="text-gray-500">Outgoing</p>
                  <p>{srv.outgoing_traffic_tb.toFixed(2)} TB</p>
                </div>
                <div>
                  <p className="text-gray-500">Ingoing</p>
                  <p>{srv.ingoing_traffic_tb.toFixed(2)} TB</p>
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
    void loadHosts(controller.signal)
    return () => controller.abort()
  }, [loadHosts])

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
        <h2 className="text-lg font-semibold text-white">Docker Hosts</h2>
      </div>

      {error && (
        <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
          {error}
          <button onClick={() => setError(null)} className="ml-2 underline cursor-pointer">dismiss</button>
        </div>
      )}

      {/* Add form */}
      <div className="flex gap-2 mb-4">
        <input
          type="text"
          placeholder="Host name"
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="flex-1 px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 text-white text-sm focus:outline-none focus:border-blue-500"
          aria-label="Docker host name"
        />
        <input
          type="text"
          placeholder="Docker API URL (e.g. https://docker.example.com:2376)"
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
          Add
        </button>
      </div>

      {/* Host list with containers */}
      {hosts.length === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">No Docker hosts configured yet. Add one above to start monitoring containers.</p>
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
                    title="Remove host"
                    aria-label={`Remove ${host.name}`}
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
                  <p className="text-xs text-gray-500 px-4 py-3">No containers</p>
                ) : null}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
