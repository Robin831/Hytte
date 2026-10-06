category: Changed
- **Frontend type-checking now uses TypeScript 7** - `tsc -b` in `web/` runs the native TypeScript 7 compiler (installed as `@typescript/native`), while the `typescript` package is aliased to `@typescript/typescript6` so typescript-eslint, which does not support TypeScript 7 yet, keeps the TypeScript 6 API. (Hytte-btf1a)
