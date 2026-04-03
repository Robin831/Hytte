category: Added
- **Per-expense split configuration for recurring transactions** - Added `split_type` (percentage/equal/fixed_you/fixed_partner) and `split_pct` (0–100, nullable) fields to recurring transaction rules. `split_type` defaults to `percentage`; `split_pct` defaults to null (uses the global income split). Enables per-expense split ratios in the regning calculator. (Hytte-lyiw)
