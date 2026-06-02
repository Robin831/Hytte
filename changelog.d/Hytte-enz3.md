category: Fixed
- **Abort stale Wordfeud solver requests on position change** - Editing the board or changing the rack while a solve is in flight now aborts that request so its slow response can no longer overwrite the current results, and clears the move selection/hover so preview overlays never highlight squares that no longer match the board. (Hytte-enz3)
