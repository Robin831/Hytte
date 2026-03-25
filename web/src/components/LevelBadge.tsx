import { useTranslation } from 'react-i18next'

interface LevelBadgeProps {
  level: number
  emoji?: string
  title: string
}

export default function LevelBadge({ level, emoji, title }: LevelBadgeProps) {
  const { t } = useTranslation('common')
  return (
    <div className="inline-flex items-center gap-1.5 bg-yellow-500/10 border border-yellow-500/20 rounded-full px-4 py-1">
      {emoji && (
        <span className="text-sm" role="img" aria-hidden="true">{emoji}</span>
      )}
      <span className="text-yellow-300 text-sm font-medium">
        {t('stars.level', { level })} · {title}
      </span>
    </div>
  )
}
