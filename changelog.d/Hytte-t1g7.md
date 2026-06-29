category: Added
- **Optimistic message sending in Family Chat** - Typed text messages now render instantly in a "sending" state and reconcile in place when the saved message arrives (via the POST response or the SSE event, whichever lands first), with no duplicate. A failed send shows a "failed — tap to retry" affordance that preserves the text and re-sends it. (Hytte-t1g7)
