category: Added
- **Family Chat voice-note plumbing** - Added audio/webm and audio/ogg (plus application/ogg, the type http.DetectContentType returns for OggS-prefixed bodies) to the attachment MIME allowlist so MediaRecorder voice notes upload across browsers. Added a nullable meta_json column to family_chat_messages — encrypted at rest like the body — and exposed it on the POST /messages request and GET /messages response so clients can persist the precomputed waveform and duration server-side. (Hytte-bgvt)

