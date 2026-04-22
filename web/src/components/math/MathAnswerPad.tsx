import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Delete } from 'lucide-react'

export interface MathAnswerPadProps {
  input: string
  onDigit: (digit: string) => void
  onBackspace: () => void
  onSubmit: () => void
  disabled?: boolean
  busy?: boolean
}

// Shared answer display + numeric keypad for Regnemester game modes.
// Owns the physical-keyboard listener so callers don't have to re-wire
// identical handlers in every mode.
export function MathAnswerPad({ input, onDigit, onBackspace, onSubmit, disabled, busy }: MathAnswerPadProps) {
  const { t } = useTranslation('regnemester')

  useEffect(() => {
    if (disabled) return
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (e.key >= '0' && e.key <= '9') {
        onDigit(e.key)
        e.preventDefault()
      } else if (e.key === 'Backspace') {
        onBackspace()
        e.preventDefault()
      } else if (e.key === 'Enter') {
        onSubmit()
        e.preventDefault()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [disabled, onDigit, onBackspace, onSubmit])

  const inputsDisabled = disabled || busy
  const submitDisabled = inputsDisabled || input.length === 0

  return (
    <>
      <div
        aria-live="polite"
        aria-label={t('keypad.answerInputLabel')}
        className="w-full max-w-xs mx-auto h-16 sm:h-20 rounded-lg border-2 border-gray-700 bg-gray-800 flex items-center justify-center text-3xl sm:text-4xl font-bold text-white tabular-nums select-none"
      >
        {input || <span className="text-gray-600">_</span>}
      </div>
      <div className="grid grid-cols-3 gap-2 sm:gap-3 max-w-md mx-auto w-full pb-2 mt-6">
        {['1', '2', '3', '4', '5', '6', '7', '8', '9'].map(d => (
          <KeypadButton key={d} onClick={() => onDigit(d)} disabled={inputsDisabled}>
            {d}
          </KeypadButton>
        ))}
        <KeypadButton
          onClick={onBackspace}
          disabled={inputsDisabled}
          variant="muted"
          ariaLabel={t('keypad.backspaceAria')}
        >
          <Delete size={28} />
        </KeypadButton>
        <KeypadButton onClick={() => onDigit('0')} disabled={inputsDisabled}>
          0
        </KeypadButton>
        <KeypadButton
          onClick={onSubmit}
          disabled={submitDisabled}
          variant="primary"
          ariaLabel={t('keypad.enterAria')}
        >
          {busy ? '…' : t('keypad.enter')}
        </KeypadButton>
      </div>
    </>
  )
}

interface KeypadButtonProps {
  onClick: () => void
  disabled?: boolean
  variant?: 'default' | 'primary' | 'muted'
  ariaLabel?: string
  children: React.ReactNode
}

function KeypadButton({ onClick, disabled, variant = 'default', ariaLabel, children }: KeypadButtonProps) {
  const base = 'h-16 sm:h-20 rounded-lg text-2xl sm:text-3xl font-bold flex items-center justify-center select-none transition-colors disabled:opacity-50 disabled:cursor-not-allowed touch-manipulation'
  const styles = {
    default: 'bg-gray-800 hover:bg-gray-700 active:bg-gray-600 text-white border border-gray-700',
    primary: 'bg-blue-600 hover:bg-blue-500 active:bg-blue-700 text-white',
    muted: 'bg-gray-800 hover:bg-gray-700 active:bg-gray-600 text-gray-300 border border-gray-700',
  }[variant]
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={ariaLabel}
      className={`${base} ${styles}`}
    >
      {children}
    </button>
  )
}
