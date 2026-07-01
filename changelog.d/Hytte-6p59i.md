category: Changed
- **Refactor Settings page into per-section components** - Split the monolithic `Settings.tsx` into per-section components under `web/src/pages/settings/` (profile, training, notifications, security, integrations, pokemon, AI automation, kiosk tokens) with a shared types module, leaving `Settings.tsx` as a thin orchestrator. No behavior, copy, or API changes. (Hytte-6p59i)
