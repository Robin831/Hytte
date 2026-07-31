category: Added
- **Dashboard widgets are isolated by error boundaries** - Every widget on the dashboard is now wrapped in a `WidgetBoundary`, so a render failure in one widget shows a compact "widget unavailable" tile with a Retry button instead of blanking the entire dashboard. (Hytte-q1lvu)
