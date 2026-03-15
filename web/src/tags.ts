export const AUTO_TAG_PREFIX = 'auto:'
export const AUTO_TAG_TOOLTIP = 'Auto-generated from workout structure'

export function isAutoTag(tag: string): boolean {
  return tag.startsWith(AUTO_TAG_PREFIX)
}

export function displayTag(tag: string): string {
  return isAutoTag(tag) ? tag.slice(AUTO_TAG_PREFIX.length) : tag
}
