category: Fixed
- **Cancel in-flight Stars journey fetch on unmount** - The Stars dashboard journey card now aborts its `/api/stars/journey` request when navigating away before it resolves, preventing state updates and spurious error UI on an unmounted component. (Hytte-2nzl)
