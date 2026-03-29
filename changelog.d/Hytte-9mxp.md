category: Added
- **Dialog component** - Added reusable `Dialog`, `DialogHeader`, `DialogBody`, `DialogFooter`, and `ConfirmDialog` components to `web/src/components/ui/dialog.tsx`. Replaces native `window.confirm()` calls and hand-rolled modal overlays with accessible, i18n-aware dialogs. (Hytte-9mxp)
- **Confirmation dialogs** - Replaced `window.confirm()` in Notes and WorkHoursPage, refactored `LactateImportDialog` to use the new Dialog primitives, and converted inline confirmation panels in Family, FamilyChallenges, and AllowancePage to proper modal dialogs. (Hytte-9mxp)
- **i18n keys** - Added `confirm.cancel`, `confirm.confirm`, and `confirm.delete` keys to `common.json` for English, Norwegian, and Thai locales. (Hytte-9mxp)
