category: Added
- **Infrastructure monitoring modules** - Added three built-in infra modules: Service Health Checks (HTTP endpoint monitoring), SSL Certificate Expiry (TLS cert expiration tracking), and Uptime History (historical uptime statistics). Each module includes a management UI for adding/removing targets and viewing detailed check results. (Hytte-9uq)

category: Security
- **SSRF protection with DNS rebinding defense** - URLs and hostnames are validated at both input time and connection time using a safe dialer that checks resolved IPs before establishing connections. HTTP redirects are not followed to prevent redirect-based SSRF. Port range is validated for SSL cert monitoring. (Hytte-9uq)
