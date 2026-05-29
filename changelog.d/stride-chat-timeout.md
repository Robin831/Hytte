category: Fixed
- **Stride coach chat timeout handling** - Raised the per-attempt Claude timeout to 300s (plan-editing turns stream a full week of JSON), gave each retry its own fresh deadline instead of reusing the expired one, and surface timeouts as a clear "took too long" message instead of an opaque "signal: killed".
