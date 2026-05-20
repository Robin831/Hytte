category: Added
- **Family Chat voice call hook** - Added the useVoiceCall hook that wires RTCPeerConnection to the Family Chat signalling relay: fetches STUN/TURN config, drives the offer/answer/ICE/end lifecycle, listens to call_* SSE events, and keeps an AudioContext alive so backgrounded tabs do not let the call suspend. (Hytte-go7t)
