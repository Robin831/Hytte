// Emoji choices shown in the deduction-preset icon picker, grouped by category.
export const DEDUCTION_EMOJIS = [
  { key: 'transport', emojis: ['🚗', '🚌', '🚲', '🚶', '🚕', '✈️'] },
  { key: 'childcare', emojis: ['👶', '🏫', '🎒', '🧒', '🍼'] },
  { key: 'medical', emojis: ['🏥', '💊', '🦷', '🩺', '💉'] },
  { key: 'errands', emojis: ['🛒', '📬', '🏦', '🏪', '📦'] },
  { key: 'meetings', emojis: ['☕', '📞', '💼', '🤝', '📅'] },
  { key: 'general', emojis: ['⏰', '🔧', '📋', '⏸️', '🔔'] },
]

// 'clock' was the legacy text value stored before the emoji picker was added.
// For persistence, treat it as "no icon" by normalizing it to an empty string.
export function normalizePresetIconValue(icon: string): string {
  if (icon === 'clock') return ''
  return icon || ''
}

// For UI display, show the clock emoji as a placeholder when there is no icon
// or when the legacy 'clock' value is present.
export function getPresetIconDisplay(icon: string): string {
  if (!icon || icon === 'clock') return '⏰'
  return icon
}

// Legacy helper kept for backwards compatibility: this now only performs
// storage normalization. Prefer using `normalizePresetIconValue` for
// persistence and `getPresetIconDisplay` for rendering.
export function normalizePresetIcon(icon: string): string {
  return normalizePresetIconValue(icon)
}
