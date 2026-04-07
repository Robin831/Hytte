export const SECTION_ORDER: string[] = ['ready', 'in-progress', 'unlabeled', 'needs-attention']

export const sectionColors: Record<string, string> = {
  ready: 'bg-green-500/20 text-green-400 border-green-700/30',
  'in-progress': 'bg-blue-500/20 text-blue-400 border-blue-700/30',
  unlabeled: 'bg-gray-500/20 text-gray-400 border-gray-600/30',
  'needs-attention': 'bg-amber-500/20 text-amber-400 border-amber-700/30',
}
