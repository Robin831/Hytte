import { AlertTriangle, CheckCircle2, Circle, XCircle } from 'lucide-react'
import type { StrideEvaluation } from '../../types/stride'

export function complianceIcon(compliance: StrideEvaluation['compliance']) {
  switch (compliance) {
    case 'compliant':
      return <CheckCircle2 size={18} className="text-green-400" />
    case 'partial':
      return <AlertTriangle size={18} className="text-yellow-400" />
    case 'missed':
      return <XCircle size={18} className="text-red-400" />
    case 'bonus':
      return <CheckCircle2 size={18} className="text-blue-400" />
    case 'rest_day':
      return <CheckCircle2 size={18} className="text-gray-400" />
    default:
      return <Circle size={18} className="text-gray-400" />
  }
}

export function complianceBadgeClass(compliance: StrideEvaluation['compliance']): string {
  switch (compliance) {
    case 'compliant':
      return 'bg-green-500/15 text-green-400 border-green-500/30'
    case 'partial':
      return 'bg-yellow-500/15 text-yellow-400 border-yellow-500/30'
    case 'missed':
      return 'bg-red-500/15 text-red-400 border-red-500/30'
    case 'bonus':
      return 'bg-blue-500/15 text-blue-400 border-blue-500/30'
    case 'rest_day':
      return 'bg-gray-500/15 text-gray-400 border-gray-500/30'
    default:
      return 'bg-gray-500/15 text-gray-400 border-gray-500/30'
  }
}

export function flagIsSevere(flag: string): boolean {
  return flag === 'overtraining' || flag === 'injury_risk'
}
