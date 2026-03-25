import { type CSSProperties, useEffect, useMemo, useState } from 'react'
import './confetti.css'

const COLORS = ['#facc15', '#fde047', '#fb923c', '#a855f7', '#3b82f6', '#f97316', '#fbbf24']

interface Particle {
  id: number
  left: number
  delay: number
  duration: number
  drift: number
  rot: number
  color: string
  isCircle: boolean
  size: number
}

function generateParticles(): Particle[] {
  return Array.from({ length: 50 }, (_, i) => ({
    id: i,
    left: Math.random() * 95,
    delay: Math.random() * 0.8,
    duration: 2.5 + Math.random() * 1,
    drift: (Math.random() - 0.5) * 180,
    rot: (Math.random() < 0.5 ? 1 : -1) * (360 + Math.random() * 360),
    color: COLORS[Math.floor(Math.random() * COLORS.length)],
    isCircle: Math.random() > 0.5,
    size: 8 + Math.floor(Math.random() * 9),
  }))
}

interface ConfettiProps {
  active: boolean
}

export default function Confetti({ active }: ConfettiProps) {
  const [done, setDone] = useState(false)
  const particles = useMemo(() => (active ? generateParticles() : []), [active])

  useEffect(() => {
    if (!active) return
    const maxRuntimeSeconds = particles.reduce(
      (max, p) => Math.max(max, p.delay + p.duration),
      0
    )
    const timeoutMs = maxRuntimeSeconds * 1000 + 200
    const timer = setTimeout(() => setDone(true), timeoutMs)
    return () => {
      clearTimeout(timer)
      setDone(false)
    }
  }, [active, particles])

  if (!active || done) return null

  return (
    <div
      className="fixed inset-0 pointer-events-none overflow-hidden"
      style={{ zIndex: 9999 }}
      aria-hidden="true"
    >
      {particles.map(p => (
        <div
          key={p.id}
          className="confetti-particle absolute"
          style={{
            left: `${p.left}%`,
            top: 0,
            width: p.size,
            height: p.size,
            backgroundColor: p.color,
            borderRadius: p.isCircle ? '50%' : '2px',
            '--confetti-drift': `${p.drift}px`,
            '--confetti-rot': `${p.rot}deg`,
            animation: `confettiFall ${p.duration}s ${p.delay}s ease-in forwards`,
          } as CSSProperties}
        />
      ))}
    </div>
  )
}
