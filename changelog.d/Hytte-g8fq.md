category: Fixed
- **Homework chat sends now work** - Added missing `--verbose` flag to Claude CLI invocation required by `--output-format stream-json`, which caused every homework message to fail. (Hytte-g8fq)
- **Better error diagnostics for Claude CLI failures** - Captured stderr from the Claude CLI subprocess so failures include actionable error messages instead of just "exit status 1". (Hytte-g8fq)
