category: Added
- **Persist punch-in state across page reloads** - Punch-in state is now stored server-side via a dedicated `work_open_sessions` table. Punching in creates a record with the start time; punching out closes the session and saves the completed work session. Page reloads and device switches restore the active punch-in automatically. (Hytte-by1z)
