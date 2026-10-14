category: Changed
- **Work hours punch clock state lives at the page level** - Punch-in/out state, the saving flag and the live estimate moved into a shared `usePunchClock` hook owned by the Work Hours page, so the `p` shortcut toggles the punch clock directly from any tab and is ignored while a punch is still saving. (Hytte-41pfm)
