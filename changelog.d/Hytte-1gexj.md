category: Changed
- **Dashboard loads its heavy widgets on demand** - The Infrastructure, GitHub Actions and Weather Station widgets are code-split into their own chunks so they no longer download and fetch on first paint. Each shows a widget-sized loading placeholder while its chunk arrives, and still degrades to the usual error tile if it fails to load. (Hytte-1gexj)
