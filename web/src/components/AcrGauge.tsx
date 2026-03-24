const CX = 100
const CY = 100
const R = 76
const MAX_VAL = 2.0

function valToPoint(val: number, r: number = R): { x: number; y: number } {
  const clamped = Math.max(0, Math.min(val, MAX_VAL))
  const angle = Math.PI * (1 - clamped / MAX_VAL)
  return {
    x: CX + r * Math.cos(angle),
    y: CY - r * Math.sin(angle),
  }
}

function arcSegment(v1: number, v2: number, r: number = R): string {
  const p1 = valToPoint(v1, r)
  const p2 = valToPoint(v2, r)
  const spanFraction = (v2 - v1) / MAX_VAL
  const largeArc = spanFraction > 0.5 ? 1 : 0
  return `M ${p1.x.toFixed(2)} ${p1.y.toFixed(2)} A ${r} ${r} 0 ${largeArc} 0 ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`
}

const ZONES: Array<{ min: number; max: number; color: string }> = [
  { min: 0, max: 0.8, color: '#3b82f6' },
  { min: 0.8, max: 1.3, color: '#22c55e' },
  { min: 1.3, max: 1.5, color: '#eab308' },
  { min: 1.5, max: MAX_VAL, color: '#ef4444' },
]

// Zone boundary tick values (excludes 0 and MAX_VAL — those are the arc endpoints)
const DIVIDERS = [0.8, 1.3, 1.5]

function zoneColorFor(acr: number): string {
  const zone = ZONES.find((z) => acr >= z.min && acr < z.max)
  return zone?.color ?? (acr >= MAX_VAL ? '#ef4444' : '#3b82f6')
}

interface AcrGaugeProps {
  acr: number
  ariaLabel?: string
}

export function AcrGauge({ acr, ariaLabel }: AcrGaugeProps) {
  const needlePt = valToPoint(acr, R - 12)
  const valueColor = zoneColorFor(acr)

  return (
    <svg viewBox="0 0 200 122" aria-label={ariaLabel} role="img" className="w-full h-full">
      {/* Background track */}
      <path
        d={arcSegment(0, MAX_VAL)}
        fill="none"
        stroke="#374151"
        strokeWidth={15}
        strokeLinecap="butt"
      />

      {/* Coloured zone segments */}
      {ZONES.map((z) => (
        <path
          key={z.color}
          d={arcSegment(z.min, z.max)}
          fill="none"
          stroke={z.color}
          strokeWidth={15}
          strokeLinecap="butt"
          opacity={0.9}
        />
      ))}

      {/* Divider lines between zones */}
      {DIVIDERS.map((v) => {
        const inner = valToPoint(v, R - 8)
        const outer = valToPoint(v, R + 8)
        return (
          <line
            key={v}
            x1={inner.x.toFixed(2)}
            y1={inner.y.toFixed(2)}
            x2={outer.x.toFixed(2)}
            y2={outer.y.toFixed(2)}
            stroke="#111827"
            strokeWidth={2.5}
          />
        )
      })}

      {/* Needle */}
      <line
        x1={CX}
        y1={CY}
        x2={needlePt.x.toFixed(2)}
        y2={needlePt.y.toFixed(2)}
        stroke="#f9fafb"
        strokeWidth={2.5}
        strokeLinecap="round"
      />
      <circle cx={CX} cy={CY} r={5} fill="#f9fafb" />
      <circle cx={CX} cy={CY} r={3} fill="#374151" />

      {/* ACR numeric value */}
      <text
        x={CX}
        y={CY + 17}
        textAnchor="middle"
        fill={valueColor}
        fontSize={14}
        fontWeight="bold"
      >
        {acr.toFixed(2)}
      </text>
    </svg>
  )
}
