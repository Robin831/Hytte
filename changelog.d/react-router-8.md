category: Security
- **react-router 8.3.0** - Migrated the frontend off the retired react-router-dom package to react-router v8, clearing the GHSA-qwww-vcr4-c8h2 audit advisory (RSC-mode CSRF, fixed upstream only in 8.3.0). Pure import rewrite — all APIs used live in the core package; npm audit is now clean.
