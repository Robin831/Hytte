import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronDown, ChevronRight, X } from 'lucide-react';

interface ProposedStage {
  stage_number: number;
  speed_kmh: number;
  lactate_mmol: number;
  heart_rate_bpm: number;
  lap_number: number;
}

interface PreviewResponse {
  stages: ProposedStage[];
  warnings: string[];
  method: string;
}

interface ImportCreatedTest {
  id: number;
}

interface ImportResponse {
  test: ImportCreatedTest;
}

interface Props {
  workoutId: string;
  onClose: () => void;
  onSuccess: (testId: string) => void;
}

export default function LactateImportDialog({ workoutId, onClose, onSuccess }: Props) {
  const { t } = useTranslation('lactate');

  const [rawData, setRawData] = useState('');
  const [warmupMin, setWarmupMin] = useState(10);
  const [stageMin, setStageMin] = useState(5);
  const [settingsOpen, setSettingsOpen] = useState(false);

  const [previewing, setPreviewing] = useState(false);
  const [previewError, setPreviewError] = useState('');
  const [preview, setPreview] = useState<PreviewResponse | null>(null);
  // Local editable HR values keyed by stage index
  const [editedHr, setEditedHr] = useState<Record<number, string>>({});

  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');

  async function handlePreview() {
    if (!rawData.trim()) {
      setPreviewError(t('errors.dataRequired'));
      return;
    }
    setPreviewing(true);
    setPreviewError('');
    setPreview(null);
    setEditedHr({});
    try {
      const res = await fetch('/api/lactate/tests/preview-from-workout', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          workout_id: parseInt(workoutId, 10),
          lactate_data: rawData,
          warmup_duration_min: warmupMin,
          stage_duration_min: stageMin,
        }),
      });
      if (!res.ok) {
        const data = (await res.json().catch(() => ({}))) as { error?: string; hint?: string };
        let message = data.error ?? t('errors.failedToPreview');
        if (typeof data.hint === 'string' && data.hint.trim()) {
          message = `${message} (${data.hint})`;
        }
        throw new Error(message);
      }
      const data: PreviewResponse = await res.json();
      setPreview(data);
    } catch (err) {
      setPreviewError(err instanceof Error ? err.message : t('errors.failedToPreview'));
    } finally {
      setPreviewing(false);
    }
  }

  async function handleCreate() {
    if (!preview) return;
    setCreating(true);
    setCreateError('');
    try {
      // Build stages for creation, applying any edited HR values
      const stagesForCreate = preview.stages.map((stage, idx) => {
        const edited = editedHr[idx];
        let heartRate = stage.heart_rate_bpm;
        if (edited !== undefined) {
          const parsed = parseInt(edited, 10);
          if (!isNaN(parsed)) {
            heartRate = parsed;
          }
        }
        return {
          ...stage,
          heart_rate_bpm: heartRate,
        };
      });

      const res = await fetch('/api/lactate/tests', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          method: preview.method,
          stages: stagesForCreate,
          workout_id: parseInt(workoutId, 10),
        }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error((data as { error?: string }).error ?? t('errors.failedToImport'));
      }
      const data: ImportResponse = await res.json();
      onSuccess(String(data.test.id));
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : t('errors.failedToImport'));
    } finally {
      setCreating(false);
    }
  }

  function hrForStage(index: number, stage: ProposedStage): string {
    return editedHr[index] !== undefined ? editedHr[index] : String(stage.heart_rate_bpm);
  }

  function confidenceBadge(stage: ProposedStage) {
    if (stage.heart_rate_bpm > 0) {
      return (
        <span className="inline-block rounded px-1.5 py-0.5 text-xs font-medium bg-green-900 text-green-300">
          {t('import.confidence.matched')}
        </span>
      );
    }
    return (
      <span className="inline-block rounded px-1.5 py-0.5 text-xs font-medium bg-gray-700 text-gray-400">
        {t('import.confidence.noHr')}
      </span>
    );
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="lactate-import-dialog-title"
        className="w-full max-w-2xl rounded-lg bg-gray-900 border border-gray-700 shadow-xl flex flex-col max-h-[90vh]"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-700 shrink-0">
          <h2 id="lactate-import-dialog-title" className="text-lg font-semibold text-white">{t('import.title')}</h2>
          <button
            onClick={onClose}
            aria-label={t('import.close')}
            className="text-gray-400 hover:text-white transition-colors"
          >
            <X size={20} />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
          {/* Paste area */}
          <div>
            <label htmlFor="lactate-import-data" className="block text-sm font-medium text-gray-300 mb-1">
              {t('import.pasteData')}
            </label>
            <textarea
              id="lactate-import-data"
              value={rawData}
              onChange={(e) => setRawData(e.target.value)}
              rows={6}
              placeholder={t('import.dataPlaceholder')}
              className="w-full rounded bg-gray-800 border border-gray-600 text-white text-sm px-3 py-2 placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono resize-y"
            />
            <p className="mt-1 text-xs text-gray-500">{t('import.dataHint')}</p>
          </div>

          {/* Collapsible settings */}
          <div className="border border-gray-700 rounded">
            <button
              type="button"
              onClick={() => setSettingsOpen((o) => !o)}
              className="flex items-center gap-2 w-full px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors"
              aria-expanded={settingsOpen}
            >
              {settingsOpen ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
              {t('import.settings')}
            </button>
            {settingsOpen && (
              <div className="px-4 pb-4 grid grid-cols-2 gap-4">
                <div>
                  <label htmlFor="warmup-min" className="block text-xs text-gray-400 mb-1">
                    {t('import.warmupDuration')}
                  </label>
                  <input
                    id="warmup-min"
                    type="number"
                    min={0}
                    value={warmupMin}
                    onChange={(e) => setWarmupMin(Number(e.target.value))}
                    className="w-full rounded bg-gray-800 border border-gray-600 text-white text-sm px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
                <div>
                  <label htmlFor="stage-min" className="block text-xs text-gray-400 mb-1">
                    {t('import.stageDuration')}
                  </label>
                  <input
                    id="stage-min"
                    type="number"
                    min={1}
                    max={60}
                    value={stageMin}
                    onChange={(e) => setStageMin(Number(e.target.value))}
                    className="w-full rounded bg-gray-800 border border-gray-600 text-white text-sm px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
              </div>
            )}
          </div>

          {/* Preview error */}
          {previewError && (
            <p className="text-sm text-red-400">{previewError}</p>
          )}

          {/* Warnings */}
          {preview && preview.warnings.length > 0 && (
            <div className="rounded border border-yellow-600 bg-yellow-900/30 px-4 py-3 space-y-1">
              {preview.warnings.map((w) => (
                <p key={w} className="text-sm text-yellow-300">{w}</p>
              ))}
            </div>
          )}

          {/* Preview table */}
          {preview && (
            <div>
              <p className="text-xs text-gray-500 mb-2">
                {t('import.method', { method: preview.method })}
              </p>
              <div className="overflow-x-auto rounded border border-gray-700">
                <table className="w-full text-sm text-left">
                  <thead className="bg-gray-800 text-gray-400 text-xs uppercase">
                    <tr>
                      <th className="px-3 py-2">{t('columns.number')}</th>
                      <th className="px-3 py-2">{t('columns.speedKmh')}</th>
                      <th className="px-3 py-2">{t('columns.lactateUnit')}</th>
                      <th className="px-3 py-2">{t('columns.hrBpm')}</th>
                      <th className="px-3 py-2">{t('import.confidence.label')}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-700">
                    {preview.stages.map((stage, idx) => (
                      <tr key={stage.stage_number} className="text-gray-200">
                        <td className="px-3 py-2 text-gray-400">{stage.stage_number}</td>
                        <td className="px-3 py-2">{stage.speed_kmh.toFixed(1)}</td>
                        <td className="px-3 py-2">{stage.lactate_mmol.toFixed(2)}</td>
                        <td className="px-3 py-2">
                          <input
                            type="number"
                            min={0}
                            value={hrForStage(idx, stage)}
                            onChange={(e) =>
                              setEditedHr((prev) => ({ ...prev, [idx]: e.target.value }))
                            }
                            aria-label={t('import.hrLabel', { number: stage.stage_number })}
                            className="w-20 rounded bg-gray-800 border border-gray-600 text-white text-sm px-2 py-0.5 focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                        </td>
                        <td className="px-3 py-2">{confidenceBadge(stage)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Create error */}
          {createError && (
            <p className="text-sm text-red-400">{createError}</p>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between gap-3 px-6 py-4 border-t border-gray-700 shrink-0">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors"
          >
            {t('import.cancel')}
          </button>
          <div className="flex gap-3">
            <button
              type="button"
              onClick={handlePreview}
              disabled={previewing}
              className="px-4 py-2 text-sm font-medium rounded bg-gray-700 text-white hover:bg-gray-600 disabled:opacity-50 transition-colors"
            >
              {previewing ? t('import.previewing') : t('import.preview')}
            </button>
            {preview && (
              <button
                type="button"
                onClick={handleCreate}
                disabled={creating}
                className="px-4 py-2 text-sm font-medium rounded bg-blue-600 text-white hover:bg-blue-500 disabled:opacity-50 transition-colors"
              >
                {creating ? t('import.creating') : t('import.createTest')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
