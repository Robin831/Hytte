import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { CheckCircle2, AlertTriangle, XCircle, HelpCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'

interface SystemdServiceResult {
  name: string
  unit: string
  active_state: string
  sub_state?: string
  status: string
  error?: string
}

interface ModuleResult {
  name: string
  status: 'ok' | 'degraded' | 'down' | 'unknown'
  message?: string
  details?: {
    services?: SystemdServiceResult[]
  }
}

interface StatusResponse {
  overall: 'ok' | 'degraded' | 'down' | 'unknown'
  modules: ModuleResult[]
}

function formatModuleName(name: string): string {
  return name
    .replace(/_/g, ' ')
    .replace(/\b\w/g, c => c.toUpperCase())
}

function StatusIcon({ status, size = 14 }: { status: string; size?: number }) {
  switch (status) {
    case 'ok':
      return <CheckCircle2 size={size} className="text-green-400 shrink-0" />
    case 'degraded':
      return <AlertTriangle size={size} className="text-yellow-400 shrink-0" />
    case 'down':
      return <XCircle size={size} className="text-red-400 shrink-0 animate-pulse" />
    default:
      return <HelpCircle size={size} className="text-gray-500 shrink-0" />
  }
}

const overallBannerClass: Record<string, string> = {
  ok: 'bg-green-500/10 border border-green-500/30 text-green-400',
  degraded: 'bg-yellow-500/10 border border-yellow-500/30 text-yellow-400',
  down: 'bg-red-500/15 border border-red-500/40 text-red-400 font-bold',
  unknown: 'bg-gray-700/50 border border-gray-600 text-gray-400',
}

type InfraStatusLabelKey = 'widgets.infra.statusLabels.ok' | 'widgets.infra.statusLabels.degraded' | 'widgets.infra.statusLabels.down' | 'widgets.infra.statusLabels.unknown'

const statusLabelKey: Record<'ok' | 'degraded' | 'down' | 'unknown', InfraStatusLabelKey> = {
  ok: 'widgets.infra.statusLabels.ok',
  degraded: 'widgets.infra.statusLabels.degraded',
  down: 'widgets.infra.statusLabels.down',
  unknown: 'widgets.infra.statusLabels.unknown',
}

export default function InfraStatusWidget() {
  const { t } = useTranslation('dashboard')
  const { user } = useAuth()
  const [status, setStatus] = useState<StatusResponse | null>(null)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    fetch('/api/infra/status', { credentials: 'include', signal: controller.signal })
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data) setStatus(data)
        setLoaded(true)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('InfraStatusWidget fetch error:', err)
        setLoaded(true)
      })

    return () => { controller.abort() }
  }, [user])

  if (!user || !loaded) return null
  if (loaded && (!status || status.modules.length === 0)) return null

  const systemdModule = status!.modules.find(m => m.name === 'systemd')
  const failingServices = systemdModule?.details?.services?.filter(s => s.status === 'down') ?? []

  return (
    <Widget title={t('widgets.infra.title')}>
      {/* Overall status banner */}
      <div className={`flex items-center gap-2 px-3 py-2 rounded-lg mb-3 ${overallBannerClass[status!.overall] ?? overallBannerClass.unknown}`}>
        <StatusIcon status={status!.overall} size={14} />
        <span className="text-sm">{t(statusLabelKey[status!.overall] ?? statusLabelKey.unknown)}</span>
      </div>

      {/* Systemd failing services — most prominent alert */}
      {failingServices.length > 0 && (
        <div className="bg-red-500/15 border border-red-500/50 rounded-lg px-3 py-2 mb-3">
          <div className="flex items-center gap-1.5 mb-1">
            <XCircle size={14} className="text-red-400 shrink-0 animate-pulse" />
            <span className="text-xs font-semibold text-red-400">{t('widgets.infra.servicesDown')}</span>
          </div>
          <ul className="space-y-0.5">
            {failingServices.map((s, i) => (
              <li key={`${s.unit}-${s.name}-${i}`} className="text-xs text-red-300 font-medium">
                {s.name} <span className="text-red-400/70 font-normal">({s.unit})</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Module status grid — 2 columns */}
      <div className="grid grid-cols-2 gap-1.5 mb-3">
        {status!.modules.map(m => {
          const pillStatus = m.name === 'systemd' && failingServices.length > 0 ? 'down' : m.status

          return (
            <div
              key={m.name}
              className="flex items-center gap-1.5 bg-gray-700/50 rounded-md px-2 py-1.5 min-w-0"
            >
              <StatusIcon status={pillStatus} size={12} />
              <span className="text-xs text-gray-300 truncate">{formatModuleName(m.name)}</span>
            </div>
          )
        })}
      </div>

      <div className="flex items-center justify-between text-xs text-gray-500">
        <span>{t('widgets.infra.modulesMonitored', { count: status!.modules.length })}</span>
        <Link to="/infra" className="text-blue-400 hover:text-blue-300">
          {t('widgets.infra.details')}
        </Link>
      </div>
    </Widget>
  )
}
