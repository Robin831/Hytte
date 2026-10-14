import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../ui/dialog'
import type { NoteScope } from '../../types/stride'

interface EditNoteDialogProps {
  open: boolean
  onClose: () => void
  onSubmit: (e: React.FormEvent) => void
  content: string
  onContentChange: (v: string) => void
  targetDate: string
  onTargetDateChange: (v: string) => void
  scope: NoteScope
  onScopeChange: (v: NoteScope) => void
  submitting: boolean
  error: string
}

export function EditNoteDialog({
  open, onClose, onSubmit, content, onContentChange, targetDate, onTargetDateChange,
  scope, onScopeChange, submitting, error,
}: EditNoteDialogProps) {
  const { t } = useTranslation('stride')
  const titleId = useId()
  return (
    <Dialog open={open} onClose={onClose} aria-labelledby={titleId}>
      <DialogHeader id={titleId} title={t('notes.editTitle')} onClose={onClose} />
      <DialogBody>
        <form id="edit-note-form" onSubmit={onSubmit} className="space-y-3">
          <textarea
            value={content}
            onChange={e => onContentChange(e.target.value)}
            rows={4}
            aria-label={t('notes.editTitle')}
            className="w-full bg-gray-800 border border-gray-700 rounded-xl px-4 py-3 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500 resize-none"
          />
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <label htmlFor="edit-note-target-date" className="text-xs text-gray-400 w-24">{t('notes.targetDate')}</label>
              <input
                id="edit-note-target-date"
                type="date"
                value={targetDate}
                onChange={e => onTargetDateChange(e.target.value)}
                className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
              />
            </div>
            <div className="flex items-center gap-2">
              <label htmlFor="edit-note-scope" className="text-xs text-gray-400 w-24">{t('notes.scopeLabel')}</label>
              <select
                id="edit-note-scope"
                value={scope}
                onChange={e => onScopeChange(e.target.value as NoteScope)}
                className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
              >
                <option value="any">{t('notes.scope.any')}</option>
                <option value="nightly">{t('notes.scope.nightly')}</option>
                <option value="weekly">{t('notes.scope.weekly')}</option>
              </select>
            </div>
          </div>
          {error && <p className="text-sm text-red-400">{error}</p>}
        </form>
      </DialogBody>
      <DialogFooter>
        <button
          type="button"
          onClick={onClose}
          className="px-4 py-2 text-sm text-gray-300 hover:text-white rounded-lg transition-colors"
        >
          {t('notes.cancel')}
        </button>
        <button
          type="submit"
          form="edit-note-form"
          disabled={submitting || !content.trim()}
          className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg transition-colors"
        >
          {submitting ? t('notes.saving') : t('notes.save')}
        </button>
      </DialogFooter>
    </Dialog>
  )
}
