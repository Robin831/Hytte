// Applies a digit press to the current answer string with consistent
// validation across game modes: caps at 3 digits (answers never exceed
// 1–100) and avoids leading-zero strings like "07".
export function appendAnswerDigit(current: string, digit: string): string {
  if (current.length >= 3) return current
  if (current === '' && digit === '0') return '0'
  if (current === '0') return digit
  return current + digit
}
