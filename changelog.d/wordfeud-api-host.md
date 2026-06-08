category: Fixed
- **Wordfeud integration restored after server-side host change** - Wordfeud retired their per-shard `gameNN.wordfeud.com` hostnames (all now return NXDOMAIN), which broke every API call with a DNS "no such host" error. Point the client base URL at the consolidated `api.wordfeud.com/wf` endpoint.
