export const AUTO_TAG_PREFIX = 'auto:'
export const AI_TAG_PREFIX = 'ai:'
export const AUTO_TAG_TOOLTIP = 'Auto-generated from workout structure'
export const AI_TAG_TOOLTIP = 'AI-classified by Claude'

export function isAutoTag(tag: string): boolean {
  return tag.startsWith(AUTO_TAG_PREFIX)
}

export function isAITag(tag: string): boolean {
  return tag.startsWith(AI_TAG_PREFIX)
}

export function displayTag(tag: string): string {
  if (isAutoTag(tag)) return tag.slice(AUTO_TAG_PREFIX.length)
  if (isAITag(tag)) return tag.slice(AI_TAG_PREFIX.length)
  return tag
}
