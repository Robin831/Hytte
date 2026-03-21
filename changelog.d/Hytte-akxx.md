category: Fixed
- **COROS FIT file import failures** - Replaced `tormoder/fit` with `muktihari/fit` as the FIT file decoder. The previous library strictly rejected files from COROS devices (e.g. COROS PACE Pro) where definition message field sizes didn't match the base type spec. `muktihari/fit` handles these non-standard files gracefully. (Hytte-akxx)
